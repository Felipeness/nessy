import { useEffect, useMemo, useState } from 'react'
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  arrayMove,
  horizontalListSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { api } from '../api'
import type {
  StatuslineComponentMeta,
  StatuslineConfig,
  StatuslineMock,
  StatuslineThemesResp,
} from '../types'

// defaultMock — alinhado com defaultMockInput() do backend.
const DEFAULT_MOCK: StatuslineMock = {
  cwd: '/Users/dev/projects/my-app',
  branch: 'feat/CC-1234-statusline',
  model: 'Opus 4.7',
  context_pct: 42,
  cost_usd: 0.32,
  lines_added: 45,
  lines_removed: 12,
  rate_5h_pct: 73,
  rate_7d_pct: 18,
  vim_mode: '',
}

// mockToInput transforma os campos editáveis no shape Input que o backend espera.
function mockToInput(m: StatuslineMock) {
  return {
    cwd: m.cwd,
    session_id: 'preview-mock',
    model: { display_name: m.model, id: 'claude-opus-4-7' },
    workspace: { current_dir: m.cwd, project_dir: m.cwd },
    context_window: { used_percentage: m.context_pct },
    cost: {
      total_cost_usd: m.cost_usd,
      total_lines_added: m.lines_added,
      total_lines_removed: m.lines_removed,
    },
    rate_limits: {
      five_hour: { used_percentage: m.rate_5h_pct },
      seven_day: { used_percentage: m.rate_7d_pct },
    },
    worktree: { branch: m.branch },
    vim: m.vim_mode ? { mode: m.vim_mode } : undefined,
  }
}

