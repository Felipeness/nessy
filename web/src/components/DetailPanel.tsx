import type { Cost, Session } from '../types'
import { ModelBadge } from './ModelBadge'
import { formatDuration, formatTokens } from './SessionRow'

type Props = {
  session: Session | null
  cost?: Cost | null
}

export function DetailPanel({ session: s, cost }: Props) {
  if (!s) {
    return <div className="p-6 text-[var(--color-muted)]">Selecione uma session na lista.</div>
  }

  const cacheHit =
    s.cache_read_tokens + s.input_tokens > 0
      ? s.cache_read_tokens / (s.cache_read_tokens + s.input_tokens)
      : 0
  const totalTokens =
    s.input_tokens + s.output_tokens + s.cache_creation_tokens + s.cache_read_tokens
  const minutes =
    (new Date(s.end_time).getTime() - new Date(s.start_time).getTime()) / 1000 / 60
  const msgsPerMin = minutes > 0 ? s.user_messages / minutes : 0
  const tokensPerMsg = s.message_count > 0 ? totalTokens / s.message_count : 0
  const ratio = s.assistant_messages > 0 ? s.user_messages / s.assistant_messages : 0

  const tools = Object.entries(s.tool_calls || {}).sort((a, b) => {
    if (a[1] !== b[1]) return b[1] - a[1]
    return a[0].localeCompare(b[0])
  })
  const maxTool = tools.length > 0 ? tools[0][1] : 1

  return (
    <div className="p-4 font-mono text-sm space-y-4 overflow-auto">
      <header>
        <h3 className="font-bold text-[var(--color-accent)] break-all">{s.session_id}</h3>
        <p className="text-[var(--color-muted)] truncate">{s.project_dir}</p>
        <p className="flex items-center gap-2 mt-1">
          <ModelBadge model={s.model} size="sm" />
          <span className="text-[var(--color-muted)]">{s.model || '?'}</span>
          <span className="text-[var(--color-muted)]">·</span>
          <span>{formatDuration(s.start_time, s.end_time)}</span>
          <span className="text-[var(--color-muted)]">·</span>
          <span>{s.git_branch || '-'}</span>
        </p>
      </header>

      {cost && (
        <section>
          <h4 className="font-bold text-[var(--color-accent)] mb-2">💰 Custo</h4>
          <div className="text-lg font-bold">
            ${cost.USD.toFixed(2)} USD
            {cost.BRL > 0 && (
              <span className="ml-2 text-[var(--color-muted)] text-sm">
                (R$ {cost.BRL.toFixed(2)})
              </span>
            )}
          </div>
          <div className="space-y-1 mt-2">
            {[
              ['Input', cost.InputUSD],
              ['Output', cost.OutputUSD],
              ['Cache create', cost.CacheCreationUSD],
              ['Cache read', cost.CacheReadUSD],
            ].map(([label, value]) => {
              const pct = cost.USD > 0 ? ((value as number) / cost.USD) * 100 : 0
              return (
                <div key={label as string} className="flex items-center gap-2 text-xs">
                  <span className="w-28 text-[var(--color-muted)]">{label}</span>
                  <div className="flex-1 bg-[var(--color-card)] rounded h-2 overflow-hidden">
                    <div
                      className="h-full bg-[var(--color-accent)]"
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <span className="w-20 text-right">${(value as number).toFixed(2)}</span>
                  <span className="w-12 text-right text-[var(--color-muted)]">
                    {pct.toFixed(0)}%
                  </span>
                </div>
              )
            })}
          </div>
        </section>
      )}

      <section>
        <h4 className="font-bold text-[var(--color-accent)] mb-2">🔢 Tokens</h4>
        <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
          <span className="text-[var(--color-muted)]">Input</span>
          <span className="text-right">{s.input_tokens.toLocaleString()}</span>
          <span className="text-[var(--color-muted)]">Output</span>
          <span className="text-right">{s.output_tokens.toLocaleString()}</span>
          <span className="text-[var(--color-muted)]">Cache create</span>
          <span className="text-right">{s.cache_creation_tokens.toLocaleString()}</span>
          <span className="text-[var(--color-muted)]">Cache read</span>
          <span className="text-right">{s.cache_read_tokens.toLocaleString()}</span>
        </div>
        <div className="mt-2 flex items-center gap-2 text-xs">
          <span className="w-28 text-[var(--color-muted)]">Cache hits</span>
          <div className="flex-1 bg-[var(--color-card)] rounded h-2 overflow-hidden">
            <div
              className={`h-full ${
                cacheHit > 0.6
                  ? 'bg-green-500'
                  : cacheHit > 0.3
                    ? 'bg-yellow-500'
                    : 'bg-red-500'
              }`}
              style={{ width: `${cacheHit * 100}%` }}
            />
          </div>
          <span className="w-12 text-right text-[var(--color-muted)]">
            {(cacheHit * 100).toFixed(0)}%
          </span>
        </div>
      </section>

      <section className="text-xs text-[var(--color-muted)]">
        msgs/min: <span className="text-[var(--color-fg)]">{msgsPerMin.toFixed(1)}</span> ·{' '}
        tokens/msg:{' '}
        <span className="text-[var(--color-fg)]">{formatTokens(Math.round(tokensPerMsg))}</span> ·{' '}
        u:a ratio: <span className="text-[var(--color-fg)]">{ratio.toFixed(2)}</span>
      </section>

      {tools.length > 0 && (
        <section>
          <h4 className="font-bold text-[var(--color-accent)] mb-2">🔧 Tools</h4>
          <div className="space-y-1">
            {tools.slice(0, 10).map(([name, count]) => {
              const pct = (count / maxTool) * 100
              return (
                <div key={name} className="flex items-center gap-2 text-xs">
                  <span className="w-24 truncate">{name}</span>
                  <div className="flex-1 bg-[var(--color-card)] rounded h-2 overflow-hidden">
                    <div
                      className="h-full"
                      style={{ width: `${pct}%`, background: toolColor(name) }}
                    />
                  </div>
                  <span className="w-10 text-right">{count}</span>
                </div>
              )
            })}
          </div>
        </section>
      )}

      <section className="text-xs">
        <h4 className="font-bold text-[var(--color-accent)] mb-2">💬 Primeira / Última msg</h4>
        <p className="italic text-[var(--color-muted)] mb-2">"{truncateMsg(s.first_user_msg, 200)}"</p>
        {s.last_user_msg !== s.first_user_msg && (
          <p className="italic text-[var(--color-muted)]">"{truncateMsg(s.last_user_msg, 200)}"</p>
        )}
      </section>

      <section className="pt-3 border-t border-[var(--color-border)] flex gap-2">
        <a
          href={`/api/export/${s.session_id}`}
          download
          className="px-3 py-1 rounded border border-[var(--color-border)] text-xs hover:bg-[var(--color-card)]"
        >
          ⬇ Export JSON
        </a>
      </section>
    </div>
  )
}

function truncateMsg(s: string, max: number): string {
  if (!s) return ''
  if (s.length <= max) return s
  return s.slice(0, max) + '…'
}

function toolColor(name: string): string {
  if (['Bash', 'Task', 'Skill'].includes(name)) return '#58a6ff'
  if (['Edit', 'Write', 'NotebookEdit'].includes(name)) return '#3fb950'
  if (
    ['Read', 'Grep', 'Glob', 'ToolSearch', 'WebFetch', 'WebSearch'].includes(name)
  )
    return '#d29922'
  return '#8b949e'
}
