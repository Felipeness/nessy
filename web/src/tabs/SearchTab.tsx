import { useEffect, useState } from 'react'
import { api } from '../api'
import type { SearchResult, Session } from '../types'
import { DetailPanel } from '../components/DetailPanel'
import { ModelBadge } from '../components/ModelBadge'

type Props = { reindexCounter: number }

export function SearchTab({ reindexCounter: _ }: Props) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [mode, setMode] = useState<'metadata' | 'fts'>('metadata')
  const [selected, setSelected] = useState<Session | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const m = query.startsWith(':body ') ? 'fts' : 'metadata'
    setMode(m)
    const q = m === 'fts' ? query.slice(6) : query
    if (!q.trim()) {
      api.sessions().then((s) => {
        setResults(s.map((session) => ({ session })))
        if (s.length > 0 && !selected) setSelected(s[0])
      })
      return
    }
    setLoading(true)
    const handle = setTimeout(() => {
      api
        .search(q, m)
        .then((r) => setResults(r.results || []))
        .finally(() => setLoading(false))
    }, 200)
    return () => clearTimeout(handle)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])

  return (
    <div className="flex h-full">
      <div className="w-1/2 overflow-auto border-r border-[var(--color-border)]">
        <div className="px-4 py-2 border-b border-[var(--color-border)] flex items-center gap-3">
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filtrar… (use :body <q> pra full-text via FTS5)"
            className="flex-1 px-3 py-2 rounded bg-[var(--color-card)] border border-[var(--color-border)] focus:outline-none focus:border-[var(--color-accent)] text-sm font-mono"
          />
          <span className="text-xs text-[var(--color-muted)]">
            mode: <span className="text-[var(--color-accent)]">{mode}</span>
            {results.length > 0 && ` · ${results.length} matches`}
          </span>
        </div>
        {loading && <p className="p-4 text-[var(--color-muted)]">Buscando…</p>}
        <div className="p-2 space-y-1">
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
                    className="text-xs text-[var(--color-warn)] mt-1"
                    dangerouslySetInnerHTML={{ __html: highlightSnippet(r.snippet) }}
                  />
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

function highlightSnippet(snippet: string): string {
  return escapeHtml(snippet).replace(/\[(.+?)\]/g, '<mark>$1</mark>')
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
