package config

import (
	"log"

	"github.com/BurntSushi/toml"
)

type MainConfig struct {
	Port    int    `toml:"port"`
	AppName string `toml:"appName"`
	Host    string `toml:"host"`
}

type EmailConfig struct {
	Authcode string `toml:"authcode"`
	Email    string `toml:"email" `
}

type RedisConfig struct {
	RedisPort     int    `toml:"port"`
	RedisDb       int    `toml:"db"`
	RedisHost     string `toml:"host"`
	RedisPassword string `toml:"password"`
}

type MysqlConfig struct {
	MysqlPort         int    `toml:"port"`
	MysqlHost         string `toml:"host"`
	MysqlUser         string `toml:"user"`
	MysqlPassword     string `toml:"password"`
	MysqlDatabaseName string `toml:"databaseName"`
	MysqlCharset      string `toml:"charset"`
}

type JwtConfig struct {
	ExpireDuration int    `toml:"expire_duration"`
	Issuer         string `toml:"issuer"`
	Subject        string `toml:"subject"`
	Key            string `toml:"key"`
}

type Rabbitmq struct {
	RabbitmqPort     int    `toml:"port"`
	RabbitmqHost     string `toml:"host"`
	RabbitmqUsername string `toml:"username"`
	RabbitmqPassword string `toml:"password"`
	RabbitmqVhost    string `toml:"vhost"`
}

// QdrantConfig 向量数据库
type QdrantConfig struct {
	QdrantUrl        string `toml:"url"`
	QdrantCollection string `toml:"collection"`
}

// EmbeddingConfig 文档向量化（火山方舟 Ark）
// APIKey 从环境变量 ARK_API_KEY 读取，不写进配置文件。
// 注意：Ark 的多模态向量接口一次只处理一条输入，无法批量，因此需要并发控制。
type EmbeddingConfig struct {
	EmbeddingBaseUrl     string `toml:"baseUrl"`
	EmbeddingModel       string `toml:"model"`
	EmbeddingDimensions  int    `toml:"dimensions"`  // 0 表示用模型默认维度
	EmbeddingConcurrency int    `toml:"concurrency"` // 并发请求数
}

// SearchConfig 网络搜索（Tavily）。APIKey 从环境变量 TAVILY_API_KEY 读取。
type SearchConfig struct {
	SearchBaseUrl    string `toml:"baseUrl"`
	SearchMaxResults int    `toml:"maxResults"`
}

// FetchConfig 网页抓取的安全与体量限制
type FetchConfig struct {
	FetchMaxBytes       int  `toml:"maxBytes"`       // 响应体最大读取字节数
	FetchTimeoutSeconds int  `toml:"timeoutSeconds"` // 单次抓取超时
	FetchMaxChars       int  `toml:"maxChars"`       // 返回给模型的正文字符上限
	FetchAllowPrivate   bool `toml:"allowPrivate"`   // 是否允许访问内网地址（默认 false，防 SSRF）
}

// RagConfig 切块与检索参数
type RagConfig struct {
	RagChunkSize      int     `toml:"chunkSize"`
	RagChunkOverlap   int     `toml:"chunkOverlap"`
	RagTopK           int     `toml:"topK"`
	RagScoreThreshold float32 `toml:"scoreThreshold"`
}

// ModelServiceConfig 聊天模型服务配置（对接 OpenAI 兼容网关，如 Aether 反代）
//
// APIKey 不写在配置文件里，运行时从环境变量 ANTHROPIC_API_KEY 读取
type ModelServiceConfig struct {
	BaseUrl      string   `toml:"baseUrl"`      // OpenAI 兼容端点，例如 http://host.docker.internal:8085/v1
	DefaultModel string   `toml:"defaultModel"` // 默认模型
	Models       []string `toml:"models"`       // 可供前端选择的真实模型清单
}

type Config struct {
	EmailConfig        `toml:"emailConfig"`
	RedisConfig        `toml:"redisConfig"`
	MysqlConfig        `toml:"mysqlConfig"`
	JwtConfig          `toml:"jwtConfig"`
	MainConfig         `toml:"mainConfig"`
	Rabbitmq           `toml:"rabbitmqConfig"`
	ModelServiceConfig `toml:"modelServiceConfig"`
	QdrantConfig       `toml:"qdrantConfig"`
	EmbeddingConfig    `toml:"embeddingConfig"`
	RagConfig          `toml:"ragConfig"`
	SearchConfig       `toml:"searchConfig"`
	FetchConfig        `toml:"fetchConfig"`
}

type RedisKeyConfig struct {
	CaptchaPrefix string
}

var DefaultRedisKeyConfig = RedisKeyConfig{
	CaptchaPrefix: "captcha:%s",
}

var config *Config

// InitConfig 初始化项目配置
func InitConfig() error {
	// 设置配置文件路径（相对于 main.go 所在的目录）
	if _, err := toml.DecodeFile("config/config.toml", config); err != nil {
		log.Fatal(err.Error())
		return err
	}
	return nil
}

func GetConfig() *Config {
	if config == nil {
		config = new(Config)
		_ = InitConfig()
	}
	return config
}
