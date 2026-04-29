import { useEffect, useState } from 'react'
import {
  Bar,
  BarChart,
  Brush,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { api } from '../api'
import type { Costs } from '../types'
import { ModelBadge } from '../components/ModelBadge'

type Props = { reindexCounter: number }

export function CostsTab({ reindexCounter }: Props) {
  const [costs, setCosts] = useState<Costs | null>(null)

  useEffect(() => {
    api.costs().then(setCosts)
  }, [reindexCounter])

  if (!costs) return <p className="p-4 text-[var(--color-muted)]">Carregando…</p>

  const dayData = costs.by_day.map((d) => ({ date: d.date.slice(5), cost: d.cost_usd }))
  const projData = costs.by_project.slice(0, 10).map((p) => ({
    project: shorten(p.project_dir, 30),
    cost: p.cost_usd,
  }))
  const modelData = costs.by_model.map((m) => ({
    model: m.model || 'unknown',
    cost: m.cost_usd,
  }))

  return (
    <div className="p-6 space-y-6">
      <section className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Kpi label="Mês acumulado" value={`$${costs.month_cost.Accumulated.toFixed(2)}`} />
        <Kpi label="Hoje" value={`$${costs.month_cost.Today.toFixed(2)}`} />
        <Kpi
          label="Projeção fim do mês"
          value={`$${costs.month_cost.Projection.toFixed(2)}`}
          sub={`${costs.month_cost.Days} dias no mês`}
        />
      </section>

      <section className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
        <h2 className="font-bold mb-3">📊 Custo por dia (últimos 30) — arraste o brush pra recortar</h2>
        <ResponsiveContainer width="100%" height={250}>
          <LineChart data={dayData}>
            <XAxis dataKey="date" stroke="#8b949e" tick={{ fontSize: 10 }} />
            <YAxis stroke="#8b949e" tick={{ fontSize: 10 }} />
            <Tooltip
              formatter={(v) => `$${(v as number).toFixed(2)}`}
              contentStyle={{ background: '#161b22', border: '1px solid #30363d' }}
            />
            <ReferenceLine y={5} stroke="#d29922" strokeDasharray="3 3" label="warn $5" />
            <ReferenceLine y={10} stroke="#f85149" strokeDasharray="3 3" label="alert $10" />
            <Line type="monotone" dataKey="cost" stroke="#58a6ff" strokeWidth={2} dot={false} />
            <Brush dataKey="date" height={20} stroke="#58a6ff" fill="#161b22" />
          </LineChart>
        </ResponsiveContainer>
      </section>

      <section className="grid md:grid-cols-2 gap-4">
        <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
          <h2 className="font-bold mb-3">📁 Por projeto</h2>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={projData} layout="vertical" margin={{ left: 100 }}>
              <XAxis type="number" stroke="#8b949e" />
              <YAxis type="category" dataKey="project" stroke="#8b949e" tick={{ fontSize: 10 }} />
              <Tooltip
                formatter={(v) => `$${(v as number).toFixed(2)}`}
                contentStyle={{ background: '#161b22', border: '1px solid #30363d' }}
              />
              <Bar dataKey="cost" fill="#58a6ff" />
            </BarChart>
          </ResponsiveContainer>
        </div>

        <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
          <h2 className="font-bold mb-3">🤖 Por modelo</h2>
          <div className="space-y-2 font-mono text-sm">
            {modelData.map((m) => {
              const max = modelData[0]?.cost || 1
              const pct = (m.cost / max) * 100
              return (
                <div key={m.model} className="flex items-center gap-2">
                  <ModelBadge model={m.model} size="sm" />
                  <span className="w-40 truncate">{m.model}</span>
                  <div className="flex-1 bg-[var(--color-bg)] rounded h-3 overflow-hidden">
                    <div
                      className="h-full"
                      style={{
                        width: `${pct}%`,
                        background: m.model.toLowerCase().includes('sonnet')
                          ? '#58a6ff'
                          : m.model.toLowerCase().includes('opus')
                            ? '#bc8cff'
                            : m.model.toLowerCase().includes('haiku')
                              ? '#3fb950'
                              : '#8b949e',
                      }}
                    />
                  </div>
                  <span className="w-16 text-right">${m.cost.toFixed(2)}</span>
                </div>
              )
            })}
          </div>
        </div>
      </section>

      {costs.cache_savings_usd > 0 && (
        <section className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
          <h2 className="font-bold mb-1">💾 Cache savings (30d)</h2>
          <p className="text-2xl font-bold text-green-400 font-mono">
            ${costs.cache_savings_usd.toFixed(2)}
          </p>
          <p className="text-xs text-[var(--color-muted)] mt-1">
            economia em cache hits comparado ao preço de input cheio
          </p>
        </section>
      )}
    </div>
  )
}

function Kpi({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
      <p className="text-xs text-[var(--color-muted)] uppercase">{label}</p>
      <p className="text-2xl font-bold font-mono mt-1">{value}</p>
      {sub && <p className="text-xs text-[var(--color-muted)] mt-1">{sub}</p>}
    </div>
  )
}

function shorten(s: string, max: number): string {
  if (s.length <= max) return s
  return '…' + s.slice(s.length - (max - 1))
}
