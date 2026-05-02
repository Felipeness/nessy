# Nessy

> Indexa, busca e analisa todas as suas conversas do Claude Code — local, rápido, sem cloud.

Wrapper npm pro binário Go em [Felipeness/nessy](https://github.com/Felipeness/nessy).

## Instalação

```bash
npm install -g @felipeness/nessy
# ou pnpm add -g @felipeness/nessy
# ou yarn global add @felipeness/nessy
# ou bun add -g @felipeness/nessy
```

O npm instala automaticamente apenas a `optionalDependency` que casa com seu SO/arch (`@felipeness/nessy-<os>-<cpu>`), que contém o binário nativo. Sem postinstall, sem download em runtime, sem rede.

Plataformas suportadas:

- macOS (`darwin` arm64/x64)
- Linux (`linux` arm64/x64)
- Windows (`win32` arm64/x64)

> Se seu gestor de pacotes estiver com optional deps desligado (`--no-optional` / `--omit=optional`), o `nessy` vai imprimir um erro orientando a reinstalar.

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
