import { useEffect, useState } from 'react'
import {
  Bar,
  BarChart,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { api } from '../api'
import type { Behavioral, Stats } from '../types'
import { Heatmap } from '../components/Heatmap'
import { ModelBadge } from '../components/ModelBadge'

type Props = { reindexCounter: number }

const MODEL_COLORS: Record<string, string> = {
  sonnet: '#58a6ff',
  opus: '#bc8cff',
  haiku: '#3fb950',
}

function colorFor(model: string): string {
  const m = model.toLowerCase()
  if (m.includes('sonnet')) return MODEL_COLORS.sonnet
  if (m.includes('opus')) return MODEL_COLORS.opus
  if (m.includes('haiku')) return MODEL_COLORS.haiku
  return '#8b949e'
}

export function StatsTab({ reindexCounter }: Props) {
  const [stats, setStats] = useState<Stats | null>(null)
  const [behav, setBehav] = useState<Behavioral | null>(null)

  useEffect(() => {
    api.stats().then(setStats)
    api.behavioral().then(setBehav)
  }, [reindexCounter])

  if (!stats) return <p className="p-4 text-[var(--color-muted)]">Carregando…</p>

  const monthCost = stats.month_cost
  const projection = monthCost.Projection || 0
  const today = monthCost.Today || 0

  const peakHourData = (behav?.peak_hour || []).map((v, i) => ({ hour: i, count: v }))
  const topProjectData = stats.top_projects.slice(0, 8).map((p) => ({
    project: shorten(p.project_dir, 25),
    cost: p.cost_usd,
  }))
  const modelData = stats.model_distribution.map((m) => ({
    name: m.name || 'unknown',
    value: m.count,
  }))

  return (
    <div className="p-6 space-y-6">
      {/* KPI cards */}
      <section className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Kpi label="Sessions" value={stats.total_sessions.toString()} />
        <Kpi label="Mensagens" value={stats.total_msgs.toLocaleString()} />
        <Kpi
          label="Custo total"
          value={`$${stats.total_cost_usd.toFixed(2)}`}
          sub={
            stats.cache_savings_usd > 0
              ? `cache savings: $${stats.cache_savings_usd.toFixed(2)}`
              : undefined
          }
        />
        <Kpi
          label="Projeção mês"
          value={`$${projection.toFixed(2)}`}
          sub={`hoje: $${today.toFixed(2)}`}
          warn={today > 5}
          alert={today > 10}
        />
      </section>

      {/* Heatmap */}
      <section className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
        <h2 className="font-bold mb-3">🔥 Atividade por hora × dia</h2>
        <Heatmap grid={stats.heatmap} weeks={stats.heatmap_weeks} />
      </section>

      {/* Two-column charts */}
      <section className="grid md:grid-cols-2 gap-4">
        <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
          <h2 className="font-bold mb-3">🤖 Distribuição modelos</h2>
          <ResponsiveContainer width="100%" height={220}>
            <PieChart>
              <Pie
                data={modelData}
                dataKey="value"
                nameKey="name"
                cx="50%"
                cy="50%"
                outerRadius={80}
                label={(entry) => {
                  const name = (entry as { name?: string }).name ?? ''
                  const pct = (entry as { percent?: number }).percent ?? 0
                  return `${shorten(name, 12)} ${(pct * 100).toFixed(0)}%`
                }}
              >
                {modelData.map((m, i) => (
                  <Cell key={i} fill={colorFor(m.name)} />
                ))}
              </Pie>
              <Tooltip />
            </PieChart>
          </ResponsiveContainer>
        </div>

        <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
          <h2 className="font-bold mb-3">📁 Top projetos por custo</h2>
          <ResponsiveContainer width="100%" height={220}>
            <BarChart data={topProjectData} layout="vertical" margin={{ left: 80 }}>
              <XAxis type="number" stroke="#8b949e" />
              <YAxis type="category" dataKey="project" stroke="#8b949e" tick={{ fontSize: 11 }} />
              <Tooltip
                formatter={(v) => `$${(v as number).toFixed(2)}`}
                contentStyle={{ background: '#161b22', border: '1px solid #30363d' }}
              />
              <Bar dataKey="cost" fill="#58a6ff" />
            </BarChart>
          </ResponsiveContainer>
        </div>
      </section>

      {/* Behavioral */}
      {behav && (
        <section className="grid md:grid-cols-3 gap-4">
          <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
            <h3 className="font-bold mb-2">🗣️ Suas palavras mais usadas</h3>
            <ul className="text-sm space-y-0.5 font-mono">
              {behav.top_words.slice(0, 12).map((w) => (
                <li key={w.Word} className="flex justify-between">
                  <span>{w.Word}</span>
                  <span className="text-[var(--color-muted)]">{w.Count}</span>
                </li>
              ))}
            </ul>
          </div>
          <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
            <h3 className="font-bold mb-2">✏️ Como você inicia mensagens</h3>
            <ul className="text-sm space-y-0.5 font-mono">
              {behav.top_prefixes.slice(0, 12).map((w) => (
                <li key={w.Word} className="flex justify-between">
                  <span>{w.Word}</span>
                  <span className="text-[var(--color-muted)]">{w.Count}</span>
                </li>
              ))}
            </ul>
          </div>
          <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)] space-y-3">
            <div>
              <h3 className="font-bold mb-1">🔁 Sinais de retrabalho</h3>
              <p
                className={`font-mono text-sm ${
                  behav.error_rate > 0.15
                    ? 'text-red-400'
                    : behav.error_rate > 0.05
                      ? 'text-yellow-400'
                      : 'text-green-400'
                }`}
              >
                {behav.error_hits} msgs ({(behav.error_rate * 100).toFixed(0)}% de{' '}
                {behav.error_total})
              </p>
            </div>
            <div>
              <h3 className="font-bold mb-1">⏰ Horário de pico</h3>
              <ResponsiveContainer width="100%" height={100}>
                <BarChart data={peakHourData}>
                  <XAxis dataKey="hour" stroke="#8b949e" tick={{ fontSize: 9 }} />
                  <Tooltip
                    contentStyle={{ background: '#161b22', border: '1px solid #30363d' }}
                  />
                  <Bar dataKey="count" fill="#58a6ff" />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        </section>
      )}

      {/* Long-tail */}
      <section className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
        <h2 className="font-bold mb-3">🐢 Top 5 mais caras</h2>
        <table className="w-full text-sm font-mono">
          <thead>
            <tr className="text-[var(--color-muted)] text-xs">
              <th className="text-left">ID</th>
              <th className="text-left">Projeto</th>
              <th className="text-right">Msgs</th>
              <th className="text-right">Duração</th>
              <th className="text-right">Custo</th>
              <th className="text-right">Modelo</th>
            </tr>
          </thead>
          <tbody>
            {stats.long_tail_cost.map((s) => (
              <tr key={s.session_id} className="border-t border-[var(--color-border)]">
                <td className="text-[var(--color-muted)]">{s.session_id.slice(0, 8)}</td>
                <td className="truncate max-w-[40ch]">{s.project_dir}</td>
                <td className="text-right">{s.message_count}</td>
                <td className="text-right">{formatNs(s.duration_ns)}</td>
                <td className="text-right">${s.cost_usd.toFixed(2)}</td>
                <td className="text-right">
                  {/* badge inline na tabela */}
                  <span className="inline-block">
                    <ModelBadge model={"" /* placeholder, table não traz model */} size="sm" />
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  )
}

function Kpi({
  label,
  value,
  sub,
  warn,
  alert,
}: {
  label: string
  value: string
  sub?: string
  warn?: boolean
  alert?: boolean
}) {
  const color = alert ? 'text-red-400' : warn ? 'text-yellow-400' : 'text-[var(--color-fg)]'
  return (
    <div className="bg-[var(--color-card)] rounded p-4 border border-[var(--color-border)]">
      <p className="text-xs text-[var(--color-muted)] uppercase">{label}</p>
      <p className={`text-2xl font-bold font-mono mt-1 ${color}`}>{value}</p>
      {sub && <p className="text-xs text-[var(--color-muted)] mt-1">{sub}</p>}
    </div>
  )
}

function shorten(s: string, max: number): string {
  if (s.length <= max) return s
  return '…' + s.slice(s.length - (max - 1))
}

function formatNs(ns: number): string {
  const sec = ns / 1e9
  if (sec < 60) return `${sec.toFixed(0)}s`
  const m = sec / 60
  if (m < 60) return `${m.toFixed(0)}m`
  const h = m / 60
  return `${h.toFixed(1)}h`
}
