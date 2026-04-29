import { useEffect, useState } from 'react'
import {
  Bar,
  BarChart,
  Cell,
  ResponsiveContainer,
  Scatter,
  ScatterChart,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { api } from '../api'
import type { BehaviorAdvanced } from '../types'

type Props = { reindexCounter: number }

function colorFor(model: string): string {
  const m = model.toLowerCase()
  if (m.includes('sonnet')) return '#58a6ff'
  if (m.includes('opus')) return '#bc8cff'
  if (m.includes('haiku')) return '#3fb950'
  return '#8b949e'
}

export function BehaviorTab({ reindexCounter }: Props) {
  const [data, setData] = useState<BehaviorAdvanced | null>(null)

  useEffect(() => {
    api.behaviorAdvanced().then(setData)
  }, [reindexCounter])

  if (!data) return <p className="p-4 text-zinc-400">Carregando…</p>

  return (
    <div className="p-6 space-y-6">
      {/* KPI cards */}
      <section className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Kpi label="Bigrams" value={data.bigrams.length.toString()} />
        <Kpi label="Co-occurrence pairs" value={data.co_occurrences.length.toString()} />
        <Kpi
          label="High-error sessions"
          value={data.high_error_sessions.length.toString()}
          warn={data.high_error_sessions.length > 0}
        />
        <Kpi label="P90 msgs/sess" value={data.flow.P90.toString()} />
      </section>

      {/* Bigrams + Trigrams */}
      <section className="grid md:grid-cols-2 gap-4">
        <div className="bg-[#161b22] rounded p-4 border border-[#30363d]">
          <h2 className="font-bold mb-3">🔗 Bigrams (top 20)</h2>
          <ul className="space-y-1 font-mono text-sm">
            {data.bigrams.map((b, i) => (
              <li key={i} className="flex justify-between">
                <span>
                  <span className="text-zinc-100">{b.A}</span>{' '}
                  <span className="text-zinc-300">{b.B}</span>
                </span>
                <span className="text-zinc-500">{b.Count}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="bg-[#161b22] rounded p-4 border border-[#30363d]">
          <h2 className="font-bold mb-3">🔗🔗 Trigrams (top 12)</h2>
          <ul className="space-y-1 font-mono text-sm">
            {data.trigrams.map((t, i) => (
              <li key={i} className="flex justify-between">
                <span>
                  <span className="text-zinc-100">{t.A}</span>{' '}
                  <span className="text-zinc-300">{t.B}</span>{' '}
                  <span className="text-zinc-100">{t.C}</span>
                </span>
                <span className="text-zinc-500">{t.Count}</span>
              </li>
            ))}
          </ul>
        </div>
      </section>

      {/* Co-occurrence */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">🕸️ Co-occurrence (top 30 — ranqueado por PMI)</h2>
        <p className="text-xs text-zinc-500 mb-2">
          PMI alto = palavras aparecem juntas mais do que o esperado por chance
        </p>
        <table className="w-full text-sm font-mono">
          <thead>
            <tr className="text-zinc-500 text-xs">
              <th className="text-left">Par</th>
              <th className="text-right">Count</th>
              <th className="text-right">PMI</th>
            </tr>
          </thead>
          <tbody>
            {data.co_occurrences.map((c, i) => (
              <tr key={i} className="border-t border-[#30363d]">
                <td>
                  <span className="text-zinc-100">{c.A}</span>{' '}
                  <span className="text-zinc-500">↔</span>{' '}
                  <span className="text-zinc-100">{c.B}</span>
                </td>
                <td className="text-right">{c.Count}</td>
                <td className="text-right text-blue-400">{c.PMI.toFixed(2)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      {/* Time vs cost scatter */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">⏰ Hora do dia × custo</h2>
        <ResponsiveContainer width="100%" height={280}>
          <ScatterChart>
            <XAxis
              type="number"
              dataKey="hour"
              name="hora"
              domain={[0, 23]}
              ticks={[0, 4, 8, 12, 16, 20, 23]}
              stroke="#8b949e"
            />
            <YAxis type="number" dataKey="cost_usd" name="custo" stroke="#8b949e" />
            <Tooltip
              cursor={{ strokeDasharray: '3 3' }}
              contentStyle={{ background: '#0d1117', border: '1px solid #30363d' }}
              formatter={(value, name) => {
                if (name === 'cost_usd') return [`$${(value as number).toFixed(2)}`, 'custo']
                if (name === 'hour') return [`${value}h`, 'hora']
                return [value, name]
              }}
            />
            <Scatter data={data.time_cost_points} fill="#58a6ff">
              {data.time_cost_points.map((p, i) => (
                <Cell key={i} fill={colorFor(p.model)} />
              ))}
            </Scatter>
          </ScatterChart>
        </ResponsiveContainer>
      </section>

      {/* Flow histogram */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">💬 Distribuição msgs/session</h2>
        <p className="text-xs text-zinc-500 mb-2">
          p50: <span className="text-zinc-100">{data.flow.P50}</span> · p90:{' '}
          <span className="text-zinc-100">{data.flow.P90}</span> · p99:{' '}
          <span className="text-zinc-100">{data.flow.P99}</span>
        </p>
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={data.flow.Hist}>
            <XAxis dataKey="Bucket" stroke="#8b949e" />
            <YAxis stroke="#8b949e" />
            <Tooltip contentStyle={{ background: '#0d1117', border: '1px solid #30363d' }} />
            <Bar dataKey="Count" fill="#58a6ff" />
          </BarChart>
        </ResponsiveContainer>
      </section>

      {/* Style comparison */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">🆚 Você vs IA</h2>
        <table className="w-full text-sm font-mono">
          <thead>
            <tr className="text-zinc-500 text-xs">
              <th className="text-left">Métrica</th>
              <th className="text-right">Você</th>
              <th className="text-right">IA</th>
            </tr>
          </thead>
          <tbody>
            <StyleRow
              label="Avg palavras / msg"
              a={data.style.AvgWordsUser}
              b={data.style.AvgWordsAssistant}
              fmt={(n) => n.toFixed(1)}
            />
            <StyleRow
              label="Vocabulário único"
              a={data.style.UniqueWordsUser}
              b={data.style.UniqueWordsAssistant}
            />
            <StyleRow
              label="Avg sentenças / msg"
              a={data.style.AvgSentencesUser}
              b={data.style.AvgSentencesAssistant}
              fmt={(n) => n.toFixed(1)}
            />
          </tbody>
        </table>
        <div className="grid md:grid-cols-2 gap-4 mt-4">
          <div>
            <h3 className="font-bold text-xs mb-1 text-zinc-400">Suas top palavras</h3>
            <ul className="text-sm font-mono space-y-0.5">
              {data.style.TopWordsUser.map((w) => (
                <li key={w.Word} className="flex justify-between">
                  <span>{w.Word}</span>
                  <span className="text-zinc-500">{w.Count}</span>
                </li>
              ))}
            </ul>
          </div>
          <div>
            <h3 className="font-bold text-xs mb-1 text-zinc-400">Top palavras da IA</h3>
            <ul className="text-sm font-mono space-y-0.5">
              {data.style.TopWordsAssistant.map((w) => (
                <li key={w.Word} className="flex justify-between">
                  <span>{w.Word}</span>
                  <span className="text-zinc-500">{w.Count}</span>
                </li>
              ))}
            </ul>
          </div>
        </div>
      </section>

      {/* High-error sessions */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">🔁 High-error sessions (&gt;15% retrabalho)</h2>
        {data.high_error_sessions.length === 0 ? (
          <p className="text-sm text-green-400">Nenhuma — saudável 🎉</p>
        ) : (
          <ul className="space-y-1 font-mono text-sm">
            {data.high_error_sessions.map((e) => (
              <li
                key={e.session.session_id}
                className="flex items-center gap-3 px-3 py-2 rounded hover:bg-[#0d1117]"
              >
                <span className="text-yellow-400 w-12 text-right">
                  {(e.error_rate * 100).toFixed(0)}%
                </span>
                <span className="text-zinc-500">{e.session.session_id.slice(0, 8)}</span>
                <span className="text-zinc-500">
                  {e.hits}/{e.total} msgs
                </span>
                <span className="truncate flex-1">{e.session.project_dir}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}

function Kpi({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="bg-[#161b22] rounded p-4 border border-[#30363d]">
      <p className="text-xs text-zinc-500 uppercase">{label}</p>
      <p
        className={`text-2xl font-bold font-mono mt-1 ${warn ? 'text-yellow-400' : 'text-zinc-100'}`}
      >
        {value}
      </p>
    </div>
  )
}

function StyleRow({
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
  return (
    <tr className="border-t border-[#30363d]">
      <td>{label}</td>
      <td className="text-right text-blue-400">{f(a)}</td>
      <td className="text-right text-purple-400">{f(b)}</td>
    </tr>
  )
}
