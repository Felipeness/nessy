# Nessy

> Indexa, busca e analisa todas as suas conversas do Claude Code — local, rápido, sem cloud.

Wrapper npm pro binário Go em [Felipeness/nessy](https://github.com/Felipeness/nessy).

## Instalação

```bash
npm install -g nessy
```

O `postinstall` detecta seu SO/arch e baixa o binário correto do GitHub Releases. Suporta:

- macOS (darwin amd64/arm64)
- Linux (amd64/arm64)
- Windows (amd64/arm64)

## Uso rápido

```bash
nessy tui              # TUI Bubble Tea, 10 abas
nessy serve            # Studio web em http://localhost:5555
nessy search "auth"    # busca híbrida
nessy ask "como X?"    # chat RAG sobre seu histórico
nessy --help           # tudo
```

## Documentação completa

https://github.com/Felipeness/nessy
