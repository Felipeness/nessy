import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { ChatTurn } from '../types'

// NessTab — chat conversacional com "Ness IA": segundo cérebro técnico
// que conhece todas suas sessions passadas. Cada pergunta dispara RAG
// no backend (top-8 sessions por similarity), resposta cita session_ids.
export function NessTab() {
  const [history, setHistory] = useState<ChatTurn[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [history, loading])

  const send = async () => {
    const text = input.trim()
    if (!text || loading) return
    const next: ChatTurn[] = [...history, { role: 'user', content: text }]
    setHistory(next)
    setInput('')
    setLoading(true)
    setError('')
    try {
      const resp = await api.aiChat(
        next.map((t) => ({ role: t.role, content: t.content })),
      )
      setHistory([
        ...next,
        { role: 'assistant', content: resp.response, sources: resp.sources },
      ])
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  const onKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  const clear = () => {
    setHistory([])
    setError('')
  }

  return (
    <div className="flex flex-col h-full">
      <header className="px-4 py-3 border-b border-[var(--color-border)] flex items-center gap-3">
        <h1 className="font-bold text-[var(--color-accent)]">🧠 Ness IA</h1>
        <span className="text-xs text-[var(--color-muted)]">
          chat com seu segundo cérebro — conhece todas suas sessions
        </span>
        {history.length > 0 && (
          <button
            onClick={clear}
            className="ml-auto px-2 py-1 rounded border border-[var(--color-border)] text-xs text-[var(--color-muted)] hover:text-[var(--color-fg)]"
          >
            ↺ nova conversa
          </button>
        )}
      </header>

      <div className="flex-1 overflow-auto p-4 space-y-4">
        {history.length === 0 && <EmptyState />}
        {history.map((turn, i) => (
          <ChatBubble key={i} turn={turn} />
        ))}
        {loading && (
          <div className="text-sm text-[var(--color-muted)] italic px-4 animate-pulse">
            Ness IA pensando…
          </div>
        )}
        {error && (
          <div className="text-sm text-red-400 px-4 border-l-2 border-red-400 pl-3">
            erro: {error}
          </div>
        )}
        <div ref={bottomRef} />
      </div>

      <div className="border-t border-[var(--color-border)] p-3">
        <div className="flex gap-2">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKey}
            placeholder="pergunta pro Ness — ex: 'como resolvi auth bug 3 meses atrás?'"
            disabled={loading}
            rows={2}
            className="flex-1 bg-[var(--color-card)] border border-[var(--color-border)] rounded px-3 py-2 text-sm font-mono resize-none focus:outline-none focus:border-[var(--color-accent)] disabled:opacity-50"
          />
          <button
            onClick={send}
            disabled={loading || !input.trim()}
            className="px-4 py-2 rounded bg-[var(--color-accent)] text-black text-sm font-medium hover:opacity-90 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            {loading ? '…' : 'Enviar'}
          </button>
        </div>
        <div className="mt-1 text-[10px] text-[var(--color-muted)]">
          Enter envia · Shift+Enter quebra linha · Ness usa RAG sobre todas suas
          sessions com knowledge gerado
        </div>
      </div>
    </div>
  )
}

function EmptyState() {
  const examples = [
    'como resolvi auth bug 3 meses atrás?',
    'quais decisões tomei sobre o setup do Mac sem sudo?',
    'qual foi minha melhor solução pra rebase em monorepo?',
    'tem algo em aberto que eu deveria fechar?',
    'qual padrão eu mais uso pra error handling em Go?',
  ]
  return (
    <div className="text-center py-12 max-w-2xl mx-auto">
      <h2 className="text-lg font-bold text-[var(--color-fg)] mb-2">
        Pergunta qualquer coisa sobre seu histórico
      </h2>
      <p className="text-sm text-[var(--color-muted)] mb-6">
        Eu conheço tudo que você fez nas suas sessions do Claude Code — problemas,
        soluções, decisões, learnings. Cito as fontes em <code>[abc12345]</code>.
      </p>
      <div className="text-xs text-[var(--color-muted)] mb-2">exemplos:</div>
      <ul className="text-sm text-left space-y-1 max-w-md mx-auto">
        {examples.map((e) => (
          <li key={e} className="px-3 py-1.5 rounded border border-[var(--color-border)]">
            <code className="text-[var(--color-accent)]">"{e}"</code>
          </li>
        ))}
      </ul>
      <p className="text-[10px] text-[var(--color-muted)] mt-4">
        ⚠ Pra Ness IA funcionar bem, gere knowledge das suas sessions na tab AI:{' '}
        <code>📚 Gerar knowledge (todas)</code>.
      </p>
    </div>
  )
}

function ChatBubble({ turn }: { turn: ChatTurn }) {
  const isUser = turn.role === 'user'
  return (
    <div className={`flex gap-3 ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`max-w-[80%] ${
          isUser
            ? 'bg-[var(--color-accent)]/10 border-[var(--color-accent)]/40 text-[var(--color-fg)]'
            : 'bg-[var(--color-card)] border-[var(--color-border)] text-[var(--color-fg)]'
        } rounded-lg border px-4 py-3 space-y-2`}
      >
        <div className="text-[10px] uppercase tracking-wide text-[var(--color-muted)]">
          {isUser ? 'você' : '🧠 ness'}
        </div>
        <div className="text-sm whitespace-pre-wrap leading-relaxed">{turn.content}</div>
        {turn.sources && turn.sources.length > 0 && (
          <div className="pt-2 border-t border-[var(--color-border)] mt-2">
            <div className="text-[10px] uppercase tracking-wide text-[var(--color-muted)] mb-1">
              fontes ({turn.sources.length})
            </div>
            <div className="flex flex-wrap gap-1.5">
              {turn.sources.map((s) => (
                <a
                  key={s.session_id}
                  href={`#search`}
                  onClick={(e) => {
                    e.preventDefault()
                    // copia o ID pro clipboard pra fácil paste no Search
                    navigator.clipboard?.writeText(s.session_id).catch(() => {})
                  }}
                  title={`${s.summary}\n\nsimilarity: ${(s.similarity * 100).toFixed(0)}%\nclick pra copiar session_id`}
                  className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-[var(--color-bg)] text-[var(--color-muted)] hover:text-[var(--color-accent)] border border-[var(--color-border)]"
                >
                  [{s.session_id.slice(0, 8)}] {(s.similarity * 100).toFixed(0)}%
                </a>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
