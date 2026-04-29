type Props = {
  grid: number[][]
  weeks: number
}

const HOUR_LABELS = ['00-04', '04-08', '08-12', '12-16', '16-20', '20-24']
const DAY_LABELS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

export function Heatmap({ grid, weeks }: Props) {
  let max = 1
  for (const row of grid) for (const v of row) if (v > max) max = v
  return (
    <div className="font-mono text-xs">
      <p className="mb-2 text-[var(--color-muted)]">Atividade últimas {weeks} semanas</p>
      <div className="inline-block">
        <div className="flex gap-1 mb-1">
          <div className="w-12" />
          {DAY_LABELS.map((d) => (
            <div key={d} className="w-8 text-center text-[var(--color-muted)]">
              {d}
            </div>
          ))}
        </div>
        {grid.map((row, ri) => (
          <div key={ri} className="flex gap-1 mb-1">
            <div className="w-12 text-[var(--color-muted)]">{HOUR_LABELS[ri]}</div>
            {row.map((v, ci) => {
              const pct = v / max
              const bg =
                pct === 0 ? 'transparent' : `rgba(88, 166, 255, ${0.15 + pct * 0.85})`
              return (
                <div
                  key={ci}
                  className="w-8 h-6 rounded border border-[var(--color-border)] flex items-center justify-center"
                  style={{ background: bg }}
                  title={`${HOUR_LABELS[ri]} ${DAY_LABELS[ci]}: ${v} sessions`}
                >
                  {v > 0 && <span className="text-[10px]">{v}</span>}
                </div>
              )
            })}
          </div>
        ))}
      </div>
    </div>
  )
}
