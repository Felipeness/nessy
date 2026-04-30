import { useEffect, useMemo, useState } from 'react'
import { AnsiUp } from 'ansi_up'
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
  StatuslineThemesResp,
} from '../types'

// Studio é o editor visual do statusline. Esquerda: theme + style + lista de
// linhas com components draggable. Direita: preview live (debounce 150ms).
export function StudioTab() {
  const [cfg, setCfg] = useState<StatuslineConfig | null>(null)
  const [components, setComponents] = useState<StatuslineComponentMeta[]>([])
  const [themesResp, setThemesResp] = useState<StatuslineThemesResp | null>(null)
  const [preview, setPreview] = useState('')
  const [saveStatus, setSaveStatus] = useState('')
  const [pickerLineIdx, setPickerLineIdx] = useState<number | null>(null)

  // Inicial load
  useEffect(() => {
    Promise.all([
      api.statuslineConfigGet(),
      api.statuslineComponents(),
      api.statuslineThemes(),
    ])
      .then(([c, comps, ths]) => {
        setCfg(c)
        setComponents(comps)
        setThemesResp(ths)
      })
      .catch((err) => setSaveStatus('erro: ' + String(err)))
  }, [])

  // Live preview com debounce — toda mudança em cfg dispara render.
  useEffect(() => {
    if (!cfg) return
    const t = setTimeout(() => {
      api
        .statuslineRender(cfg)
        .then((r) => setPreview(r.ansi))
        .catch((err) => setPreview('error: ' + String(err)))
    }, 150)
    return () => clearTimeout(t)
  }, [cfg])

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
              />
            ))}
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
            <AnsiPreview ansi={preview} />
          </div>
          <div className="mt-2 text-xs text-[var(--color-muted)]">
            Mock: <code>~/dev/projects/my-app</code> · branch <code>feat/CC-1234</code> · Opus 4.7 ·
            42% context · $0.32 · 5h limit 73%
          </div>
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
    </div>
  )
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
}: {
  idx: number
  line: { components: string[]; separator?: string }
  components: StatuslineComponentMeta[]
  onChange: (line: { components: string[]; separator?: string }) => void
  onDelete: () => void
  onAddClick: () => void
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
  onRemove,
}: {
  name: string
  label: string
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
      <button
        onClick={(e) => {
          e.stopPropagation()
          onRemove()
        }}
        className="text-[var(--color-muted)] hover:text-red-400"
      >
        ×
      </button>
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

// AnsiPreview converte ANSI → HTML usando ansi_up.
function AnsiPreview({ ansi }: { ansi: string }) {
  const html = useMemo(() => {
    if (!ansi) return ''
    const conv = new AnsiUp()
    conv.use_classes = false
    return conv.ansi_to_html(ansi)
  }, [ansi])
  if (!html) return <span className="text-[var(--color-muted)]">renderizando…</span>
  return <span dangerouslySetInnerHTML={{ __html: html }} />
}
