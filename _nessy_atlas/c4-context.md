# C4 Context

```mermaid
graph TB
    User[👤 Developer<br/>usa Claude Code diariamente]
    Claude[🤖 Claude Code<br/>CLI]
    OtherClaude[🤖 Outro Claude<br/>session futura]
    Ollama[🦙 Ollama<br/>local llamafile]
    Nessy[📚 Nessy<br/>indexer + explorer + spec gen]
    JSONLs[(📄 ~/.claude/projects/<br/>session JSONL files)]

    User -->|escreve código,<br/>conversa| Claude
    Claude -->|append| JSONLs
    Nessy -->|read+parse| JSONLs
    User -->|nessy tui /<br/>nessy serve /<br/>nessy ask| Nessy
    Nessy -->|gen, embed, chat| Ollama
    OtherClaude -->|MCP tools<br/>(search/ask/knowledge)| Nessy
    Claude -->|/nessy command<br/>(skill installed)| Nessy
```

🟢 Todas conexões verificadas via grep dos imports/handlers.

## Boundaries

- **User ↔ Claude**: fora do escopo nosso (Claude Code product).
- **Nessy ↔ JSONLs**: read-only, never modify.
- **Nessy ↔ Ollama**: opcional (`cfg.AI.Enabled`). Health-checked async.
- **Nessy ↔ Other Claude (MCP)**: stdio JSON-RPC. Tools são `search`, `ask`,
  `knowledge`, `insights`, `aggregated`, `project`, `standup`, `advise`. 🟢
- **Claude ↔ Nessy (skill)**: filesystem-mediated — Nessy nunca executa quando
  user roda `/nessy`; é o Claude do user que executa, lendo SKILL.md instructions.

## Non-actors

- ❌ No remote server, no SaaS, no cloud DB.
- ❌ No telemetry, no opt-in analytics.
- ❌ No multi-user, no team/org.