// Studio é o editor visual do statusline. Esquerda: theme + style + lista de
// linhas com components draggable. Direita: preview live (debounce 150ms).
export function StudioTab() {
  const [cfg, setCfg] = useState<StatuslineConfig | null>(null)
  const [components, setComponents] = useState<StatuslineComponentMeta[]>([])
  const [themesResp, setThemesResp] = useState<StatuslineThemesResp | null>(null)
  const [presets, setPresets] = useState<Record<string, StatuslineConfig>>({})
  const [presetNames, setPresetNames] = useState<string[]>([])
  const [previewHTML, setPreviewHTML] = useState('')
  const [saveStatus, setSaveStatus] = useState('')
  const [pickerLineIdx, setPickerLineIdx] = useState<number | null>(null)
  const [mock, setMock] = useState<StatuslineMock>(DEFAULT_MOCK)
  const [editingComponent, setEditingComponent] = useState<string | null>(null)

  // Inicial load
  useEffect(() => {
    Promise.all([
      api.statuslineConfigGet(),
      api.statuslineComponents(),
      api.statuslineThemes(),
      api.statuslinePresets(),
    ])
      .then(([c, comps, ths, pres]) => {
        setCfg(c)
        setComponents(comps)
        setThemesResp(ths)
        setPresets(pres.presets)
        setPresetNames(pres.names)
      })
      .catch((err) => setSaveStatus('erro: ' + String(err)))
  }, [])

  // Reset pra preset — pede confirmação só se cfg estiver "sujo" (não-vazio)
  const resetToPreset = (name: string) => {
    const p = presets[name]
    if (!p) return
    if (cfg && cfg.lines.some((l) => l.components.length > 0)) {
      if (!confirm(`Substituir config atual pelo preset "${name}"?`)) return
    }
    setCfg(structuredClone(p))
    setSaveStatus(`↺ resetado pro preset "${name}" — clique Salvar pra persistir`)
    setTimeout(() => setSaveStatus(''), 4000)
  }

  // Live preview com debounce — toda mudança em cfg dispara render.
  useEffect(() => {
    if (!cfg) return
    const t = setTimeout(() => {
      api
        .statuslineRender(cfg, mockToInput(mock))
        .then((r) => setPreviewHTML(r.html))
        .catch((err) => setPreviewHTML('<span class="text-red-400">error: ' + String(err) + '</span>'))
    }, 150)
    return () => clearTimeout(t)
  }, [cfg, mock])

  // Save → POST + status flash
  const save = async () => {
    if (!cfg) return
    setSaveStatus('saving…')
    try {
      const r = await api.statuslineConfigSave(cfg)
      setSaveStatus(`✓ saved → ${r.path}`)
      setTimeout(() => setSaveStatus(''), 4000)
    } catch (err) {
      setSaveStatus('erro: ' + String(err))
    }
  }

  if (!cfg || !themesResp) {
    return <div className="p-6 text-[var(--color-muted)]">carregando studio…</div>
  }

  return (
    <div className="grid grid-cols-[420px_1fr] gap-4 h-full p-4">
      {/* LEFT — config panel */}
      <div className="overflow-auto space-y-4 pr-2">
        <Section title="Theme">
          <div className="grid grid-cols-3 gap-2">
            {themesResp.themes.map((t) => (
              <ThemeCard
                key={t.name}
                theme={t}
                active={cfg.theme === t.name}
                onClick={() => setCfg({ ...cfg, theme: t.name })}
              />
            ))}
          </div>
        </Section>

        <Section title="Style">
          <div className="flex gap-2">
            {themesResp.styles.map((s) => (
              <button
                key={s}
                onClick={() => setCfg({ ...cfg, style: s })}
                className={`px-3 py-1.5 rounded text-xs border ${
                  cfg.style === s
                    ? 'bg-[var(--color-accent)] text-black border-[var(--color-accent)]'
                    : 'border-[var(--color-border)] text-[var(--color-muted)] hover:text-[var(--color-fg)]'
                }`}
              >
                {s}
              </button>
            ))}
          </div>
        </Section>

        <Section
          title="Lines"
          right={
            <button
              onClick={() =>
                setCfg({
                  ...cfg,
                  lines: [...cfg.lines, { components: [], separator: ' │ ' }],
                })
              }
              className="text-xs text-[var(--color-accent)] hover:underline"
            >
              + linha
            </button>
          }
        >
          <div className="space-y-3">
            {cfg.lines.map((line, idx) => (
              <LineEditor
                key={idx}
                idx={idx}
                line={line}
                components={components}
                onChange={(newLine) => {
                  const lines = [...cfg.lines]
                  lines[idx] = newLine
                  setCfg({ ...cfg, lines })
                }}
                onDelete={() => {
                  const lines = cfg.lines.filter((_, i) => i !== idx)
                  setCfg({ ...cfg, lines })
                }}
                onAddClick={() => setPickerLineIdx(idx)}
                onEditThreshold={setEditingComponent}
              />
            ))}
          </div>
        </Section>

        <Section title="Resetar pra preset">
          <div className="flex gap-2 flex-wrap">
            {presetNames.map((name) => (
              <button
                key={name}
                onClick={() => resetToPreset(name)}
                className="px-3 py-1.5 rounded text-xs border border-[var(--color-border)] text-[var(--color-muted)] hover:text-[var(--color-fg)] hover:border-[var(--color-fg)]"
                title={presetDescription(name)}
              >
                ↺ {name}
              </button>
            ))}
          </div>
          <div className="mt-2 text-[10px] text-[var(--color-muted)]">
            Substitui a config no editor — só persiste depois de clicar em Salvar.
          </div>
        </Section>

        <div className="flex gap-2 pt-2">
          <button
            onClick={save}
            className="px-4 py-2 rounded bg-[var(--color-accent)] text-black text-sm font-medium hover:opacity-90"
          >
            Salvar
          </button>
          {saveStatus && (
            <div className="self-center text-xs text-[var(--color-muted)]">{saveStatus}</div>
          )}
        </div>
      </div>

      {/* RIGHT — preview */}
      <div className="space-y-3">
        <Section title="Preview live">
          <div className="bg-black rounded p-4 font-mono text-sm overflow-x-auto">
            <AnsiPreview html={previewHTML} />
          </div>
          <div className="mt-2 text-xs text-[var(--color-muted)]">
            Edite os valores em "Mock data" abaixo pra simular cenários.
          </div>
        </Section>

        <Section
          title="Mock data"
          right={
            <button
              onClick={() => setMock(DEFAULT_MOCK)}
              className="text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)]"
            >
              ↺ resetar
            </button>
          }
        >
          <MockDataEditor mock={mock} onChange={setMock} />
        </Section>

        <Section title="Como instalar">
          <pre className="bg-[var(--color-card)] rounded p-3 text-xs overflow-x-auto">
            {`# 1. salvar config (botão acima)
# 2. instalar entrada no settings.json:
claude-history statusline-install --preset compact

# 3. reiniciar o Claude Code (statusLine só carrega no boot)`}
          </pre>
        </Section>

        <Section title="Catálogo de components">
          <div className="text-xs text-[var(--color-muted)] mb-2">
            {components.length} components disponíveis
          </div>
          <div className="grid grid-cols-2 gap-2">
            {components.map((c) => (
              <div
                key={c.name}
                className="border border-[var(--color-border)] rounded p-2 text-xs"
              >
                <div className="font-mono font-bold">{c.label}</div>
                <div className="text-[var(--color-muted)]">{c.description}</div>
                <div className="mt-1 flex gap-2 text-[10px] text-[var(--color-muted)]">
                  <span className="px-1 bg-[var(--color-card)] rounded">{c.category}</span>
                  {c.needs_history && <span className="text-amber-400">requer daemon</span>}
                </div>
              </div>
            ))}
          </div>
        </Section>
      </div>

      {/* Picker modal */}
      {pickerLineIdx !== null && (
        <ComponentPicker
          components={components}
          excluded={cfg.lines[pickerLineIdx].components}
          onPick={(name) => {
            const lines = [...cfg.lines]
            lines[pickerLineIdx] = {
              ...lines[pickerLineIdx],
              components: [...lines[pickerLineIdx].components, name],
            }
            setCfg({ ...cfg, lines })
            setPickerLineIdx(null)
          }}
          onClose={() => setPickerLineIdx(null)}
        />
      )}

      {/* Threshold editor modal */}
      {editingComponent && (
        <ThresholdEditor
          componentName={editingComponent}
          meta={components.find((c) => c.name === editingComponent)}
          opts={cfg.components?.[editingComponent] ?? {}}
          onSave={(opts) => {
            setCfg({
              ...cfg,
              components: { ...(cfg.components ?? {}), [editingComponent]: opts },
            })
            setEditingComponent(null)
          }}
          onClose={() => setEditingComponent(null)}
        />
      )}
    </div>
  )
}

