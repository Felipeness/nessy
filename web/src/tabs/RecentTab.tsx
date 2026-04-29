import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Session } from '../types'
import { SessionRow } from '../components/SessionRow'
import { DetailPanel } from '../components/DetailPanel'

type Props = { reindexCounter: number }

export function RecentTab({ reindexCounter }: Props) {
  const [sessions, setSessions] = useState<Session[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [groupBy, setGroupBy] = useState<'time' | 'project'>('time')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.sessions().then((s) => {
      setSessions(s)
      if (s.length > 0 && !selectedId) setSelectedId(s[0].session_id)
      setLoading(false)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reindexCounter])

  const selected = sessions.find((s) => s.session_id === selectedId) ?? null
  const groups = groupBy === 'time' ? groupByTime(sessions) : groupByProject(sessions)

  return (
    <div className="flex h-full">
      <div className="w-1/2 overflow-auto border-r border-[var(--color-border)]">
        <div className="px-4 py-2 flex items-center gap-2 border-b border-[var(--color-border)]">
          <button
            onClick={() => setGroupBy(groupBy === 'time' ? 'project' : 'time')}
            className="px-2 py-1 rounded border border-[var(--color-border)] text-xs hover:bg-[var(--color-card)]"
          >
            ⇆ Group by {groupBy === 'time' ? 'project' : 'time'}
          </button>
          <span className="text-xs text-[var(--color-muted)]">{sessions.length} sessions</span>
        </div>
        {loading && <p className="p-4 text-[var(--color-muted)]">Carregando…</p>}
        {!loading &&
          groups.map((g) => (
            <section key={g.label}>
              <h4 className="px-4 py-1 text-xs uppercase text-[var(--color-muted)] bg-[var(--color-card)] sticky top-0 z-10">
                ─── {g.label} ─────────────
              </h4>
              <div className="p-2 space-y-1">
                {g.sessions.map((s) => (
                  <SessionRow
                    key={s.session_id}
                    session={s}
                    selected={selectedId === s.session_id}
                    onClick={() => setSelectedId(s.session_id)}
                  />
                ))}
              </div>
            </section>
          ))}
      </div>
      <div className="flex-1 overflow-auto">
        <DetailPanel session={selected} />
      </div>
    </div>
  )
}

function groupByTime(sessions: Session[]): { label: string; sessions: Session[] }[] {
  const now = new Date()
  const today = startOfDay(now)
  const yesterday = startOfDay(addDays(now, -1))
  const weekAgo = startOfDay(addDays(now, -7))
  const groups: Record<string, Session[]> = {
    Today: [],
    Yesterday: [],
    'This week': [],
    Older: [],
  }
  for (const s of sessions) {
    const t = new Date(s.end_time)
    if (t >= today) groups.Today.push(s)
    else if (t >= yesterday) groups.Yesterday.push(s)
    else if (t >= weekAgo) groups['This week'].push(s)
    else groups.Older.push(s)
  }
  return Object.entries(groups)
    .filter(([, v]) => v.length > 0)
    .map(([label, ss]) => ({ label, sessions: ss }))
}

function groupByProject(sessions: Session[]): { label: string; sessions: Session[] }[] {
  const map: Record<string, Session[]> = {}
  for (const s of sessions) {
    if (!map[s.project_dir]) map[s.project_dir] = []
    map[s.project_dir].push(s)
  }
  return Object.entries(map)
    .map(([label, ss]) => ({ label: `${label} (${ss.length})`, sessions: ss }))
    .sort((a, b) => {
      const aT = new Date(a.sessions[0].end_time).getTime()
      const bT = new Date(b.sessions[0].end_time).getTime()
      return bT - aT
    })
}

function startOfDay(d: Date): Date {
  const x = new Date(d)
  x.setHours(0, 0, 0, 0)
  return x
}
function addDays(d: Date, n: number): Date {
  const x = new Date(d)
  x.setDate(x.getDate() + n)
  return x
}
