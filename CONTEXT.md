# GopherAI — Domain Glossary

This file defines the core vocabulary of the project. It is a glossary, not a
spec: it says what terms *mean*, not how they are implemented.

## User identity

A **User** is identified by three distinct attributes that must not be confused:

- **Email** — the user's login credential. Unique across all users. Entered at
  both registration and login. This is the only thing a user types to sign in.

- **Nickname** — the user's chosen display name. Entered at registration and
  shown in the UI. Not unique; purely cosmetic. (Formerly the `Name` field.)

- **Username** — the system-generated, unique, internal account identifier.
  Never shown to or entered by the user. It is the stable key that ties a user
  to their data: it is carried in the JWT and used as the owner key for
  Sessions, Messages, the AIHelper manager, and the per-user uploads directory.

### Why three?

Login handle (Email), human-facing label (Nickname), and internal key
(Username) change for different reasons and have different constraints:

- Email must be unique and is user-facing but stable-ish.
- Nickname is user-facing and freely changeable, so it must never be a key.
- Username must be permanently stable and unique because everything the user
  owns is keyed on it; making it user-editable would orphan their data.

Keeping them separate means a user can change their Nickname without touching
any ownership links, and login stays decoupled from the internal key.

## AI model selection

Three orthogonal concepts, previously conflated in one "选择模型" dropdown:

- **Provider (平台)** — where a model is hosted / how it's reached. This project
  reaches all chat models through one OpenAI-compatible gateway (an "Aether"
  reverse proxy), so switching provider is not a user concern.

- **Model (模型)** — the actual LLM the user picks, e.g. `claude-opus-5` or
  `gpt-5.6-sol`. This is the only thing the model dropdown selects. The list is
  served by `GET /AI/models` from `modelServiceConfig.models`. The request
  carries the chosen model name; the backend forwards it as the `model` field.

- **Capability (能力)** — orthogonal to the model:
  - **RAG (文档问答)** — automatic: if the user has uploaded a document, the
    question is augmented with retrieved passages before hitting the model. Not
    a user-selectable mode.
  - **Vision (图像理解)** — automatic: if the message carries an image, it is
    sent as a multimodal message to the chosen (vision-capable) model. This
    replaced the old standalone ONNX image-recognition tool.
  - **Streaming** — always on; not a user choice.

## Agent and Tools

- **Tool** — a capability the model may invoke on its own: a name, a
  human-readable description, and a JSON Schema for its parameters. Tools are
  declared to the model on every request; the model decides whether to call one,
  which one, and with what arguments. The user never selects a tool.

- **Tool Registry** — the single place tools are registered. This is the seam of
  the agent: adding a capability means registering a tool, not changing the
  loop. Future MCP tools (translated from an MCP server's `tools/list`) and RAG
  (`search_documents`) become entries here.

- **Agent Loop** — the cycle that makes tools useful: send messages + tool
  declarations, and if the model responds with tool calls, execute them locally,
  feed the results back as `tool` messages, and let the model continue. Repeats
  until the model answers without requesting tools, bounded by a maximum step
  count to prevent runaway loops.

Text is streamed to the client throughout the loop, so the same code path serves
both streaming and non-streaming callers.

MCP, RAG retrieval, Ollama, and text-to-speech are not part of this version.
RAG is blocked on an embedding provider: the chat gateway does not serve
embeddings, so vectorising documents requires a separate provider.

## Captcha

A short-lived verification code emailed to a user's Email during registration.
Stored in Redis with a 2-minute expiry. Sent via QQ SMTP.