function presetDescription(name: string): string {
  switch (name) {
    case 'compact':
      return '1 linha enxuta: cwd · git · model · context · cost'
    case 'max':
      return '2 linhas com cost_today/month, ticket, cluster, lines, time'
    case 'powerline':
      return 'estilo powerline com graphite e segmentos coloridos'
    default:
      return name
  }
}

function Section({
  title,
  right,
  children,
}: {
  title: string
  right?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section className="border border-[var(--color-border)] rounded p-3">
      <div className="flex items-center justify-between mb-2">
        <h2 className="text-xs uppercase tracking-wide text-[var(--color-muted)] font-bold">
          {title}
        </h2>
        {right}
      </div>
      {children}
    </section>
  )
}

function rgb(c: { r: number; g: number; b: number }) {
  return `rgb(${c.r}, ${c.g}, ${c.b})`
}

function ThemeCard({
  theme,
  active,
  onClick,
}: {
  theme: StatuslineThemesResp['themes'][number]
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={`p-2 rounded border text-left transition-all ${
        active
          ? 'border-[var(--color-accent)] ring-1 ring-[var(--color-accent)]'
          : 'border-[var(--color-border)] hover:border-[var(--color-fg)]'
      }`}
    >
      <div className="text-xs font-medium mb-1">{theme.name}</div>
      <div
        className="rounded px-2 py-1 text-[10px] font-mono"
        style={{ background: rgb(theme.default.bg), color: rgb(theme.default.fg) }}
      >
        sample text
      </div>
      <div className="flex gap-1 mt-1">
        <div
          className="w-3 h-3 rounded-full"
          style={{ background: rgb(theme.status.ok) }}
          title="ok"
        />
        <div
          className="w-3 h-3 rounded-full"
          style={{ background: rgb(theme.status.warn) }}
          title="warn"
        />
        <div
          className="w-3 h-3 rounded-full"
          style={{ background: rgb(theme.status.crit) }}
          title="crit"
        />
      </div>
    </button>
  )
}

