import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { Session } from '../types'
import { ModelBadge } from '../components/ModelBadge'
import { formatDuration, formatTokens } from '../components/SessionRow'

type Props = { reindexCounter: number }

export function CompareTab({ reindexCounter: _ }: Props) {
  const [sessions, setSessions] = useState<Session[]>([])
  const [aId, setAId] = useState<string | null>(null)
  const [bId, setBId] = useState<string | null>(null)

  useEffect(() => {
    api.sessions().then((s) => {
      setSessions(s)
      if (s.length >= 2) {
        setAId(s[0].session_id)
        setBId(s[1].session_id)
      }
    })
  }, [])

  const a = useMemo(() => sessions.find((s) => s.session_id === aId), [sessions, aId])
  const b = useMemo(() => sessions.find((s) => s.session_id === bId), [sessions, bId])

  return (
    <div className="p-6 space-y-6">
      <header className="grid md:grid-cols-2 gap-4">
        <SessionPicker label="A" sessions={sessions} value={aId} onChange={setAId} />
        <SessionPicker label="B" sessions={sessions} value={bId} onChange={setBId} />
      </header>

      {a && b && (
        <section className="grid md:grid-cols-2 gap-4">
          <SummaryCard session={a} />
          <SummaryCard session={b} />
        </section>
      )}

      {a && b && (
        <section className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
          <h2 className="font-bold mb-3">Δ Comparação</h2>
          <table className="w-full text-sm font-mono">
            <thead>
              <tr className="text-[var(--color-muted)] text-xs">
                <th className="text-left">Métrica</th>
                <th className="text-right">A</th>
                <th className="text-right">B</th>
                <th className="text-right">Δ</th>
              </tr>
            </thead>
            <tbody>
              <Row label="Mensagens" a={a.message_count} b={b.message_count} />
              <Row label="User msgs" a={a.user_messages} b={b.user_messages} />
              <Row label="Assistant msgs" a={a.assistant_messages} b={b.assistant_messages} />
              <Row
                label="Input tokens"
                a={a.input_tokens}
                b={b.input_tokens}
                fmt={formatTokens}
              />
              <Row
                label="Output tokens"
                a={a.output_tokens}
                b={b.output_tokens}
                fmt={formatTokens}
              />
              <Row
                label="Duração"
                a={(new Date(a.end_time).getTime() - new Date(a.start_time).getTime()) / 1000}
                b={(new Date(b.end_time).getTime() - new Date(b.start_time).getTime()) / 1000}
                fmt={(v) => formatDuration(new Date(0).toISOString(), new Date(v * 1000).toISOString())}
              />
            </tbody>
          </table>
        </section>
      )}
    </div>
  )
}

function SessionPicker({
  label,
  sessions,
  value,
  onChange,
}: {
  label: string
  sessions: Session[]
  value: string | null
  onChange: (id: string) => void
}) {
  return (
    <div className="bg-[var(--color-card)] rounded p-3 border border-[var(--color-border)]">
      <p className="text-xs text-[var(--color-muted)] mb-1">Session {label}</p>
      <select
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
        className="w-full bg-[var(--color-bg)] border border-[var(--color-border)] rounded px-2 py-1 text-sm font-mono"
      >
        {sessions.map((s) => (
          <option key={s.session_id} value={s.session_id}>
            {s.session_id.slice(0, 8)} · {s.project_dir} · {s.first_user_msg.slice(0, 30)}
          </option>
        ))}
      </select>
    </div>
  )
}

function SummaryCard({ session: s }: { session: Session }) {
  return (
    <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)] space-y-2">
      <div className="flex items-center gap-2">
        <ModelBadge model={s.model} size="sm" />
        <span className="font-mono text-sm">{s.session_id.slice(0, 8)}</span>
      </div>
      <p className="text-xs text-[var(--color-muted)] truncate">{s.project_dir}</p>
      <p className="italic text-xs text-[var(--color-muted)]">"{s.first_user_msg}"</p>
    </div>
  )
}

function Row({
  label,
  a,
  b,
  fmt,
}: {
  label: string
  a: number
  b: number
  fmt?: (n: number) => string
}) {
  const f = fmt ?? ((n: number) => n.toString())
  const delta = b - a
  const sign = delta > 0 ? '+' : ''
  return (
    <tr className="border-t border-[var(--color-border)]">
      <td>{label}</td>
      <td className="text-right">{f(a)}</td>
      <td className="text-right">{f(b)}</td>
      <td
        className={`text-right ${
          delta > 0 ? 'text-yellow-400' : delta < 0 ? 'text-green-400' : 'text-[var(--color-muted)]'
        }`}
      >
        {sign}
        {f(Math.abs(delta))}
      </td>
    </tr>
  )
}
