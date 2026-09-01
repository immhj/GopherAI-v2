# GopherAI

一个用 Go + Vue 3 写的 AI 对话应用。后端基于 Gin，前端基于 Vue 3 + Element Plus，整套服务用 Docker Compose 一键拉起。

对话支持**多模型切换**、**流式输出**、**图片理解**，以及**由模型自主决定何时调用工具**的 agent 能力。

## 功能

- **邮箱注册 / 登录** —— 注册时邮件发送验证码（验证码存 Redis，2 分钟有效），JWT 鉴权
- **多模型切换** —— 模型清单由后端下发，前端下拉直接选真实模型（如 `claude-opus-5`、`gpt-5.6-sol`）
- **流式输出** —— SSE 逐字返回，默认且唯一的响应方式
- **图片理解** —— 对话里直接附带图片，作为多模态消息发给视觉模型
- **Agent 工具调用** —— 工具清单随请求声明给模型，由模型自己决定要不要调、调哪个、参数怎么填；后端执行后把结果回灌，循环直到模型给出最终答案
- **多会话管理** —— 会话列表、历史记录，消息经 RabbitMQ 异步落库

## 技术栈

| 层 | 技术 |
|---|---|
| 前端 | Vue 3、Vue Router、Element Plus、Axios、nginx |
| 后端 | Go、Gin、GORM、golang-jwt |
| 存储 | MySQL 8（业务数据）、Redis Stack（验证码 / 向量索引） |
| 消息队列 | RabbitMQ（消息异步持久化） |
| 模型接入 | 任意 OpenAI 兼容网关 |
| 部署 | Docker、Docker Compose |

## 架构

```
浏览器
  │
  ▼
nginx (前端静态资源 + /api 反向代理，SSE 透传)
  │
  ▼
Go / Gin 后端 ──► MySQL      业务数据
  │              ├► Redis     验证码、向量索引
  │              └► RabbitMQ  消息异步落库
  ▼
OpenAI 兼容网关 ──► claude / gpt / ...
```

## 快速开始

**前置条件**：Docker、Docker Compose，以及一个 OpenAI 兼容的模型服务端点。

```bash
# 1. 克隆
git clone https://github.com/immhj/GopherAI-v2.git
cd GopherAI-v2

# 2. 准备容器配置（真实配置不入库，必须先复制模板）
cp config/config.docker.toml.example config/config.docker.toml

# 3. 填写 config/config.docker.toml
#    - emailConfig：QQ 邮箱地址 + SMTP 授权码（注册验证码要用）
#    - modelServiceConfig：你的模型网关地址与可选模型清单

# 4. 提供模型网关的 API Key
cp .env.example .env      # 然后填入 ANTHROPIC_API_KEY

# 5. 启动
docker compose up -d --build
```

打开 <http://localhost:8090> 注册并开始对话。

> `config/config.docker.toml` 已被 `.gitignore` 忽略（内含真实凭证）。它是 docker-compose 的挂载来源，**不复制模板的话后端会起不来**。

### SMTP 授权码怎么拿

QQ 邮箱 → 设置 → 账号 → 开启「POP3/SMTP 服务」→ 按提示获取 16 位授权码。注意填的是**授权码，不是邮箱登录密码**。

## 服务与端口

| 服务 | 地址 | 说明 |
|---|---|---|
| 前端 | http://localhost:8090 | 应用入口 |
| 后端 API | http://localhost:9090 | Gin 服务 |
| phpMyAdmin | http://localhost:8081 | MySQL 可视化，账号 `root` / 密码见配置 |
| RabbitMQ 管理台 | http://localhost:15672 | 默认 `root` / `123456` |
| RedisInsight | http://localhost:8001 | Redis 可视化 |
| MySQL | localhost:13306 | 映射到宿主机的非默认端口，避免冲突 |
| Redis | localhost:16379 | 同上 |

宿主机端口特意避开了 3306 / 6379 / 8080 等常用端口。容器之间走 Compose 内部网络，不受这些映射影响。

## 环境变量

