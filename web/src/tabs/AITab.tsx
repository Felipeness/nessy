import { useEffect, useState } from 'react'
import { api } from '../api'
import { useSSE } from '../sse'
import type {
  AIHealth,
  AISummary,
  ClusterInfo,
  Insight,
  Profile,
  Session,
  SimilarResult,
} from '../types'

type Props = { reindexCounter: number }

export function AITab({ reindexCounter }: Props) {
  const [health, setHealth] = useState<AIHealth | null>(null)
  const [summaries, setSummaries] = useState<AISummary[]>([])
  const [clusters, setClusters] = useState<ClusterInfo[]>([])
  const [sessions, setSessions] = useState<Session[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [similar, setSimilar] = useState<SimilarResult[]>([])
  const [insights, setInsights] = useState<Insight[]>([])
  const [profile, setProfile] = useState<Profile | null>(null)
  const [genStatus, setGenStatus] = useState<string>('')

  // health + sessions sempre carregam ao montar / quando refresh externo
  useEffect(() => {
    api.aiHealth().then(setHealth).catch(() => setHealth(null))
    api.sessions().then(setSessions)
  }, [reindexCounter])

  // summaries + clusters + insights + profile dependem de health.enabled
  useEffect(() => {
    if (!health?.enabled) return
    api.aiSummaries().then(setSummaries).catch(() => {})
    api.aiClusters().then(setClusters).catch(() => {})
    api.aiInsights().then(setInsights).catch(() => {})
    api.aiProfile().then(setProfile).catch(() => {})
  }, [health?.enabled, reindexCounter])

  useEffect(() => {
    if (!selectedId) return
    api.aiSimilar(selectedId, 10).then(setSimilar).catch(() => setSimilar([]))
  }, [selectedId])

  // poll periódico pra capturar progress de auto-generate em background
  useEffect(() => {
    if (!health?.enabled || !health.ollama_reachable) return
    const t = setInterval(() => {
      api.aiHealth().then(setHealth).catch(() => {})
      api.aiSummaries().then(setSummaries).catch(() => {})
    }, 4000)
    return () => clearInterval(t)
  }, [health?.enabled, health?.ollama_reachable])

  // SSE: quando backend termina recompute de clusters, refetch
  const clustersDone = useSSE<{ clusters: ClusterInfo[] }>('clusters_done')
  useEffect(() => {
    if (!clustersDone) return
    setGenStatus('clusters atualizados')
    api.aiClusters().then(setClusters).catch(() => {})
    api.aiSummaries().then(setSummaries).catch(() => {})
  }, [clustersDone])

  // SSE: quando uma session ganha resumo, refetch summaries pra atualizar
  const summaryDone = useSSE<{ session_id: string }>('summary_done')
  useEffect(() => {
    if (!summaryDone || !health?.enabled) return
    api.aiSummaries().then(setSummaries).catch(() => {})
    api.aiHealth().then(setHealth).catch(() => {})
  }, [summaryDone, health?.enabled])

  // SSE: insights e profile
  const insightsDone = useSSE<{ count: number; error?: string }>('insights_done')
  useEffect(() => {
    if (!insightsDone) return
    if (insightsDone.error) {
      setGenStatus('insights error: ' + insightsDone.error)
      return
    }
    setGenStatus(`${insightsDone.count} insights gerados`)
    api.aiInsights().then(setInsights).catch(() => {})
  }, [insightsDone])

  const profileDone = useSSE<{ length: number; error?: string }>('profile_done')
  useEffect(() => {
    if (!profileDone) return
    if (profileDone.error) {
      setGenStatus('profile error: ' + profileDone.error)
      return
    }
    setGenStatus('profile atualizado')
    api.aiProfile().then(setProfile).catch(() => {})
  }, [profileDone])

  if (!health) return <p className="p-6 text-zinc-400">Carregando…</p>

  if (!health.enabled) {
    return (
      <div className="p-6">
        <div className="bg-[#161b22] rounded p-6 border border-[#30363d] max-w-2xl">
          <h2 className="text-xl font-bold mb-2">🤖 AI desabilitada</h2>
          <p className="text-sm text-zinc-400 mb-4">
            Ative em <code className="bg-[#0d1117] px-1 rounded">~/.claude-history/config.toml</code>:
          </p>
          <pre className="bg-[#0d1117] p-3 rounded text-xs overflow-auto">
            {`[ai]\nenabled = true\nollama_url = "http://localhost:11434"\ngen_model = "qwen2.5:7b"\nembed_model = "nomic-embed-text"`}
          </pre>
          <p className="text-sm text-zinc-400 mt-4">
            Ou rode sem o flag <code className="bg-[#0d1117] px-1 rounded">--no-ai</code>.
          </p>
        </div>
      </div>
    )
  }

  if (!health.ollama_reachable) {
    return (
      <div className="p-6">
        <div className="bg-[#161b22] rounded p-6 border border-[#f85149] max-w-2xl">
          <h2 className="text-xl font-bold mb-2 text-red-400">🤖 Ollama não responde</h2>
          <p className="text-sm text-zinc-400 mb-4">
            Inicie o Ollama e baixe os modelos necessários:
          </p>
          <pre className="bg-[#0d1117] p-3 rounded text-xs overflow-auto">
            {`ollama serve\nollama pull ${health.gen_model}\nollama pull ${health.embed_model}`}
          </pre>
        </div>
      </div>
    )
  }

  const summaryByID = new Map(summaries.map((s) => [s.session_id, s]))
  const handleGenerateAll = async () => {
    setGenStatus('queueing…')
    const r = await api.aiGenerateAll()
    setGenStatus(`${r.queued} sessions enfileiradas, processando em background`)
  }
  const handleRecompute = async () => {
    setGenStatus('recomputing clusters…')
    await api.aiRecomputeClusters()
    setGenStatus('clusters recomputados em background')
  }
  const handleGenInsights = async () => {
    setGenStatus('analisando padrões…')
    await api.aiInsightsGenerate()
  }
  const handleGenProfile = async () => {
    setGenStatus('gerando profile…')
    await api.aiProfileGenerate()
  }
  const copyProfile = () => {
    if (profile?.content) navigator.clipboard.writeText(profile.content)
  }

  return (
    <div className="p-6 space-y-6">
      {/* Status */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d] flex items-center gap-4">
        <div>
          <h2 className="font-bold">🤖 Ollama ✓ {health.gen_model}</h2>
          <p className="text-xs text-zinc-400">
            embed: {health.embed_model} · {health.cached}/{health.total} cached · queue:{' '}
            {health.queued}
          </p>
        </div>
        <div className="ml-auto flex gap-2 flex-wrap">
          <button
            onClick={handleGenerateAll}
            className="px-3 py-1 rounded border border-[#30363d] text-sm hover:bg-[#0d1117]"
          >
            🚀 Generate all
          </button>
          <button
            onClick={handleRecompute}
            className="px-3 py-1 rounded border border-[#30363d] text-sm hover:bg-[#0d1117]"
          >
            🔄 Recompute clusters
          </button>
          <button
            onClick={handleGenInsights}
            className="px-3 py-1 rounded border border-[#30363d] text-sm hover:bg-[#0d1117]"
          >
            💡 Gerar insights
          </button>
          <button
            onClick={handleGenProfile}
            className="px-3 py-1 rounded border border-[#30363d] text-sm hover:bg-[#0d1117]"
          >
            🧠 Gerar profile
          </button>
          {genStatus && <span className="text-xs text-zinc-400 self-center">{genStatus}</span>}
        </div>
      </section>

      {/* Insights */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">💡 Insights & advisor</h2>
        {insights.length === 0 ? (
          <p className="text-sm text-zinc-500">
            Nenhum insight ainda. Clica "Gerar insights" pra a IA analisar seus padrões.
          </p>
        ) : (
          <div className="grid md:grid-cols-2 gap-3">
            {insights.map((i) => (
              <div
                key={i.ID}
                className={`bg-[#0d1117] rounded p-3 border-l-4 ${insightColor(i.Type)}`}
              >
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-xs font-mono text-zinc-500">{insightIcon(i.Type)}</span>
                  <span className="text-xs uppercase text-zinc-500">{i.Type.replace(/_/g, ' ')}</span>
                </div>
                <h3 className="font-bold text-sm mb-1">{i.Title}</h3>
                <p className="text-xs text-zinc-300 mb-2">{i.Description}</p>
                {i.SuggestedAction && (
                  <p className="text-xs text-blue-400">→ {i.SuggestedAction}</p>
                )}
                {i.Evidence && (
                  <p className="text-[10px] text-zinc-600 mt-2 font-mono truncate" title={i.Evidence}>
                    {i.Evidence}
                  </p>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Personal profile */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <div className="flex items-center mb-3">
          <h2 className="font-bold">🧠 Personal profile</h2>
          {profile?.content && (
            <button
              onClick={copyProfile}
              className="ml-auto px-2 py-1 rounded border border-[#30363d] text-xs hover:bg-[#0d1117]"
            >
              📋 Copiar
            </button>
          )}
        </div>
        {profile?.content ? (
          <pre className="text-sm text-zinc-300 whitespace-pre-wrap font-sans">
            {profile.content}
          </pre>
        ) : (
          <p className="text-sm text-zinc-500">
            Nenhum profile ainda. Clica "Gerar profile" pra a IA criar uma representação textual de
            quem você é.
          </p>
        )}
      </section>

      {/* Clusters */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">🗂 Clusters temáticos</h2>
        {clusters.length === 0 ? (
          <p className="text-sm text-zinc-500">
            Nenhum cluster ainda. Clica "Recompute clusters" pra gerar (precisa de embeddings).
          </p>
        ) : (
          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-3">
            {clusters.map((c) => (
              <div key={c.cluster_id} className="bg-[#0d1117] rounded p-3 border border-[#30363d]">
                <h3 className="font-bold text-sm text-blue-400">[{c.label}]</h3>
                <p className="text-xs text-zinc-500 mb-2">{c.session_ids.length} sessions</p>
                <ul className="text-xs space-y-1 font-mono">
                  {c.session_ids.slice(0, 4).map((sid) => {
                    const s = summaryByID.get(sid)
                    return (
                      <li key={sid} className="truncate">
                        <span className="text-zinc-500">{sid.slice(0, 8)}</span>{' '}
                        {s?.summary && <span className="text-zinc-300">{s.summary}</span>}
                      </li>
                    )
                  })}
                  {c.session_ids.length > 4 && (
                    <li className="text-zinc-500">+{c.session_ids.length - 4} more…</li>
                  )}
                </ul>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Find similar */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">🔗 Sessions similares</h2>
        <select
          value={selectedId ?? ''}
          onChange={(e) => setSelectedId(e.target.value || null)}
          className="w-full bg-[#0d1117] border border-[#30363d] rounded px-2 py-1 text-sm font-mono mb-3"
        >
          <option value="">Selecione uma session…</option>
          {sessions.map((s) => (
            <option key={s.session_id} value={s.session_id}>
              {s.session_id.slice(0, 8)} · {s.first_user_msg.slice(0, 50)}
            </option>
          ))}
        </select>
        {similar.length > 0 ? (
          <ul className="space-y-1 font-mono text-sm">
            {similar.map((r) => {
              const s = summaryByID.get(r.session_id)
              return (
                <li key={r.session_id} className="flex items-center gap-3">
                  <span className="w-12 text-right text-blue-400">
                    {r.similarity.toFixed(2)}
                  </span>
                  <span className="text-zinc-500">{r.session_id.slice(0, 8)}</span>
                  <span className="truncate flex-1">
                    {s?.summary || sessions.find((x) => x.session_id === r.session_id)?.first_user_msg}
                  </span>
                </li>
              )
            })}
          </ul>
        ) : (
          selectedId && (
            <p className="text-sm text-zinc-500">
              Sem similares — embedding ainda não gerado pra essa session.
            </p>
          )
        )}
      </section>

      {/* Summaries */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <h2 className="font-bold mb-3">📋 Resumos gerados ({summaries.length})</h2>
        <ul className="space-y-1 font-mono text-sm max-h-[400px] overflow-auto">
          {summaries.map((s) => (
            <li
              key={s.session_id}
              className="flex items-start gap-3 px-2 py-1 hover:bg-[#0d1117] rounded"
            >
              <span className="text-zinc-500 w-20 shrink-0">{s.session_id.slice(0, 8)}</span>
              {s.label && (
                <span className="text-blue-400 text-xs shrink-0">[{s.label}]</span>
              )}
              <span className="text-zinc-200">{s.summary}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}

function insightColor(type: string): string {
  switch (type) {
    case 'repeated_task':
      return 'border-blue-500'
    case 'chronic_problem':
      return 'border-red-500'
    case 'script_opportunity':
      return 'border-green-500'
    case 'token_waste':
      return 'border-orange-500'
    case 'performance_hint':
      return 'border-purple-500'
    case 'anti_pattern':
      return 'border-pink-500'
    case 'personal_pattern':
      return 'border-yellow-500'
    default:
      return 'border-zinc-500'
  }
}

function insightIcon(type: string): string {
  switch (type) {
    case 'repeated_task':
      return '🔁'
    case 'chronic_problem':
      return '⚠️'
    case 'script_opportunity':
      return '🚀'
    case 'token_waste':
      return '💸'
    case 'performance_hint':
      return '⚡'
    case 'anti_pattern':
      return '🚫'
    case 'personal_pattern':
      return '🎯'
    default:
      return '💡'
  }
}
