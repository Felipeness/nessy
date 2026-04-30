import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { SearchResult, Session } from '../types'
import { DetailPanel } from '../components/DetailPanel'
import { ModelBadge } from '../components/ModelBadge'

type Props = { reindexCounter: number }

type Mode = 'hybrid' | 'metadata' | 'fts' | 'semantic'

function detectMode(q: string): { mode: Mode; stripped: string } {
  if (q.startsWith(':body ')) return { mode: 'fts', stripped: q.slice(6) }
  if (q.startsWith(':meta ')) return { mode: 'metadata', stripped: q.slice(6) }
  if (q.startsWith(':sim ')) return { mode: 'semantic', stripped: q.slice(5) }
  return { mode: 'hybrid', stripped: q }
}

export function SearchTab({ reindexCounter: _ }: Props) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [mode, setMode] = useState<Mode>('metadata')
  const [selected, setSelected] = useState<Session | null>(null)
  const [loading, setLoading] = useState(false)
  const [showHelp, setShowHelp] = useState(false)

  const effective = useMemo(() => detectMode(query), [query])

  useEffect(() => {
    setMode(effective.mode)
    if (!query.trim()) {
      api.sessions().then((s) => {
        setResults(s.map((session) => ({ session })))
        if (s.length > 0 && !selected) setSelected(s[0])
      })
      return
    }
    setLoading(true)
    const handle = setTimeout(() => {
      // backend detecta :body / :sim sozinho via prefixo do q.
      api
        .search(query, effective.mode === 'metadata' ? 'metadata' : 'fts')
        .then((r) => setResults(r.results || []))
        .finally(() => setLoading(false))
    }, 200)
    return () => clearTimeout(handle)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])

  return (
    <div className="flex h-full">
      <div className="w-1/2 overflow-auto border-r border-[var(--color-border)] flex flex-col">
        <div className="px-4 py-2 border-b border-[var(--color-border)] flex items-center gap-3 sticky top-0 bg-[var(--color-bg)] z-10">
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder='ex: docker   |   :sim auth refactor   |   project:claude cost:>1 since:7d   (hybrid: busca em tudo)'
            className="flex-1 px-3 py-2 rounded bg-[var(--color-card)] border border-[var(--color-border)] focus:outline-none focus:border-[var(--color-accent)] text-sm font-mono"
          />
          <button
            onClick={() => setShowHelp((v) => !v)}
            title="ajuda — operadores e exemplos"
            className="w-7 h-7 rounded-full border border-[var(--color-border)] text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)] hover:border-[var(--color-accent)]"
          >
            ?
          </button>
          <span className="text-xs text-[var(--color-muted)] whitespace-nowrap">
            mode: <ModeBadge mode={mode} />
            {results.length > 0 && ` · ${results.length}`}
          </span>
        </div>

        {showHelp && <SearchHelp />}

        {loading && <p className="p-4 text-[var(--color-muted)]">Buscando…</p>}

        {!loading && results.length === 0 && query.trim() && (
          <div className="p-6 text-center text-[var(--color-muted)] text-sm">
            <p>Nenhum match pra <code className="text-[var(--color-accent)]">{query}</code></p>
            <p className="mt-2 text-xs">
              Tenta <code>:body {effective.stripped}</code> pra full-text ou{' '}
              <code>:sim {effective.stripped}</code> pra semântica.
            </p>
          </div>
        )}

        <div className="p-2 space-y-1 flex-1">
          {results.map((r, i) => (
            <button
              key={`${r.session.session_id}-${i}`}
              onClick={() => setSelected(r.session)}
              className={`w-full text-left px-3 py-2 rounded text-sm flex items-start gap-3 transition-colors ${
                selected?.session_id === r.session.session_id
                  ? 'bg-[var(--color-card)] border border-[var(--color-accent)]'
                  : 'hover:bg-[var(--color-card)] border border-transparent'
              }`}
            >
              <ModelBadge model={r.session.model} size="sm" />
              <div className="flex-1 min-w-0">
                <p className="text-xs text-[var(--color-muted)] truncate">{r.session.project_dir}</p>
                <p className="text-sm truncate">{r.session.first_user_msg}</p>
                {r.snippet && (
                  <p
                    className="text-xs text-[var(--color-warn)] mt-1 break-words"
                    dangerouslySetInnerHTML={{ __html: highlightSnippet(r.snippet) }}
                  />
                )}
                {r.role && r.role !== 'session' && (
                  <span className="text-[10px] uppercase text-[var(--color-muted)] mt-1 inline-block">
                    match em: <span className="text-[var(--color-accent)]">{r.role}</span>
                  </span>
                )}
              </div>
            </button>
          ))}
        </div>
      </div>
      <div className="flex-1 overflow-auto">
        <DetailPanel session={selected} />
      </div>
    </div>
  )
}

