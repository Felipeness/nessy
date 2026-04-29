type Props = { model: string; size?: 'sm' | 'md' }

export function ModelBadge({ model, size = 'md' }: Props) {
  const m = model.toLowerCase()
  let letter = '?'
  let bg = 'bg-zinc-700'
  if (m.includes('sonnet')) {
    letter = 'S'
    bg = 'bg-blue-600'
  } else if (m.includes('opus')) {
    letter = 'O'
    bg = 'bg-purple-600'
  } else if (m.includes('haiku')) {
    letter = 'H'
    bg = 'bg-green-600'
  }
  const dim = size === 'sm' ? 'w-5 h-5 text-xs' : 'w-6 h-6 text-sm'
  return (
    <span
      className={`inline-flex items-center justify-center rounded ${dim} ${bg} text-white font-bold font-mono`}
      title={model || 'unknown model'}
    >
      {letter}
    </span>
  )
}