| 变量 | 用途 | 必填 |
|---|---|---|
| `ANTHROPIC_API_KEY` | 模型网关的鉴权 Key | 是 |
| `OPENAI_API_KEY` | RAG 向量化（embedding）凭证 | 否，本版未启用 RAG |

配置文件里不放 Key，一律走环境变量。

### 连接宿主机上的模型网关

如果网关跑在宿主机而非容器里，配置中用 `host.docker.internal`：

```toml
[modelServiceConfig]
baseUrl = "http://host.docker.internal:8085/v1"
defaultModel = "claude-opus-5"
models = ["claude-opus-5", "gpt-5.6-sol"]
```

`docker-compose.yml` 里已通过 `extra_hosts` 把该域名指向宿主网关。

## API

所有接口前缀 `/api/v1`，除注册登录外均需 `Authorization: Bearer <token>`。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/user/captcha` | 发送邮箱验证码 |
| POST | `/user/register` | 注册（邮箱 + 昵称 + 验证码 + 密码） |
| POST | `/user/login` | 登录（邮箱 + 密码） |
| GET | `/AI/models` | 可选模型清单 |
| GET | `/AI/chat/sessions` | 会话列表 |
| POST | `/AI/chat/history` | 会话历史 |
| POST | `/AI/chat/send-stream-new-session` | 新建会话并流式对话 |
| POST | `/AI/chat/send-stream` | 在已有会话中流式对话 |
| POST | `/AI/chat/send-new-session` | 新建会话并同步对话 |
| POST | `/AI/chat/send` | 在已有会话中同步对话 |
| POST | `/file/upload` | 上传文档（.md / .txt），供 RAG 使用 |

### 流式协议

SSE 的每一帧都是 JSON 事件，避免文本里的空格和换行破坏分帧：

```
data: {"sessionId":"..."}          // 新会话建好时先下发
data: {"tool":"get_current_time"}  // agent 正在调用工具
data: {"content":" world"}         // 文本增量，空格/换行原样保留
data: [DONE]
```

## 项目结构

```
├── main.go                 程序入口
├── router/                 路由注册
├── controller/             HTTP 层
├── service/                业务逻辑
├── dao/                    数据访问
├── model/                  数据模型
├── middleware/jwt/         JWT 鉴权中间件
├── common/
│   ├── aihelper/           模型客户端、工具表、agent 循环
│   ├── rag/                向量检索（本版未接入）
│   ├── mcp/                MCP 天气服务（独立程序，本版未接入）
│   ├── mysql/ redis/ rabbitmq/ email/
├── config/                 配置定义与文件
├── vue-frontend/           Vue 3 前端
└── docker-compose.yml
```

## 设计说明

### 用户身份的三个字段

刻意拆开，因为三者的约束和变更频率完全不同：

- **Email** —— 登录凭证，唯一
- **Nickname** —— 界面展示的昵称，可改、不唯一
- **Username** —— 系统生成的内部唯一标识，用户不可见；会话、消息、上传目录都以它为归属键，所以必须永久稳定

改昵称不会影响任何归属关系，登录方式也与内部主键解耦。

### 工具表是 agent 的接缝

模型每次请求都会收到工具清单（名字 + 描述 + 参数 JSON Schema），**由模型自主决定**是否调用。新增能力只需往工具表注册一个条目，agent 循环本身无需改动：

- MCP：把 MCP server 的 `tools/list` 翻译成工具声明注册进来
- RAG：注册一个 `search_documents` 工具，让模型自己判断该不该查文档

循环有最大轮数限制，防止反复调用工具停不下来。

术语定义详见 [CONTEXT.md](./CONTEXT.md)。

## 本版未包含

- **RAG 检索** —— 代码保留，但卡在 embedding：聊天网关不提供 embedding 接口，向量化需要另配服务商
- **MCP** —— `common/mcp` 下有一个可用的天气 server，但尚未接进 agent 工具表
- **语音合成** —— 已移除

## 安全提示

`config/config.toml` 与模板中的 MySQL 密码、RabbitMQ 密码、JWT key 都是**开发用弱默认值**。部署到公网前请务必全部替换，并改为从环境变量注入。JWT key 泄露意味着 token 可被伪造。

## License

MIT
