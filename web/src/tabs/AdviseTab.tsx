import { useEffect, useState } from 'react'
import { api } from '../api'
import type { AdviseResponse, NessyConfig, NotifyConfig, Recommendation } from '../types'

type Props = { reindexCounter: number }

// AdviseTab — recomendações deterministas (rule-based) pra melhorar uso do
// Claude Code. Espelha o `nessy advise` CLI. Categorias com ícones
// reforçam a hierarquia: CLI > skill > hook > MCP.
export function AdviseTab({ reindexCounter }: Props) {
  const [data, setData] = useState<AdviseResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<string>('all')
  const [showSettings, setShowSettings] = useState(false)

  useEffect(() => {
    setLoading(true)
    api
      .advise()
      .then((d) => {
        setData(d)
        setLoading(false)
      })
      .catch((e) => {
        console.error('advise load failed', e)
        setLoading(false)
      })
  }, [reindexCounter])

  if (loading) return <div className="p-6 text-[var(--color-muted)]">analisando padrões…</div>
  if (!data) return <div className="p-6 text-[var(--color-muted)]">erro ao carregar</div>

  const recs = data.recommendations || []
  const filtered = filter === 'all' ? recs : recs.filter((r) => r.type === filter)
  const types = [...new Set(recs.map((r) => r.type))]

  return (
    <div className="overflow-auto h-full p-4 space-y-4">
      <header>
        <div className="flex items-baseline justify-between mb-1">
          <h1 className="text-lg font-bold">💡 {recs.length} recomendações</h1>
          <button
            onClick={() => setShowSettings(!showSettings)}
            className="text-xs text-[var(--color-muted)] hover:text-[var(--color-fg)]"
          >
            {showSettings ? '✕ fechar' : '⚙️ configurar notificações'}
          </button>
        </div>
        <p className="text-xs text-[var(--color-muted)] mb-3">
          Análise determinística (rule-based) sobre seus dados — sem LLM, instantâneo.
          Ordenadas por impacto. Hierarquia: <strong>CLI &gt; skill &gt; hook &gt; MCP</strong>.
        </p>
        {showSettings && <NotifySettings />}
        <div className="flex gap-2 flex-wrap">
          <FilterButton value="all" current={filter} onChange={setFilter}>
            Todas ({recs.length})
          </FilterButton>
          {types.map((t) => (
            <FilterButton key={t} value={t} current={filter} onChange={setFilter}>
              {iconFor(t)} {labelFor(t)} ({recs.filter((r) => r.type === t).length})
            </FilterButton>
          ))}
        </div>
      </header>

      {filtered.length === 0 ? (
        <div className="text-[var(--color-muted)] italic">
          Sem recomendações nessa categoria — uso parece bom!
        </div>
      ) : (
        <div className="space-y-3">
          {filtered.map((r, i) => (
            <RecommendationCard key={i} rec={r} index={i + 1} />
          ))}
        </div>
      )}
    </div>
  )
}

