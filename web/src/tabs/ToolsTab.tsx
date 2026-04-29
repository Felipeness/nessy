import { useEffect, useState } from 'react'
import { api } from '../api'
import type { ToolDrill, ToolStat } from '../types'

type Props = { reindexCounter: number }

export function ToolsTab({ reindexCounter }: Props) {
  const [tools, setTools] = useState<ToolStat[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [drill, setDrill] = useState<ToolDrill[]>([])

  useEffect(() => {
    api.tools().then((ts) => {
      setTools(ts)
      if (ts.length > 0 && !selected) setSelected(ts[0].name)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reindexCounter])

  useEffect(() => {
    if (!selected) return
    api.toolDrill(selected).then(setDrill)
  }, [selected])

  const max = tools[0]?.total_calls || 1

  return (
    <div className="flex h-full">
      <div className="w-1/2 overflow-auto border-r border-[var(--color-border)] p-4">
        <h2 className="font-bold mb-3">🔧 Top tools globais</h2>
        <ul className="space-y-1 font-mono text-sm">
          {tools.map((t) => {
            const pct = (t.total_calls / max) * 100
            const isSel = selected === t.name
            return (
              <li key={t.name}>
                <button
                  onClick={() => setSelected(t.name)}
                  className={`w-full text-left px-3 py-2 rounded flex items-center gap-3 transition-colors ${
                    isSel
                      ? 'bg-[var(--color-card)] border border-[var(--color-accent)]'
                      : 'hover:bg-[var(--color-card)] border border-transparent'
                  }`}
                >
                  <span className="w-24 truncate">{t.name}</span>
                  <div className="flex-1 bg-[var(--color-bg)] rounded h-3 overflow-hidden">
                    <div
                      className="h-full"
                      style={{ width: `${pct}%`, background: toolColor(t.name) }}
                    />
                  </div>
                  <span className="w-12 text-right">{t.total_calls}</span>
                  <span className="w-20 text-right text-xs text-[var(--color-muted)]">
                    {t.num_sessions} sess
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      </div>
      <div className="flex-1 overflow-auto p-4">
        <h2 className="font-bold mb-3">📊 Sessions usando {selected || '…'}</h2>
        <ul className="space-y-1 font-mono text-sm">
          {drill.map((d) => (
            <li
              key={d.session.session_id}
              className="flex items-center gap-3 px-3 py-2 rounded hover:bg-[var(--color-card)]"
            >
              <span className="w-16 text-right text-[var(--color-accent)]">{d.count}×</span>
              <span className="text-[var(--color-muted)]">{d.session.session_id.slice(0, 8)}</span>
              <span className="text-[var(--color-muted)]">
                {new Date(d.session.start_time).toLocaleString('pt-BR')}
              </span>
              <span className="truncate flex-1">{d.session.project_dir}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

function toolColor(name: string): string {
  if (['Bash', 'Task', 'Skill'].includes(name)) return '#58a6ff'
  if (['Edit', 'Write', 'NotebookEdit'].includes(name)) return '#3fb950'
  if (['Read', 'Grep', 'Glob', 'ToolSearch', 'WebFetch', 'WebSearch'].includes(name))
    return '#d29922'
  return '#8b949e'
}
