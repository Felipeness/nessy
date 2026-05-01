import { useEffect, useState } from 'react'
import { api } from '../api'
import type { MetaResponse } from '../types'

type Props = { reindexCounter: number }

// MetaTab — análise cross-session pra entender padrões agregados de uso.
// 4 cards: file reuse, cost por ticket, convergence speed, loops detectados.
export function MetaTab({ reindexCounter }: Props) {
  const [data, setData] = useState<MetaResponse | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    api
      .meta()
      .then((d) => {
        setData(d)
        setLoading(false)
      })
      .catch((e) => {
        console.error('meta load failed', e)
        setLoading(false)
      })
  }, [reindexCounter])

  if (loading) {
    return <div className="p-6 text-[var(--color-muted)]">carregando…</div>
  }
  if (!data) {
    return <div className="p-6 text-[var(--color-muted)]">erro ao carregar</div>
  }

  return (
    <div className="overflow-auto h-full p-4 space-y-4">
      <header className="text-xs text-[var(--color-muted)]">
        Meta-análise cross-session — gerado em{' '}
        {new Date(data.generated_at * 1000).toLocaleString()}
      </header>

      {/* Card: File Reuse */}
      <Card
        title="🔁 Arquivos tocados em múltiplas sessions"
        subtitle="Sinal de iteração frequente — arquivos que voltam várias sessions seguidas costumam ser pontos de instabilidade ou área de foco ativo."
      >
        {!data.file_reuse || data.file_reuse.length === 0 ? (
          <Empty text="Nenhum arquivo aparece em ≥2 sessions ainda." />
        ) : (
          <table className="w-full font-mono text-xs">
            <thead className="text-[var(--color-muted)]">
              <tr>
                <th className="text-left py-1">Sessions</th>
                <th className="text-left py-1">Total ops</th>
                <th className="text-left py-1">Arquivo</th>
              </tr>
            </thead>
            <tbody>
              {data.file_reuse.map((f) => (
                <tr key={f.file_path} className="border-t border-[var(--color-border)]">
                  <td className="py-1 pr-3 text-[var(--color-accent)] font-bold">
                    {f.session_count}×
                  </td>
                  <td className="py-1 pr-3">{f.total_ops}</td>
                  <td className="py-1 truncate" title={f.file_path}>
                    {f.file_path}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {/* Card: Cost By Ticket */}
      <Card
        title="🎫 Custo por ticket Jira"
        subtitle="Branches no formato XX-1234 (ex: CC-1234) agregadas. Mostra onde o dinheiro foi por feature/bug."
      >
        {!data.cost_by_ticket || data.cost_by_ticket.length === 0 ? (
          <Empty text="Nenhuma branch com pattern de ticket detectada." />
        ) : (
          <table className="w-full font-mono text-xs">
            <thead className="text-[var(--color-muted)]">
              <tr>
                <th className="text-left py-1">Ticket</th>
                <th className="text-left py-1">Sessions</th>
                <th className="text-left py-1">Custo</th>
                <th className="text-left py-1">Branches</th>
              </tr>
            </thead>
            <tbody>
              {data.cost_by_ticket.map((t) => (
                <tr key={t.ticket} className="border-t border-[var(--color-border)]">
                  <td className="py-1 pr-3 text-[var(--color-accent)] font-bold">
                    {t.ticket}
                  </td>
                  <td className="py-1 pr-3">{t.sessions}</td>
                  <td className="py-1 pr-3">${t.cost_usd.toFixed(2)}</td>
                  <td className="py-1 text-[var(--color-muted)] truncate">
                    {t.branches.join(', ')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {/* Card: Convergence by Model */}
      <Card
        title="🎯 Convergence speed por modelo"
        subtitle="Em quantos turns o user disse 'funcionou/perfeito/resolvido' — quanto menor, mais rápido o modelo converge. Sessions sem signal não contam."
      >
        {!data.convergence_by_model || data.convergence_by_model.length === 0 ? (
          <Empty text="Nenhuma session com signal de resolução detectado." />
        ) : (
          <table className="w-full font-mono text-xs">
            <thead className="text-[var(--color-muted)]">
              <tr>
                <th className="text-left py-1">Modelo</th>
                <th className="text-right py-1">Resolvidas</th>
                <th className="text-right py-1">P50</th>
                <th className="text-right py-1">P90</th>
                <th className="text-right py-1">Total</th>
              </tr>
            </thead>
            <tbody>
              {data.convergence_by_model.map((c) => {
                const rate = c.total > 0 ? (c.resolved / c.total) * 100 : 0
                return (
                  <tr key={c.group} className="border-t border-[var(--color-border)]">
                    <td className="py-1 pr-3 text-[var(--color-accent)]">{c.group}</td>
                    <td className="py-1 pr-3 text-right">
                      {c.resolved}{' '}
                      <span className="text-[var(--color-muted)]">({rate.toFixed(0)}%)</span>
                    </td>
                    <td className="py-1 pr-3 text-right">{c.p50_turns || '—'}</td>
                    <td className="py-1 pr-3 text-right">{c.p90_turns || '—'}</td>
                    <td className="py-1 pr-3 text-right text-[var(--color-muted)]">
                      {c.total}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </Card>

      {/* Card: Loops Detected */}
      <Card
        title="🔂 Loops de tool detectados"
        subtitle="Mesma tool com input idêntico ≥3× em ≤60min. Sinal de agente preso em retry — vale revisar e considerar caching ou abort."
      >
        {!data.loops_detected || data.loops_detected.length === 0 ? (
          <Empty text="Nenhum loop detectado nos últimos dados." />
        ) : (
          <div className="space-y-2 font-mono text-xs">
            {data.loops_detected.map((h) => (
              <div
                key={`${h.session_id}-${h.input_hash}`}
                className="border-t border-[var(--color-border)] pt-2"
              >
                <div className="flex items-baseline gap-3">
                  <span
                    className={`font-bold ${
                      h.count >= 5 ? 'text-red-400' : 'text-yellow-400'
                    }`}
                  >
                    {h.count}×
                  </span>
                  <span className="text-[var(--color-fg)]">{h.tool_name}</span>
                  <span className="text-[var(--color-muted)]">
                    [{h.session_id.slice(0, 8)}]
                  </span>
                  <span className="text-[var(--color-muted)]">
                    {new Date(h.first_at).toLocaleString()}
                  </span>
                  <span className="text-[var(--color-muted)]">
                    span {fmtSecs(h.span_secs)}
                  </span>
                </div>
                <div className="text-[var(--color-muted)] italic pl-6 truncate">
                  → {h.input_preview || '(sem preview)'}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}

type CardProps = {
  title: string
  subtitle?: string
  children: React.ReactNode
}

function Card({ title, subtitle, children }: CardProps) {
  return (
    <div className="border border-[var(--color-border)] rounded-lg p-4 bg-[var(--color-card)]">
      <h2 className="font-bold mb-1">{title}</h2>
      {subtitle && (
        <p className="text-xs text-[var(--color-muted)] mb-3">{subtitle}</p>
      )}
      {children}
    </div>
  )
}

function Empty({ text }: { text: string }) {
  return <p className="text-xs text-[var(--color-muted)] italic">{text}</p>
}

function fmtSecs(s: number): string {
  if (s < 60) return `${Math.round(s)}s`
  if (s < 3600) return `${Math.round(s / 60)}m`
  return `${(s / 3600).toFixed(1)}h`
}
