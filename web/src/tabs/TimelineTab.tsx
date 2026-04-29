import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Timeline } from '../types'
import { ModelBadge } from '../components/ModelBadge'
import { activityIcon, formatDuration } from '../components/SessionRow'

type Props = { reindexCounter: number }

export function TimelineTab({ reindexCounter }: Props) {
  const [timeline, setTimeline] = useState<Timeline | null>(null)
  const [days, setDays] = useState(7)

  useEffect(() => {
    const from = new Date()
    from.setDate(from.getDate() - days + 1)
    api.timeline(toIso(from)).then(setTimeline)
  }, [days, reindexCounter])

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center gap-3">
        <span className="text-sm text-[var(--color-muted)]">Últimos</span>
        {[7, 14, 30, 90].map((n) => (
          <button
            key={n}
            onClick={() => setDays(n)}
            className={`px-3 py-1 rounded text-sm transition-colors ${
              days === n
                ? 'bg-[var(--color-card)] text-[var(--color-accent)] border border-[var(--color-border)]'
                : 'text-[var(--color-muted)] hover:text-[var(--color-fg)]'
            }`}
          >
            {n}d
          </button>
        ))}
      </div>

      {!timeline && <p className="text-[var(--color-muted)]">Carregando…</p>}
      {timeline && timeline.days.length === 0 && (
        <p className="text-[var(--color-muted)]">Nenhuma session no período.</p>
      )}

      {timeline?.days.map((day) => (
        <section
          key={day.date}
          className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]"
        >
          <h3 className="font-bold text-[var(--color-accent)] mb-3 font-mono">{day.date}</h3>
          <ul className="space-y-2 font-mono text-sm">
            {day.sessions.map((s) => (
              <li key={s.session_id} className="flex items-center gap-3">
                <span className="text-[var(--color-muted)] w-12">
                  {new Date(s.start_time).toLocaleTimeString('pt-BR', {
                    hour: '2-digit',
                    minute: '2-digit',
                  })}
                </span>
                <span>{activityIcon(s.end_time)}</span>
                <span className="text-[var(--color-muted)]">─●─</span>
                <ModelBadge model={s.model} size="sm" />
                <span className="text-[var(--color-muted)] truncate flex-1">{s.project_dir}</span>
                <span className="text-[var(--color-muted)]">{s.message_count} msg</span>
                <span className="text-[var(--color-muted)]">
                  {formatDuration(s.start_time, s.end_time)}
                </span>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}

function toIso(d: Date): string {
  return d.toISOString().slice(0, 10)
}