function ModeBadge({ mode }: { mode: Mode }) {
  const colors: Record<Mode, string> = {
    hybrid: 'text-[var(--color-accent)]',
    metadata: 'text-blue-400',
    fts: 'text-amber-400',
    semantic: 'text-purple-400',
  }
  return <span className={colors[mode]}>{mode}</span>
}

function SearchHelp() {
  return (
    <div className="px-4 py-3 border-b border-[var(--color-border)] bg-[var(--color-card)] text-xs space-y-3">
      <div>
        <p className="font-bold text-[var(--color-fg)] mb-1">4 modos de busca</p>
        <table className="w-full text-[11px]">
          <tbody>
            <tr>
              <td className="py-1 pr-3 align-top">
                <span className="text-[var(--color-accent)]">hybrid</span>
                <span className="text-[var(--color-muted)]"> (default)</span>
              </td>
              <td className="text-[var(--color-muted)]">
                metadata + full-text combinados. Acha em path, branch, msgs, AI
                summary E também dentro do conteúdo das conversas. É o que você
                quer 99% do tempo — basta digitar.
              </td>
            </tr>
            <tr>
              <td className="py-1 pr-3 align-top">
                <code className="text-blue-400">:meta &lt;q&gt;</code>
              </td>
              <td className="text-[var(--color-muted)]">
                só metadata (sem ler conteúdo). Mais rápido pra paths/branches.
              </td>
            </tr>
            <tr>
              <td className="py-1 pr-3 align-top">
                <code className="text-amber-400">:body &lt;q&gt;</code>
              </td>
              <td className="text-[var(--color-muted)]">
                só FTS5 com ranking BM25 sobre o conteúdo das mensagens.
              </td>
            </tr>
            <tr>
              <td className="py-1 pr-3 align-top">
                <code className="text-purple-400">:sim &lt;q&gt;</code>
              </td>
              <td className="text-[var(--color-muted)]">
                semântica via embeddings — acha sessions parecidas mesmo sem
                palavra exata. Requer Ollama up.
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div>
        <p className="font-bold text-[var(--color-fg)] mb-1">Filtros (combinam com qualquer modo)</p>
        <table className="w-full text-[11px]">
          <tbody>
            <tr>
              <td className="py-0.5 pr-3"><code>project:&lt;substr&gt;</code></td>
              <td className="text-[var(--color-muted)]">só sessions cujo path contém substr</td>
            </tr>
            <tr>
              <td className="py-0.5 pr-3"><code>branch:&lt;substr&gt;</code></td>
              <td className="text-[var(--color-muted)]">filtra por git branch</td>
            </tr>
            <tr>
              <td className="py-0.5 pr-3"><code>model:&lt;substr&gt;</code></td>
              <td className="text-[var(--color-muted)]">opus, sonnet, haiku</td>
            </tr>
            <tr>
              <td className="py-0.5 pr-3"><code>since:&lt;dur&gt;</code></td>
              <td className="text-[var(--color-muted)]">7d, 24h, 30m</td>
            </tr>
            <tr>
              <td className="py-0.5 pr-3"><code>cost:&gt;N</code> ou <code>cost:&lt;N</code></td>
              <td className="text-[var(--color-muted)]">filtra por custo USD</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div>
        <p className="font-bold text-[var(--color-fg)] mb-1">Exemplos</p>
        <ul className="space-y-1 text-[11px] text-[var(--color-muted)] font-mono">
          <li><code>react</code> — substring "react" em qualquer campo</li>
          <li><code>:body decoder bug</code> — sessions com "decoder bug" no conteúdo</li>
          <li><code>:sim error handling pattern</code> — sessions semanticamente parecidas</li>
          <li><code>cost:&gt;5 since:7d</code> — sessions caras dos últimos 7 dias</li>
          <li><code>project:claude-history :body fts5</code> — full-text só no projeto X</li>
          <li><code>branch:feat/CC-1234</code> — todas sessions desse ticket</li>
        </ul>
      </div>
    </div>
  )
}

function highlightSnippet(snippet: string): string {
  return escapeHtml(snippet).replace(/\[(.+?)\]/g, '<mark>$1</mark>')
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
