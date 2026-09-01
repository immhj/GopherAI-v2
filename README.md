# GopherAI

一个用 Go + Vue 3 写的 AI 对话应用。后端基于 Gin，前端基于 Vue 3 + Element Plus，整套服务用 Docker Compose 一键拉起。

对话支持**多模型切换**、**流式输出**、**图片理解**、**文档知识库检索（RAG）**，以及**由模型自主决定何时调用工具**的 agent 能力。

## 功能

- **邮箱注册 / 登录** —— 注册时邮件发送验证码（验证码存 Redis，2 分钟有效），JWT 鉴权
- **多模型切换** —— 模型清单由后端下发，前端下拉直接选真实模型（如 `claude-opus-5`、`gpt-5.6-sol`）
- **流式输出** —— SSE 逐字返回，且贯穿整个工具调用过程
- **图片理解** —— 对话里直接附带图片，作为多模态消息发给视觉模型
- **文档知识库（RAG）** —— 上传 .md / .txt，自动切块 + 向量化 + 存入向量库；提问时**由模型自己判断**要不要检索
- **Agent 工具调用** —— 工具清单随请求声明给模型，模型决定调不调、调哪个、参数怎么填；后端执行后回灌结果，循环至给出答案
- **多会话管理** —— 会话列表、历史记录，消息经 RabbitMQ 异步落库
- **自动会话命名** —— 新会话由模型把首个提问概括成几个字作为标题，与回答并发生成，不拖慢首字

## 技术栈

| 层 | 技术 |
|---|---|
| 前端 | Vue 3、Vue Router、Element Plus、Axios、nginx |
| 后端 | Go、Gin、GORM、golang-jwt |
| 存储 | MySQL 8（业务数据）、Redis（验证码）、Qdrant（向量） |
| 消息队列 | RabbitMQ（消息异步持久化） |
| 模型接入 | 任意 OpenAI 兼容网关（对话）、火山方舟 Ark（向量化） |
| 部署 | Docker、Docker Compose |

后端只有 9 个直接依赖，没有引入任何 LLM 框架或向量库 SDK，模型和向量库都是直连 HTTP。

## 架构

```
浏览器
  │
  ▼
nginx (前端静态资源 + /api 反向代理，SSE 透传)
  │
  ▼
Go / Gin 后端 ──► MySQL      用户、会话、消息、文档元信息
  │              ├► Redis     验证码
  │              ├► RabbitMQ  消息异步落库
  │              └► Qdrant    文档向量
  │
  ├──► OpenAI 兼容网关 ──► claude / gpt / ...   对话
  └──► 火山方舟 Ark                              文档向量化
```

## 快速开始

**前置条件**：Docker、Docker Compose、一个 OpenAI 兼容的模型服务端点、一个火山方舟 API Key（做 RAG 用）。

```bash
# 1. 克隆
git clone https://github.com/immhj/GopherAI-v2.git
cd GopherAI-v2

# 2. 准备容器配置（真实配置不入库，必须先复制模板）
cp config/config.docker.toml.example config/config.docker.toml

# 3. 填写 config/config.docker.toml
#    - emailConfig：QQ 邮箱地址 + SMTP 授权码（注册验证码要用）
#    - modelServiceConfig：你的模型网关地址与可选模型清单

# 4. 提供两个 API Key
cp .env.example .env      # 填入 ANTHROPIC_API_KEY 和 ARK_API_KEY

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
| Qdrant 面板 | http://localhost:6333/dashboard | 向量数据可视化 |
| RabbitMQ 管理台 | http://localhost:15672 | 默认 `root` / `123456` |
| RedisInsight | http://localhost:8001 | Redis 可视化 |
| MySQL | localhost:13306 | 映射到宿主机的非默认端口，避免冲突 |
| Redis | localhost:16379 | 同上 |

宿主机端口特意避开了 3306 / 6379 / 8080 等常用端口。容器之间走 Compose 内部网络，不受这些映射影响。

## 环境变量

密钥一律走环境变量，不写进配置文件。

| 变量 | 用途 | 必填 |
|---|---|---|
| `ANTHROPIC_API_KEY` | 对话模型网关的鉴权 Key | 是 |
| `ARK_API_KEY` | 火山方舟，文档向量化 | 用 RAG 则必填 |

> Windows 上如果把变量设在「用户环境变量」里，**已经打开的终端读不到**，需要新开一个终端再 `docker compose up`，否则容器里拿不到 key。

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
| POST | `/file/upload` | 上传文档（.md / .txt），同步完成切块与向量化 |
| GET | `/file/documents` | 文档列表 |
| DELETE | `/file/documents/:id` | 删除文档及其向量 |

### 流式协议

SSE 的每一帧都是 JSON 事件，避免文本里的空格和换行破坏分帧：

```
data: {"sessionId":"..."}          // 新会话建好时先下发
data: {"tool":"search_documents"}  // agent 正在调用工具
data: {"content":" world"}         // 文本增量，空格/换行原样保留
data: [DONE]
data: {"title":"Go并发协作机制"}    // 会话短标题，生成好后下发
```

`title` 在 `[DONE]` 之后下发：标题是与回答并发生成的，而 SSE 不能由两个 goroutine 并发写，
所以由写回答的那一方在流结束后统一补发。前端读到流关闭为止，因此顺序不影响处理。

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
│   ├── rag/                切块 + 向量化 + 检索的编排
│   ├── chunk/              文本切块
│   ├── embedding/          火山方舟向量化客户端
│   ├── qdrant/             向量库客户端
│   ├── mcp/                MCP 天气服务（独立程序，尚未接入）
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
- **Username** —— 系统生成的内部唯一标识，用户不可见；会话、消息、文档、上传目录都以它为归属键，所以必须永久稳定

改昵称不会影响任何归属关系，登录方式也与内部主键解耦。

### Agent：工具表是唯一的接缝

模型每次请求都会收到工具清单（名字 + 描述 + 参数 JSON Schema），**由模型自主决定**是否调用。新增能力只需往工具表注册一个条目，agent 循环本身无需改动 —— 接入 RAG 时 `runAgent` 确实一行都没改。

当前注册了 2 个工具：

| 工具 | 作用 |
|---|---|
| `get_current_time` | 查当前日期时间，可指定时区 |
| `search_documents` | 在该用户自己的文档里做语义检索 |

需要区分的是：**并非所有能力都是工具**。图片理解、会话历史、流式输出是管道内置的，模型没有选择权；只有文档检索是交给模型判断的。

### 这个循环不是经典 ReAct

形状像，机制不同。经典 ReAct 是**提示词技巧**：让模型吐出 `Thought:` / `Action:` / `Observation:` 文本，框架再解析这段文本。本项目用的是模型 API 的**原生函数调用**：

| | 经典 ReAct | 本项目 |
|---|---|---|
| 动作表达 | 模型吐文本，框架正则解析 | 结构化 `tool_calls`，带 Schema 校验 |
| Thought | 显式文本字段，可展示 | 没有独立字段，推理不可见 |
| 并行动作 | 一步一个 Action | 一步可并发多个 |
| 终止条件 | 匹配 `Final Answer:` | 不再返回 `tool_calls` 即终止 |

循环逻辑（`common/aihelper/aihelper.go` 的 `runAgent`，上限 5 轮防死循环）：

```
注入 userName 到 context（工具要用，但模型不可指定）
循环：
  ├─ 带工具清单发起流式请求，文本增量实时推给前端
  ├─ 没有 tool_calls → 本轮内容即最终答案，结束
  └─ 有 tool_calls → 逐个执行、结果作为 role=tool 消息回灌 → 下一轮