function LineEditor({
  idx,
  line,
  components,
  onChange,
  onDelete,
  onAddClick,
  onEditThreshold,
}: {
  idx: number
  line: { components: string[]; separator?: string }
  components: StatuslineComponentMeta[]
  onChange: (line: { components: string[]; separator?: string }) => void
  onDelete: () => void
  onAddClick: () => void
  onEditThreshold: (name: string) => void
}) {
  const sensors = useSensors(useSensor(PointerSensor))
  const ids = line.components

  const handleDragEnd = (e: DragEndEvent) => {
    const { active, over } = e
    if (!over || active.id === over.id) return
    const oldIdx = ids.indexOf(String(active.id))
    const newIdx = ids.indexOf(String(over.id))
    onChange({ ...line, components: arrayMove(ids, oldIdx, newIdx) })
  }

  return (
    <div className="border border-[var(--color-border)] rounded p-2">
      <div className="flex items-center justify-between mb-2 text-xs">
        <span className="text-[var(--color-muted)]">linha {idx + 1}</span>
        <button onClick={onDelete} className="text-red-400 hover:underline">
          remover
        </button>
      </div>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={ids} strategy={horizontalListSortingStrategy}>
          <div className="flex flex-wrap gap-1.5 min-h-8">
            {ids.map((name) => {
              const meta = components.find((c) => c.name === name)
              return (
                <SortableChip
                  key={name}
                  name={name}
                  label={meta?.label ?? name}
                  hasWarnAt={meta?.has_warn_at ?? false}
                  onEditThreshold={() => onEditThreshold(name)}
                  onRemove={() =>
                    onChange({
                      ...line,
                      components: line.components.filter((c) => c !== name),
                    })
                  }
                />
              )
            })}
            <button
              onClick={onAddClick}
              className="px-2 py-1 rounded border border-dashed border-[var(--color-border)] text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)] hover:border-[var(--color-accent)]"
            >
              + add
            </button>
          </div>
        </SortableContext>
      </DndContext>
      <div className="mt-2 flex items-center gap-2 text-xs text-[var(--color-muted)]">
        <label>separator:</label>
        <input
          type="text"
          value={line.separator ?? ' │ '}
          onChange={(e) => onChange({ ...line, separator: e.target.value })}
          className="bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-0.5 font-mono text-xs w-20"
        />
      </div>
    </div>
  )
}

function SortableChip({
  name,
  label,
  hasWarnAt,
  onEditThreshold,
  onRemove,
}: {
  name: string
  label: string
  hasWarnAt: boolean
  onEditThreshold: () => void
  onRemove: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: name,
  })
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  }
  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      className="flex items-center gap-1 px-2 py-1 rounded bg-[var(--color-card)] border border-[var(--color-border)] text-xs cursor-grab active:cursor-grabbing"
    >
      <span>{label}</span>
      {hasWarnAt && (
        <button
          onClick={(e) => {
            e.stopPropagation()
            onEditThreshold()
          }}
          onPointerDown={(e) => e.stopPropagation()}
          className="text-[var(--color-muted)] hover:text-[var(--color-accent)]"
          title="editar thresholds (warn/critical)"
        >
          ⚙
        </button>
      )}
      <button
        onClick={(e) => {
          e.stopPropagation()
          onRemove()
        }}
        onPointerDown={(e) => e.stopPropagation()}
        className="text-[var(--color-muted)] hover:text-red-400"
      >
        ×
      </button>
    </div>
  )
}

