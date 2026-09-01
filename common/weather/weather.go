package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// 天气查询，数据来自 wttr.in（无需 API key）。
// 逻辑取自 common/mcp 里那个独立的 MCP 天气 server，这里作为内置工具直接调用，
// 省掉一个进程和一层协议。

const weatherTimeout = 20 * time.Second

type wttrResponse struct {
	CurrentCondition []struct {
		TempC         string `json:"temp_C"`
		FeelsLikeC    string `json:"FeelsLikeC"`
		Humidity      string `json:"humidity"`
		WindspeedKmph string `json:"windspeedKmph"`
		WeatherDesc   []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
	} `json:"current_condition"`

	NearestArea []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
	} `json:"nearest_area"`

	Weather []struct {
		Date     string `json:"date"`
		MaxtempC string `json:"maxtempC"`
		MintempC string `json:"mintempC"`
	} `json:"weather"`
}

// Get 查询指定城市的天气，返回可直接回灌给模型的文本
func Get(ctx context.Context, city string) (string, error) {
	if city == "" {
		return "", fmt.Errorf("city is required")
	}

	apiURL := fmt.Sprintf("https://wttr.in/%s?format=j1&lang=zh", url.PathEscape(city))

	reqCtx, cancel := context.WithTimeout(ctx, weatherTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/8.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("weather service returned %d", resp.StatusCode)
	}

	var parsed wttrResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse weather data: %w", err)
	}
	if len(parsed.CurrentCondition) == 0 {
		return "", fmt.Errorf("没有查询到 %s 的天气数据", city)
	}

	cc := parsed.CurrentCondition[0]

	location := city
	if len(parsed.NearestArea) > 0 && len(parsed.NearestArea[0].AreaName) > 0 {
		location = parsed.NearestArea[0].AreaName[0].Value
	}

	condition := "未知"
	if len(cc.WeatherDesc) > 0 {
		condition = cc.WeatherDesc[0].Value
	}

	out := fmt.Sprintf("地点: %s\n天气: %s\n气温: %s°C（体感 %s°C）\n湿度: %s%%\n风速: %s km/h",
		location, condition, cc.TempC, cc.FeelsLikeC, cc.Humidity, cc.WindspeedKmph)

	// 附上未来两天的温度区间，问"明天冷不冷"时用得上
	if len(parsed.Weather) > 1 {
		out += "\n\n未来几天:"
		for i, d := range parsed.Weather {
			if i == 0 || i > 2 {
				continue
			}
			out += fmt.Sprintf("\n%s: %s ~ %s°C", d.Date, d.MintempC, d.MaxtempC)
		}
	}

	// 数值字段做一次解析校验，异常时给模型一个提示而不是静默给出怪数据
	if _, err := strconv.ParseFloat(cc.TempC, 64); err != nil {
		out += "\n（注意：温度字段解析异常，数据可能不准确）"
	}

	return out, nil
}
