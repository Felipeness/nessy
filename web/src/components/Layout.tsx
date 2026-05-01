import type { ReactNode } from 'react'
import type { TabName } from '../types'

const TABS: { id: TabName; label: string }[] = [
  { id: 'recent', label: 'Recent' },
  { id: 'search', label: 'Search' },
  { id: 'stats', label: 'Stats' },
  { id: 'costs', label: 'Costs' },
  { id: 'timeline', label: 'Timeline' },
  { id: 'tools', label: 'Tools' },
  { id: 'behavior', label: 'Behavior' },
  { id: 'ai', label: 'AI' },
  { id: 'compare', label: 'Compare' },
  { id: 'studio', label: 'Studio' },
  { id: 'ness', label: '🧠 Ness' },
  { id: 'meta', label: '📊 Meta' },
]

type Props = {
  active: TabName
  onTabChange: (tab: TabName) => void
  status?: string
  onRefresh?: () => void
  children: ReactNode
}

export function Layout({ active, onTabChange, status, onRefresh, children }: Props) {
  return (
    <div className="flex flex-col h-screen">
      <header className="border-b border-[var(--color-border)] px-4 py-2 flex items-center gap-4">
        <h1 className="font-mono font-bold text-[var(--color-accent)]">claude-history</h1>
        <nav className="flex gap-1 flex-1">
          {TABS.map((t) => (
            <button
              key={t.id}
              onClick={() => onTabChange(t.id)}
              className={`px-3 py-1 rounded text-sm transition-colors ${
                active === t.id
                  ? 'bg-[var(--color-card)] text-[var(--color-accent)] border border-[var(--color-border)]'
                  : 'text-[var(--color-muted)] hover:text-[var(--color-fg)]'
              }`}
            >
              {t.label}
            </button>
          ))}
        </nav>
        <div className="flex items-center gap-3 text-xs text-[var(--color-muted)]">
          {status && <span>{status}</span>}
          {onRefresh && (
            <button
              onClick={onRefresh}
              className="px-2 py-1 rounded border border-[var(--color-border)] hover:bg-[var(--color-card)]"
              title="Reindex"
            >
              ↻ Refresh
            </button>
          )}
        </div>
      </header>
      <main className="flex-1 overflow-auto">{children}</main>
    </div>
  )
}
