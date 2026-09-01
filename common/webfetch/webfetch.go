package webfetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"GopherAI/config"
)

// 网页抓取工具。
//
// 安全前提：抓取的 URL 来自模型，而模型可能被提示注入操纵。如果不加限制，
// 它就能把后端当作访问内网的跳板 —— 例如去读 http://mysql:3306、
// http://qdrant:6333，或云厂商的 169.254.169.254 元数据接口。
// 因此这里做了四道限制：
//  1. 协议白名单：只允许 http / https
//  2. 目标地址解析后逐个校验，命中私有 / 回环 / 链路本地 / 保留网段则拒绝
//  3. 重定向的每一跳都重新校验（否则可用公网域名 302 跳到内网）
//  4. 响应体大小与超时上限，避免被巨大页面拖死

// 需要拦截的网段：真正可能承载内部服务的地址。
//
// 刻意没有拦 198.18.0.0/15（RFC 2544 基准测试段）和 192.0.0.0/24：
// 它们不承载常规内网服务，而 Clash / sing-box 这类代理在 fake-IP 模式下
// 会把公网域名映射到 198.18.0.0/15，拦掉会导致正常网站全部访问不了。
var blockedCIDRs = []string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",  // CGNAT / Tailscale
	"127.0.0.0/8",    // 回环
	"169.254.0.0/16", // 链路本地（含云元数据 169.254.169.254）
	"172.16.0.0/12",
	"192.168.0.0/16",
	"224.0.0.0/4", // 多播
	"240.0.0.0/4",
	"::1/128",
	"fc00::/7",  // IPv6 私有
	"fe80::/10", // IPv6 链路本地
}

var blockedNets []*net.IPNet

func init() {
	for _, c := range blockedCIDRs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			blockedNets = append(blockedNets, n)
		}
	}
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// validateTarget 校验 URL 的协议，并确认它解析出的所有 IP 都不在受限网段
func validateTarget(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL 无法解析: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("只允许 http/https，收到: %s", u.Scheme)
	}

	if config.GetConfig().FetchConfig.FetchAllowPrivate {
		return nil
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少主机名")
	}

	// 直接写 IP 的情况
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("拒绝访问内网或保留地址: %s", host)
		}
		return nil
	}

	// 域名：解析后逐个校验，防止域名指向内网
	resolver := &net.Resolver{}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("域名解析失败: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("域名没有解析到任何地址: %s", host)
	}
	for _, a := range addrs {
		if isBlockedIP(a.IP) {
			return fmt.Errorf("拒绝访问内网或保留地址: %s -> %s", host, a.IP)
		}
	}
	return nil
}

// Fetch 抓取网页并提取正文文本
func Fetch(ctx context.Context, raw string) (string, error) {
	cfg := config.GetConfig().FetchConfig

	timeout := time.Duration(cfg.FetchTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	maxBytes := cfg.FetchMaxBytes
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	maxChars := cfg.FetchMaxChars
	if maxChars <= 0 {
		maxChars = 8000
	}

	if err := validateTarget(ctx, raw); err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{
		// 每一跳重定向都要重新校验，否则公网域名可以 302 到内网
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			return validateTarget(req.Context(), req.URL.String())
		},
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GopherAI/1.0)")
	req.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("抓取失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("目标返回状态码 %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !isTextual(ct) {
		return "", fmt.Errorf("不支持的内容类型: %s（只处理文本类页面）", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	text := body2text(string(body), ct)
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("页面编码不是 UTF-8，无法解析")
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("页面没有可提取的文本内容")
	}

	truncated := false
	if utf8.RuneCountInString(text) > maxChars {
		text = string([]rune(text)[:maxChars])
		truncated = true
	}

	out := fmt.Sprintf("来源: %s\n\n%s", raw, text)
	if truncated {
		out += "\n\n（内容较长，以上为截断后的前半部分）"
	}
	return out, nil
}

func isTextual(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/") ||
		strings.Contains(ct, "json") ||
		strings.Contains(ct, "xml") ||
		strings.Contains(ct, "javascript")
}

// Go 的 regexp 用的是 RE2，不支持反向引用（\1），所以这里为每个要剔除的标签
// 单独写一条模式，而不是用一条带反向引用的通用模式。
var reStripPairs = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<script[^>]*>.*?</script\s*>`),
	regexp.MustCompile(`(?is)<style[^>]*>.*?</style\s*>`),
	regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript\s*>`),
	regexp.MustCompile(`(?is)<svg[^>]*>.*?</svg\s*>`),
	regexp.MustCompile(`(?is)<head[^>]*>.*?</head\s*>`),
}

var (
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	reBlock   = regexp.MustCompile(`(?i)</(p|div|br|li|tr|h[1-6]|section|article)\s*>`)
	reTag     = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpaces  = regexp.MustCompile(`[ \t]+`)
	reNewline = regexp.MustCompile(`\n{3,}`)
)

// body2text 从 HTML 里粗略提取正文。
// 不追求完美排版，只要让模型能读懂主要文字即可。
func body2text(body, contentType string) string {
	ct := strings.ToLower(contentType)
	// 纯文本 / JSON 直接返回
	if strings.HasPrefix(ct, "text/plain") || strings.Contains(ct, "json") {
		return strings.TrimSpace(body)
	}

	s := reComment.ReplaceAllString(body, " ")
	for _, re := range reStripPairs {
		s = re.ReplaceAllString(s, " ")
	}
	// 块级标签结束处换行，保留段落感
	s = reBlock.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	s = unescapeEntities(s)
	s = reSpaces.ReplaceAllString(s, " ")

	// 逐行清理空白
	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	out := strings.Join(cleaned, "\n")
	return strings.TrimSpace(reNewline.ReplaceAllString(out, "\n\n"))
}

func unescapeEntities(s string) string {
	replacements := []string{
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", "\"", "&#39;", "'", "&apos;", "'", "&mdash;", "—",
		"&ndash;", "–", "&hellip;", "…", "&#x27;", "'", "&#x2F;", "/",
	}
	return strings.NewReplacer(replacements...).Replace(s)
}
