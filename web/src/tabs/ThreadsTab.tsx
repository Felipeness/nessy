import { useEffect, useState } from 'react'
import { api } from '../api'
import type { ThreadResp } from '../types'

type Props = { reindexCounter: number }

// ThreadsTab — visualização hierárquica project › branch › sessions.
// Espelha o TUI Threads tab mas em formato web. Cards expansíveis, dados
// agregados (sessions count, total cost, sidechain agents).
export function ThreadsTab({ reindexCounter }: Props) {
  const [threads, setThreads] = useState<ThreadResp[]>([])
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState<Set<number>>(new Set())

  useEffect(() => {
    setLoading(true)
    api
      .threads()
      .then((d) => {
        setThreads(d || [])
        setLoading(false)
      })
      .catch((e) => {
        console.error('threads load failed', e)
        setLoading(false)
      })
  }, [reindexCounter])

  if (loading) return <div className="p-6 text-[var(--color-muted)]">carregando…</div>

  // Group by project for visual hierarchy
  const byProject: Record<string, ThreadResp[]> = {}
  for (const t of threads) {
    if (!byProject[t.project_dir]) byProject[t.project_dir] = []
    byProject[t.project_dir].push(t)
  }
  const projects = Object.keys(byProject).sort()

  const toggle = (idx: number) => {
    const next = new Set(expanded)
    if (next.has(idx)) next.delete(idx)
    else next.add(idx)
    setExpanded(next)
  }

  return (
    <div className="overflow-auto h-full p-4">
      <header className="mb-4">
        <h1 className="text-lg font-bold mb-1">🌳 Threads</h1>
        <p className="text-xs text-[var(--color-muted)]">
          Sessions agrupadas por <strong>project + branch + gap ≤ 30min</strong>.
          Cada thread = continuidade de trabalho.
        </p>
      </header>

      <div className="space-y-4">
        {projects.map((proj) => (
          <div key={proj}>
            <h2 className="text-sm font-mono text-[var(--color-muted)] mb-2">
              📁 {shortenProj(proj)}
            </h2>
            <div className="space-y-2 ml-4">
              {byProject[proj].map((t) => {
                const idx = threads.indexOf(t)
                const isOpen = expanded.has(idx)
                return (
                  <ThreadCard
                    key={idx}
                    thread={t}
                    expanded={isOpen}
                    onToggle={() => toggle(idx)}
                  />
                )
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function ThreadCard({
  thread,
  expanded,
  onToggle,
}: {
  thread: ThreadResp
  expanded: boolean
  onToggle: () => void
}) {
  const branch = thread.branch || '(no branch)'
  const branchColor = colorForBranch(branch)
  const totalSubs = thread.sessions.reduce((sum, s) => sum + s.sidechain_agents, 0)

  return (
    <div className="border border-[var(--color-border)] rounded-lg bg-[var(--color-card)]">
      <button
        onClick={onToggle}
        className="w-full px-3 py-2 flex items-center gap-3 hover:bg-[var(--color-bg)]/50 rounded-t-lg text-sm font-mono text-left"
      >
        <span className="text-[var(--color-muted)]">{expanded ? '▼' : '▶'}</span>
        <span
          className="font-bold px-2 rounded"
          style={{ color: branchColor, backgroundColor: branchColor + '22' }}
        >
          {branch}
        </span>
        <span className="text-[var(--color-muted)]">
          {thread.sessions.length} sess
        </span>
        <span className="text-yellow-400">${thread.total_cost.toFixed(2)}</span>
        <span className="text-[var(--color-muted)] text-xs">
          {fmtDateRange(thread.start_time, thread.end_time)}
        </span>
        {totalSubs > 0 && (
          <span
            className="text-purple-400 font-bold text-xs px-1.5 py-0.5 bg-purple-900/30 rounded"
            title={`${totalSubs} subagents totais nessa thread`}
          >
            ↳{totalSubs}
          </span>
        )}
      </button>
      {expanded && (
        <div className="border-t border-[var(--color-border)] divide-y divide-[var(--color-border)]">
          {thread.sessions.map((s) => (
            <SessionRowMini key={s.session_id} session={s} />
          ))}
        </div>
      )}
    </div>
  )
}

function SessionRowMini({ session: s }: { session: import('../types').ThreadSessionOut }) {
  const kindIcon = s.kind === 'compact' ? '◉' : s.kind === 'first' ? '●' : '↻'
  const kindColor =
    s.kind === 'compact' ? 'text-yellow-400' : s.kind === 'first' ? 'text-green-400' : 'text-blue-400'
  const when = new Date(s.start_time).toLocaleString('pt-BR', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
  return (
    <div className="px-3 py-2 flex items-center gap-3 text-xs font-mono hover:bg-[var(--color-bg)]/30">
      <span className={kindColor}>{kindIcon}</span>
      <span className="text-[var(--color-muted)] w-28">{when}</span>
      <span className="text-[var(--color-muted)] w-12">{s.message_count}m</span>
      <span className="text-yellow-400 w-16">${s.cost_usd.toFixed(2)}</span>
      {s.gap_from_prev_secs > 0 && (
        <span className="text-[var(--color-muted)] w-16">
          +{fmtGap(s.gap_from_prev_secs)}
        </span>
      )}
      <span className="truncate text-[var(--color-fg)] flex-1">
        {s.first_user_msg}
      </span>
      {s.sidechain_agents > 0 && (
        <span className="text-purple-400 font-bold px-1.5 bg-purple-900/30 rounded">
          ↳{s.sidechain_agents}
        </span>
      )}
      <span className="text-[var(--color-muted)]">[{s.session_id.slice(0, 8)}]</span>
    </div>
  )
}

function fmtDateRange(start: string, end: string): string {
  const s = new Date(start)
  const e = new Date(end)
  const sStr = s.toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit' })
  const eStr = e.toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit' })
  if (sStr === eStr) return sStr
  return `${sStr} → ${eStr}`
}

function fmtGap(secs: number): string {
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m`
  return `${(secs / 3600).toFixed(1)}h`
}

function shortenProj(p: string): string {
  const home = '/Users/felipe.coelho'
  if (p.startsWith(home)) return '~' + p.slice(home.length)
  return p
}

function colorForBranch(branch: string): string {
  const prefix = branch.split(/[/-]/)[0]
  const colors: Record<string, string> = {
    feat: '#7dd3fc',
    fix: '#f87171',
    chore: '#9ca3af',
    refactor: '#a78bfa',
    docs: '#34d399',
    perf: '#fbbf24',
    test: '#60a5fa',
    main: '#fbbf24',
    master: '#fbbf24',
    HEAD: '#fbbf24',
  }
  return colors[prefix] || '#cbd5e1'
}