// ThresholdEditor é um modal pra editar warn_at/critical_at de um component.
function ThresholdEditor({
  componentName,
  meta,
  opts,
  onSave,
  onClose,
}: {
  componentName: string
  meta?: StatuslineComponentMeta
  opts: { warn_at?: number; critical_at?: number; hide?: boolean }
  onSave: (opts: { warn_at?: number; critical_at?: number; hide?: boolean }) => void
  onClose: () => void
}) {
  const [warn, setWarn] = useState<string>(opts.warn_at?.toString() ?? '')
  const [crit, setCrit] = useState<string>(opts.critical_at?.toString() ?? '')

  const save = () => {
    const w = warn.trim() === '' ? undefined : Number(warn)
    const c = crit.trim() === '' ? undefined : Number(crit)
    if ((w !== undefined && Number.isNaN(w)) || (c !== undefined && Number.isNaN(c))) {
      alert('valores precisam ser numéricos')
      return
    }
    if (w !== undefined && c !== undefined && c <= w) {
      alert('critical_at precisa ser maior que warn_at')
      return
    }
    onSave({ warn_at: w, critical_at: c, hide: opts.hide })
  }

  const reset = () => {
    setWarn('')
    setCrit('')
    onSave({ hide: opts.hide })
  }

  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="bg-[var(--color-bg)] border border-[var(--color-border)] rounded-lg w-[440px] p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-3">
          <div>
            <div className="text-sm font-bold">{meta?.label ?? componentName}</div>
            <div className="text-xs text-[var(--color-muted)]">{meta?.description}</div>
          </div>
          <button onClick={onClose} className="text-[var(--color-muted)]">×</button>
        </div>
        <div className="space-y-3 text-xs">
          <p className="text-[var(--color-muted)]">
            <code>warn_at</code> = valor a partir do qual o component fica amarelo.{' '}
            <code>critical_at</code> = vermelho. Vazio = usa default do component.
          </p>
          <div>
            <label className="block mb-1 text-[var(--color-muted)]">warn_at</label>
            <input
              type="number"
              step="any"
              value={warn}
              onChange={(e) => setWarn(e.target.value)}
              placeholder="(default)"
              className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1 font-mono"
            />
          </div>
          <div>
            <label className="block mb-1 text-[var(--color-muted)]">critical_at</label>
            <input
              type="number"
              step="any"
              value={crit}
              onChange={(e) => setCrit(e.target.value)}
              placeholder="(default)"
              className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1 font-mono"
            />
          </div>
          <ThresholdHints name={componentName} />
        </div>
        <div className="flex gap-2 mt-4">
          <button
            onClick={save}
            className="flex-1 px-3 py-1.5 rounded bg-[var(--color-accent)] text-black font-medium text-sm"
          >
            Salvar
          </button>
          <button
            onClick={reset}
            className="px-3 py-1.5 rounded border border-[var(--color-border)] text-xs text-[var(--color-muted)] hover:text-[var(--color-fg)]"
          >
            ↺ default
          </button>
        </div>
      </div>
    </div>
  )
}

function ThresholdHints({ name }: { name: string }) {
  const hints: Record<string, string> = {
    context_pct: 'Default: warn=50, critical=80 (% do context window).',
    cost_session: 'Default: warn=0.8×p90, critical=1.2×p90 (multiplicador). Só age se daemon up.',
    burn_rate: 'Default: warn=1500, critical=3000 (tokens/min).',
    rate_5h: 'Default: warn=70, critical=90 (% do bloco de 5h).',
    rate_7d: 'Default: warn=70, critical=90 (% do bloco semanal).',
  }
  const h = hints[name]
  if (!h) return null
  return <p className="text-[10px] text-[var(--color-muted)]">{h}</p>
}

// MockDataEditor — form pra editar os valores que viram Input no preview.
function MockDataEditor({
  mock,
  onChange,
}: {
  mock: StatuslineMock
  onChange: (m: StatuslineMock) => void
}) {
  const set = <K extends keyof StatuslineMock>(k: K, v: StatuslineMock[K]) =>
    onChange({ ...mock, [k]: v })

  return (
    <div className="space-y-2 text-xs">
      <div className="grid grid-cols-2 gap-2">
        <Field label="cwd">
          <input
            type="text"
            value={mock.cwd}
            onChange={(e) => set('cwd', e.target.value)}
            className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1 font-mono text-xs"
          />
        </Field>
        <Field label="branch">
          <input
            type="text"
            value={mock.branch}
            onChange={(e) => set('branch', e.target.value)}
            className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1 font-mono text-xs"
          />
        </Field>
        <Field label="model">
          <input
            type="text"
            value={mock.model}
            onChange={(e) => set('model', e.target.value)}
            className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1 font-mono text-xs"
          />
        </Field>
        <Field label="vim mode">
          <select
            value={mock.vim_mode}
            onChange={(e) => set('vim_mode', e.target.value as StatuslineMock['vim_mode'])}
            className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1"
          >
            <option value="">(off)</option>
            <option value="NORMAL">NORMAL</option>
            <option value="INSERT">INSERT</option>
          </select>
        </Field>
      </div>

      <SliderField
        label="context %"
        value={mock.context_pct}
        min={0}
        max={100}
        step={1}
        onChange={(v) => set('context_pct', v)}
      />
      <SliderField
        label="cost USD"
        value={mock.cost_usd}
        min={0}
        max={5}
        step={0.01}
        onChange={(v) => set('cost_usd', v)}
      />

      <div className="grid grid-cols-2 gap-2">
        <SliderField
          label="rate 5h %"
          value={mock.rate_5h_pct}
          min={0}
          max={100}
          step={1}
          onChange={(v) => set('rate_5h_pct', v)}
        />
        <SliderField
          label="rate 7d %"
          value={mock.rate_7d_pct}
          min={0}
          max={100}
          step={1}
          onChange={(v) => set('rate_7d_pct', v)}
        />
        <Field label="lines added">
          <input
            type="number"
            min={0}
            value={mock.lines_added}
            onChange={(e) => set('lines_added', Number(e.target.value))}
            className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1 font-mono"
          />
        </Field>
        <Field label="lines removed">
          <input
            type="number"
            min={0}
            value={mock.lines_removed}
            onChange={(e) => set('lines_removed', Number(e.target.value))}
            className="w-full bg-[var(--color-card)] border border-[var(--color-border)] rounded px-2 py-1 font-mono"
          />
        </Field>
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block mb-1 text-[10px] text-[var(--color-muted)] uppercase tracking-wide">
        {label}
      </label>
      {children}
    </div>
  )
}

