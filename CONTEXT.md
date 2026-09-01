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

- **Capability (能力)** — orthogonal to the model. Two kinds, and the difference
  matters:
  - **Tool-borne** — offered to the model, which decides when to use it.
    Document retrieval works this way (`search_documents`).
  - **Pipeline-borne** — always applied, never the model's choice. Vision
    (an attached image becomes a multimodal message), conversation history, and
    streaming are all pipeline-borne.

## Agent and Tools

- **Tool** — a capability the model may invoke on its own: a name, a
  human-readable description, and a JSON Schema for its parameters. Tools are
  declared to the model on every request; the model decides whether to call one,
  which one, and with what arguments. The user never selects a tool.

- **Tool Registry** — the single place tools are registered. This is the seam of
  the agent: adding a capability means registering a tool, not changing the
  loop. Document retrieval was added this way without touching the loop at all.

- **MCP** — a *protocol* for exposing tools that live in a separate process. It
  is a **source** of tools, not a tool, and not a kind of capability. To the
  model there is no difference: it sees registry entries and cannot tell whether
  one is a local function or a proxied remote call. Implication worth recording,
  because it is routinely misunderstood: routing a tool through MCP does not
  remove that tool's need for credentials. A search tool needs a search API key
  whether it is called directly or through an MCP server.

- **Agent Loop** — the cycle that makes tools useful: send messages + tool
  declarations, and if the model responds with tool calls, execute them locally,
  feed the results back as `tool` messages, and let the model continue. Repeats
  until the model answers without requesting tools, bounded by a maximum step
  count to prevent runaway loops.

Text is streamed to the client throughout the loop, so the same code path serves
both streaming and non-streaming callers.

### This loop is not classic ReAct

Worth stating precisely, because the shapes look alike. Classic ReAct is a
*prompting* technique: the model emits `Thought:` / `Action:` / `Observation:`
as prose and the framework parses that text back out. This project uses the
model API's **native tool calling** instead. Same control flow (act, observe,
reconsider, repeat), different mechanism:

- Actions arrive as structured tool-call objects, schema-checked, not regex-parsed.
- There is no separate `Thought` field, so the reasoning trace is not something
  the UI can display.
- Several tool calls may be requested in a single step; classic ReAct takes one
  action per step.
- Termination is the absence of tool calls, not a `Final Answer:` marker.

An earlier version of this project did hand-roll a pseudo-ReAct scaffold with a
two-stage JSON prompt. It was replaced once the models were confirmed to support
native tool calling.

### Known limits of the current loop

- **Tool traffic is not persisted.** Only the user's question and the final
  answer are stored. Tool calls and their results live for one request, so a
  follow-up question cannot see what an earlier turn retrieved.
- **No visible reasoning.** Follows from native tool calling having no `Thought`
  field; the UI can only report which tool is running.

MCP, Ollama, and text-to-speech are not part of this version.

## Documents and retrieval

- **Document** — a file a user uploaded to be searchable. A user may hold many.
  Ownership is explicit and enforced server-side.

- **Chunking (切块)** — splitting a document into passages. Pure text
  processing; **no model is involved**. Worth stating because "chunking" and
  "embedding" are easily conflated: the embedding model does not chunk.

- **Embedding (向量化)** — turning one passage into a vector. This is what the
  embedding model does, and it is a separate step from chunking.

- **Chunk overlap** — passages deliberately share a margin of text with their
  neighbours, so an answer that straddles a boundary is still retrievable.

- **Vector store** — where chunk vectors live, and what is searched by semantic
  similarity. Every vector carries its owner, so retrieval is filtered to the
  asking user by the server. The user identity reaches the retrieval tool
  through request context, never as a tool parameter: a parameter could be
  steered by prompt injection into reading another user's documents.

- **Score threshold** — a similarity floor. Below it, retrieval reports nothing
  found rather than returning weak matches, so the model can honestly say the
  documents do not cover the question.

## Outbound requests made on the model's behalf

Some tools take a destination from the model and then have the server make a
request to it. That inverts the usual trust direction: the *target* of a
privileged network call becomes attacker-influencable via prompt injection.

- **Blocked range** — an address the server refuses to fetch on the model's
  behalf, because reaching it would expose internal services or cloud metadata.
  Loopback, private ranges, link-local, and CGNAT are all treated this way.
  Redirects are re-checked per hop, since a public hostname can otherwise
  redirect inward.

The general rule this reflects: **anything a tool uses to decide *who* it acts
as, or *what* it may reach, must come from the server, not from the model.**
User identity reaches retrieval through request context for the same reason.

## Captcha

A short-lived verification code emailed to a user's Email during registration.
Stored in Redis with a 2-minute expiry. Sent via QQ SMTP.
