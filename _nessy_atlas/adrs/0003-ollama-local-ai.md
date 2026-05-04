# ADR-0003: Ollama local-only for AI (no cloud APIs)

**Status**: Retroactive (deduzido 2026-05-04)
**Confidence**: 🟢

## Context

`internal/ai/ollama.go` aponta exclusivamente pra `localhost:11434` (Ollama default).
Nenhum import de SDK de cloud AI (Anthropic, OpenAI, etc). README enfatiza
"sem mandar nada pro cloud".

## Rationale 🟢

1. **Privacy** — sessions Claude Code do user contêm contexto de código privado,
   secrets, decisões de negócio. Mandar isso pra API external = vazar IP do user.
2. **Custo zero pro user** — Claude API charging por token; Ollama roda local sem
   custo recorrente. Compatível com filosofia "ferramenta tua, não SaaS".
3. **Offline-capable** — sessions indexadas continuam acessíveis sem internet.
   Search/explore funcionam; só geração AI fica offline.
4. **Configurável** — user escolhe modelo (qualquer compatible com Ollama API).
   Default `llama3.2` ou similar — soberania sobre tradeoffs perf/qualidade.

## Trade-offs

- 🟡 Ollama precisa estar rodando localmente. Health check + cache mitiga UX
  pain quando offline.
- 🟡 Modelos locais open-source são (em geral) menos capazes que Claude/GPT-4.
  Knowledge extraction qualidade limitada por isso.
- 🟢 Compatível com os dois modos de execução: skill mode (Claude Code et al)
  usa o LLM do user; CLI mode (`nessy spec`) vai usar Ollama de graça.

## Future

Phase 2 do roadmap adiciona `nessy spec <project>` que roda o pipeline 5-fases
locally via Ollama — mesmo output que `/nessy` no Claude Code, mas sem
consumir tokens da API do user.
