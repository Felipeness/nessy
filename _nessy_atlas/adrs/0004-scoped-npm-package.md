# ADR-0004: @felipeness/nessy (scoped) over nessy (unscoped)

**Status**: Retroactive (deduzido 2026-05-04)
**Confidence**: 🟢

## Context

`npm/package.json:2` usa `"@felipeness/nessy"`. Versão unscoped `nessy` foi tentada
inicialmente mas o nome está tomado por outro autor (coderaiser, v6.0.1) no npm
registry.

## Rationale 🟢

1. **`nessy` taken** — verificado em `npm view nessy` (pacote unrelated, nested
   object utility).
2. **Scope `@felipeness/`** — auto-criado quando user publica primeiro pacote scoped
   pelo seu npm account. Dele.
3. **Não conflita** — instalações via `npm install -g @felipeness/nessy`. Comando
   binário continua sendo `nessy` (CLI bin name é independente do package name).

## Trade-offs

- 🟡 Levemente menos memorável que `npm install -g nessy`, mas resolvido em docs.
- 🟢 Permite expansão futura — `@felipeness/nessy-cli`, `@felipeness/nessy-mcp`,
  etc, sem brigar por nomes top-level.

## Distribuição

- Main package: `@felipeness/nessy` (com optionalDependencies pra platform binaries)
- Platform packages: `@felipeness/nessy-{darwin,linux,win32}-{arm64,x64}` (6 total)
- npm install seleciona automaticamente a optional dep do SO/arch correto via
  `os` + `cpu` fields nos manifests.
- 🟢 Padrão esbuild-style, evita postinstall download e permite repo privado.