```

工具执行失败不会中断对话：错误信息作为工具结果回灌，让模型自己决定换个查法还是如实告知。

早期版本曾手搓过一套伪 ReAct（两段式 JSON 提示词 + 字符串兜底解析），在确认模型原生支持 `tool_calls` 后已移除。

### 会话标题：三重兜底

侧边栏需要能一眼看出某个会话聊了什么，所以标题由模型概括首个提问而来。模型不一定听话，
因此有三层保护，保证列表永远有可读内容：

1. 建会话时先写入**截取提问前 10 字**作为占位
2. 与回答**并发**调模型生成短标题（提示词要求不超过 5 字），不给首字增加延迟
3. 代码里**硬截断 8 字**并清掉引号标点；调用失败或超时则保留第 1 步的占位

会话列表读数据库并按创建时间倒序。早期实现从内存 map 取列表，Go 的 map 遍历顺序不固定，
会导致列表每次刷新都在跳动。

### RAG：切块和向量化是两件事

容易混为一谈，但分工明确：

- **切块**是纯文本处理，**不需要任何模型**。按 markdown 标题和段落边界优先切，目标 700 字，相邻块重叠 100 字（避免答案正好被切断在边界）。
- **向量化**才是 embedding 模型做的事，一块文本换一个向量。

检索的归属隔离是服务端强制的：每个向量都带 owner，检索时按当前登录用户过滤。**用户身份通过请求 context 传给工具，绝不作为工具参数** —— 参数可能被提示注入操纵，从而读到别人的文档。

相似度低于阈值时返回"没找到相关内容"，让模型如实说明，而不是硬套无关片段。

术语定义详见 [CONTEXT.md](./CONTEXT.md)。

## 已知局限

- **工具调用记录不落库** —— 每轮只持久化用户问题和最终答案，工具调用与结果是临时的。追问细节时模型看不到上一轮检索到了什么，可能重新检索。
- **推理过程不可见** —— 原生函数调用没有 `Thought` 字段，前端只能显示"正在调用工具 X"。
- **文档类型仅限 .md / .txt** —— PDF、DOCX 需要额外的文本抽取。
- **向量化不能批量** —— 火山方舟的多模态向量接口一次只处理一条输入，因此靠并发（默认 5 路）而非批处理，大文档索引会慢一些。
- **会话标题多花一次模型调用** —— 每个新会话一次，提示词只有几十 token，但确实是一次额外请求。
- **会话不能手动重命名** —— 标题只在创建时自动生成一次。
- **MCP 尚未接入** —— `common/mcp` 下有一个可用的天气 server，但还没注册进工具表。
- **语音合成已移除**。

## 安全提示

`config/config.toml` 与模板中的 MySQL 密码、RabbitMQ 密码、JWT key 都是**开发用弱默认值**。部署到公网前请务必全部替换，并改为从环境变量注入。JWT key 泄露意味着 token 可被伪造。

## License

MIT