function SliderField({
  label,
  value,
  min,
  max,
  step,
  onChange,
}: {
  label: string
  value: number
  min: number
  max: number
  step: number
  onChange: (v: number) => void
}) {
  return (
    <div>
      <div className="flex justify-between text-[10px] text-[var(--color-muted)] uppercase tracking-wide mb-1">
        <span>{label}</span>
        <span className="font-mono normal-case tracking-normal text-[var(--color-fg)]">
          {step < 1 ? value.toFixed(2) : value}
        </span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-full"
      />
    </div>
  )
}

function ComponentPicker({
  components,
  excluded,
  onPick,
  onClose,
}: {
  components: StatuslineComponentMeta[]
  excluded: string[]
  onPick: (name: string) => void
  onClose: () => void
}) {
  const [filter, setFilter] = useState('')
  const available = useMemo(
    () =>
      components.filter(
        (c) =>
          !excluded.includes(c.name) &&
          (filter === '' ||
            c.label.toLowerCase().includes(filter.toLowerCase()) ||
            c.name.toLowerCase().includes(filter.toLowerCase()) ||
            c.description.toLowerCase().includes(filter.toLowerCase())),
      ),
    [components, excluded, filter],
  )
  const grouped = useMemo(() => {
    const map: Record<string, StatuslineComponentMeta[]> = {}
    for (const c of available) {
      ;(map[c.category] = map[c.category] ?? []).push(c)
    }
    return map
  }, [available])

  return (
    <div
      className="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="bg-[var(--color-bg)] border border-[var(--color-border)] rounded-lg w-[600px] max-h-[80vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-3 border-b border-[var(--color-border)] flex items-center gap-2">
          <input
            autoFocus
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="filtrar components…"
            className="flex-1 bg-[var(--color-card)] border border-[var(--color-border)] rounded px-3 py-1.5 text-sm"
          />
          <button onClick={onClose} className="text-[var(--color-muted)] hover:text-[var(--color-fg)]">
            ×
          </button>
        </div>
        <div className="p-3 overflow-auto flex-1 space-y-3">
          {Object.entries(grouped).map(([cat, items]) => (
            <div key={cat}>
              <div className="text-[10px] uppercase text-[var(--color-muted)] mb-1 tracking-wide">
                {cat}
              </div>
              <div className="grid grid-cols-2 gap-2">
                {items.map((c) => (
                  <button
                    key={c.name}
                    onClick={() => onPick(c.name)}
                    className="text-left p-2 rounded border border-[var(--color-border)] hover:border-[var(--color-accent)]"
                  >
                    <div className="text-xs font-mono font-bold">{c.label}</div>
                    <div className="text-[10px] text-[var(--color-muted)] mt-0.5">
                      {c.description}
                    </div>
                  </button>
                ))}
              </div>
            </div>
          ))}
          {available.length === 0 && (
            <div className="text-center text-[var(--color-muted)] py-8">
              {filter ? 'nenhum component com esse filtro' : 'todos components já em uso'}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// AnsiPreview renderiza o HTML que vem pronto do backend Go (engine único —
// AnsiToHTML em internal/statusline/html.go). Frontend não converte nada.
function AnsiPreview({ html }: { html: string }) {
  if (!html) return <span className="text-[var(--color-muted)]">renderizando…</span>
  return <span dangerouslySetInnerHTML={{ __html: html }} />
}
