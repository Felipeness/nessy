import type { Session } from '../types'
import { ModelBadge } from './ModelBadge'

type Props = {
  session: Session
  cost?: number
  onClick?: () => void
  selected?: boolean
}

export function SessionRow({ session: s, cost, onClick, selected }: Props) {
  const dur = formatDuration(s.start_time, s.end_time)
  const tokens = formatTokens(
    s.input_tokens + s.output_tokens + s.cache_creation_tokens + s.cache_read_tokens,
  )
  const icon = activityIcon(s.end_time)
  const time = new Date(s.end_time).toLocaleTimeString('pt-BR', {
    hour: '2-digit',
    minute: '2-digit',
  })
  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-3 py-2 rounded font-mono text-sm flex items-center gap-3 transition-colors ${
        selected
          ? 'bg-[var(--color-card)] border border-[var(--color-accent)]'
          : 'hover:bg-[var(--color-card)] border border-transparent'
      }`}
    >
      <span>{icon}</span>
      <span className="text-[var(--color-muted)] w-12">{time}</span>
      <span className="text-[var(--color-muted)] w-12">{dur}</span>
      <ModelBadge model={s.model} size="sm" />
      <span className="text-[var(--color-muted)] w-16 text-right">{tokens}</span>
      <span className="text-[var(--color-fg)] w-16 text-right">
        {cost !== undefined ? `$${cost.toFixed(2)}` : '?'}
      </span>
      <span className="truncate text-[var(--color-muted)] flex-1 max-w-[40ch]">
        {truncate(s.project_dir, 35)}
      </span>
      <span className="truncate text-[var(--color-fg)] flex-1">{s.first_user_msg}</span>
      {s.sidechain_agents && s.sidechain_agents > 0 ? (
        <span
          className="text-purple-400 font-bold text-xs px-1.5 py-0.5 bg-purple-900/30 rounded"
          title={`${s.sidechain_agents} subagents · ${s.sidechain_turns} turnos`}
        >
          ↳{s.sidechain_agents}
        </span>
      ) : null}
    </button>
  )
}

export function activityIcon(endTime: string): string {
  const elapsed = Date.now() - new Date(endTime).getTime()
  if (elapsed < 5 * 60 * 1000) return '🟢'
  if (elapsed < 60 * 60 * 1000) return '🟡'
  return '⚪'
}

export function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime()
  const sec = Math.floor(ms / 1000)
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m`
  const h = Math.floor(min / 60)
  const m = min - h * 60
  return m === 0 ? `${h}h` : `${h}h${m}m`
}

export function formatTokens(n: number): string {
  if (n < 1000) return `${n}`
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

export function truncate(s: string, max: number): string {
  if (s.length <= max) return s
  return '…' + s.slice(s.length - (max - 1))
}