// NotifySettings — painel pra configurar watcher do daemon. Lê /api/config,
// envia POST com novos valores. Daemon precisa restart pra aplicar.
function NotifySettings() {
  const [cfg, setCfg] = useState<NessyConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState<string>('')

  useEffect(() => {
    api.getConfig().then(setCfg).catch((e) => setStatus('erro: ' + e.message))
  }, [])

  if (!cfg) return <div className="text-xs text-[var(--color-muted)] mb-3">carregando config…</div>

  const update = (patch: Partial<NotifyConfig>) => {
    setCfg({ ...cfg, notify: { ...cfg.notify, ...patch } })
  }

  const save = async () => {
    setSaving(true)
    try {
      const resp = await api.saveConfig(cfg)
      setStatus(resp.note || 'salvo!')
    } catch (e) {
      setStatus('erro: ' + (e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const n = cfg.notify
  const tools = ['Bash', 'Edit', 'Write', 'Read', 'MultiEdit', 'Task', 'Grep', 'Glob']

  return (
    <div className="border border-[var(--color-border)] rounded-lg p-4 bg-[var(--color-card)] mb-4 space-y-3">
      <h2 className="font-bold mb-2">⚙️ Configuração de notificações</h2>

      <label className="flex items-center gap-2 cursor-pointer">
        <input
          type="checkbox"
          checked={n.enabled}
          onChange={(e) => update({ enabled: e.target.checked })}
        />
        <span className="text-sm">
          <strong>Notificações ativas</strong>{' '}
          <span className="text-[var(--color-muted)]">— quando desativado, ainda detecta e loga, mas não dispara osascript/notify-send/toast</span>
        </span>
      </label>

      <div className="grid grid-cols-3 gap-3">
        <NumField
          label="Repetições mínimas"
          hint="≥ N pra disparar (default 3)"
          value={n.min_count}
          min={2}
          max={50}
          onChange={(v) => update({ min_count: v })}
        />
        <NumField
          label="Janela (segundos)"
          hint="N repetições em ≤ X seg (default 60)"
          value={n.window_secs}
          min={10}
          max={3600}
          onChange={(v) => update({ window_secs: v })}
        />
        <NumField
          label="Debounce (segundos)"
          hint="mesma alerta ≤ X seg = silêncio (default 30)"
          value={n.debounce_secs}
          min={5}
          max={3600}
          onChange={(v) => update({ debounce_secs: v })}
        />
      </div>

      <div>
        <label className="block text-xs text-[var(--color-muted)] mb-1">
          Whitelist de tools (vazio = todos)
        </label>
        <div className="flex gap-2 flex-wrap">
          {tools.map((t) => {
            const isIncluded = (n.include_tools || []).includes(t)
            const isExcluded = (n.exclude_tools || []).includes(t)
            const next = () => {
              if (isIncluded) {
                update({
                  include_tools: (n.include_tools || []).filter((x) => x !== t),
                  exclude_tools: [...(n.exclude_tools || []).filter((x) => x !== t), t],
                })
              } else if (isExcluded) {
                update({ exclude_tools: (n.exclude_tools || []).filter((x) => x !== t) })
              } else {
                update({ include_tools: [...(n.include_tools || []), t] })
              }
            }
            const cls = isIncluded
              ? 'bg-green-700 text-white'
              : isExcluded
                ? 'bg-red-900 text-red-300 line-through'
                : 'bg-[var(--color-bg)] text-[var(--color-muted)]'
            return (
              <button
                key={t}
                onClick={next}
                className={`px-2 py-1 rounded text-xs font-mono border border-[var(--color-border)] ${cls}`}
                title="clique alterna: incluir → excluir → indiferente"
              >
                {t}
              </button>
            )
          })}
        </div>
        <p className="text-xs text-[var(--color-muted)] mt-1">
          verde = só notifica esses · vermelho = ignora · cinza = indiferente
        </p>
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={save}
          disabled={saving}
          className="px-3 py-1 bg-[var(--color-accent)] text-black font-bold rounded text-sm disabled:opacity-50"
        >
          {saving ? 'salvando…' : '💾 Salvar'}
        </button>
        {status && <span className="text-xs text-[var(--color-muted)]">{status}</span>}
      </div>
    </div>
  )
}

function NumField({
  label,
  hint,
  value,
  min,
  max,
  onChange,
}: {
  label: string
  hint: string
  value: number
  min: number
  max: number
  onChange: (v: number) => void
}) {
  return (
    <div>
      <label className="block text-xs text-[var(--color-muted)] mb-1">
        <strong className="text-[var(--color-fg)]">{label}</strong>
      </label>
      <input
        type="number"
        value={value}
        min={min}
        max={max}
        onChange={(e) => onChange(parseInt(e.target.value) || min)}
        className="w-full px-2 py-1 bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-sm font-mono"
      />
      <span className="text-xs text-[var(--color-muted)]">{hint}</span>
    </div>
  )
}

function FilterButton({
  value,
  current,
  onChange,
  children,
}: {
  value: string
  current: string
  onChange: (v: string) => void
  children: React.ReactNode
}) {
  const active = value === current
  return (
    <button
      onClick={() => onChange(value)}
      className={`px-3 py-1 rounded text-xs font-mono ${
        active
          ? 'bg-[var(--color-accent)] text-black'
          : 'bg-[var(--color-card)] text-[var(--color-muted)] hover:text-[var(--color-fg)]'
      }`}
    >
      {children}
    </button>
  )
}

function RecommendationCard({ rec, index }: { rec: Recommendation; index: number }) {
  const conf =
    rec.confidence === 'high'
      ? 'text-green-400'
      : rec.confidence === 'medium'
        ? 'text-yellow-400'
        : 'text-[var(--color-muted)]'
  return (
    <div className="border border-[var(--color-border)] rounded-lg p-4 bg-[var(--color-card)]">
      <div className="flex items-baseline gap-3 mb-2">
        <span className="text-2xl">{iconFor(rec.type)}</span>
        <div className="flex-1">
          <h3 className="font-bold text-[var(--color-fg)]">
            {index}. {rec.title}
          </h3>
          <div className="flex gap-3 text-xs text-[var(--color-muted)]">
            <span>{labelFor(rec.type)}</span>
            <span className={conf}>confidence: {rec.confidence}</span>
            <span>score: {rec.score.toFixed(0)}</span>
          </div>
        </div>
      </div>
      <p className="text-sm text-[var(--color-fg)] mb-2">{rec.description}</p>
      <div className="bg-[var(--color-bg)] rounded p-2 mb-2">
        <p className="text-sm text-[var(--color-accent)]">→ {rec.action}</p>
      </div>
      {rec.savings && (
        <p className="text-xs text-green-400 mb-1">💰 {rec.savings}</p>
      )}
      <p className="text-xs text-[var(--color-muted)] font-mono">📎 {rec.evidence}</p>
    </div>
  )
}

function iconFor(type: string): string {
  const icons: Record<string, string> = {
    skill: '🛠️',
    hook: '🪝',
    cli: '⚡',
    model_downgrade: '💸',
    cache: '💾',
    subagent: '🌳',
    claude_md: '📝',
  }
  return icons[type] || '•'
}

function labelFor(type: string): string {
  const labels: Record<string, string> = {
    skill: 'Skill',
    hook: 'Hook',
    cli: 'CLI nativo',
    model_downgrade: 'Downgrade modelo',
    cache: 'Cache',
    subagent: 'Subagent',
    claude_md: 'CLAUDE.md',
  }
  return labels[type] || type
}
