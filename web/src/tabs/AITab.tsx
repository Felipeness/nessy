import { useEffect, useState } from 'react'
import { api } from '../api'
import { useSSE } from '../sse'
import type {
  AIHealth,
  AISummary,
  ClusterInfo,
  Insight,
  Knowledge,
  KnowledgeAggregate,
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
  const [knowledge, setKnowledge] = useState<Knowledge | null>(null)
  const [knowledgeLoading, setKnowledgeLoading] = useState(false)
  const [aggregated, setAggregated] = useState<KnowledgeAggregate | null>(null)
  const [genStatus, setGenStatus] = useState<string>('')

  // health + sessions sempre carregam ao montar / quando refresh externo
  useEffect(() => {
    api.aiHealth().then(setHealth).catch(() => setHealth(null))
    api.sessions().then(setSessions)
  }, [reindexCounter])

  // summaries + clusters + insights + profile + aggregated dependem de health.enabled
  useEffect(() => {
    if (!health?.enabled) return
    api.aiSummaries().then(setSummaries).catch(() => {})
    api.aiClusters().then(setClusters).catch(() => {})
    api.aiInsights().then(setInsights).catch(() => {})
    api.aiProfile().then(setProfile).catch(() => {})
    api.aiKnowledgeAggregated().then(setAggregated).catch(() => {})
  }, [health?.enabled, reindexCounter])

  useEffect(() => {
    if (!selectedId) return
    api.aiSimilar(selectedId, 10).then(setSimilar).catch(() => setSimilar([]))
    setKnowledge(null)
    api
      .aiKnowledgeOne(selectedId)
      .then(setKnowledge)
      .catch(() => setKnowledge(null))
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

  const knowledgeDone = useSSE<{ session_id: string; error?: string }>('knowledge_done')
  useEffect(() => {
    if (!knowledgeDone) return
    if (knowledgeDone.error) {
      setGenStatus('knowledge error: ' + knowledgeDone.error)
      setKnowledgeLoading(false)
      return
    }
    if (knowledgeDone.session_id === selectedId) {
      api.aiKnowledgeOne(knowledgeDone.session_id).then(setKnowledge).catch(() => {})
    }
    setKnowledgeLoading(false)
  }, [knowledgeDone, selectedId])

  const knowledgeAllDone = useSSE<{ generated: number; cached: number; error?: string }>(
    'knowledge_all_done',
  )
  useEffect(() => {
    if (!knowledgeAllDone) return
    if (knowledgeAllDone.error) {
      setGenStatus('knowledge-all error: ' + knowledgeAllDone.error)
      return
    }
    setGenStatus(
      `knowledge: ${knowledgeAllDone.generated} novos, ${knowledgeAllDone.cached} já cacheados`,
    )
    if (selectedId) {
      api.aiKnowledgeOne(selectedId).then(setKnowledge).catch(() => {})
    }
    api.aiKnowledgeAggregated().then(setAggregated).catch(() => {})
  }, [knowledgeAllDone, selectedId])

  const handleGenKnowledge = async () => {
    if (!selectedId) return
    setKnowledgeLoading(true)
    setGenStatus(`extraindo knowledge de ${selectedId.slice(0, 8)}…`)
    await api.aiKnowledgeGenerate(selectedId)
  }
  const handleGenKnowledgeAll = async () => {
    setGenStatus('extraindo knowledge de todas sessions…')
    await api.aiKnowledgeGenerateAll()
  }

  if (!health) return <p className="p-6 text-zinc-400">Carregando…</p>

  if (!health.enabled) {
    return (
      <div className="p-6">
        <div className="bg-[#161b22] rounded p-6 border border-[#30363d] max-w-2xl">
          <h2 className="text-xl font-bold mb-2">🤖 AI desabilitada</h2>
          <p className="text-sm text-zinc-400 mb-4">
            Ative em <code className="bg-[#0d1117] px-1 rounded">~/.nessy/config.toml</code>:
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
          <button
            onClick={handleGenKnowledgeAll}
            title="extrai problem/solution/decisions/learnings/code_patterns de cada session"
            className="px-3 py-1 rounded border border-[#30363d] text-sm hover:bg-[#0d1117]"
          >
            📚 Gerar knowledge (todas)
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

      {/* Knowledge — segundo cérebro por session */}
      <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
        <div className="flex items-center justify-between mb-3 gap-3">
          <div>
            <h2 className="font-bold">📚 Knowledge da session</h2>
            <p className="text-xs text-zinc-500">
              Problema, solução, decisões, learnings, padrões e tech extraídos por LLM.{' '}
              <span className="text-zinc-400">
                Selecione uma session em "Sessions similares" abaixo, ou na tab Recent, e gere.
              </span>
            </p>
          </div>
          {selectedId && (
            <button
              onClick={handleGenKnowledge}
              disabled={knowledgeLoading}
              className="px-3 py-1 rounded border border-[#30363d] text-sm hover:bg-[#0d1117] whitespace-nowrap disabled:opacity-50"
            >
              {knowledgeLoading ? '⏳' : '⚡'} Extrair desta session
            </button>
          )}
        </div>
        {!selectedId && (
          <p className="text-sm text-zinc-500 italic">
            Selecione uma session primeiro pra ver/gerar o knowledge dela.
          </p>
        )}
        {selectedId && !knowledge && (
          <p className="text-sm text-zinc-500">
            Sem knowledge gerado pra <code>{selectedId.slice(0, 8)}</code> ainda. Clica em "⚡ Extrair"
            pra a IA processar — leva ~30-60s.
          </p>
        )}
        {knowledge && <KnowledgeCard k={knowledge} />}
      </section>

      {/* Knowledge agregado — visão cross-session do segundo cérebro */}
      {aggregated && aggregated.sessions_analyzed > 0 && (
        <section className="bg-[#161b22] rounded p-4 border border-[#30363d]">
          <div className="flex items-center justify-between mb-3 gap-3">
            <div>
              <h2 className="font-bold">🧬 Knowledge agregado</h2>
              <p className="text-xs text-zinc-500">
                Visão cross-session: padrões mais usados, decisões timeline,
                problemas recorrentes, tech consolidada, perguntas em aberto.{' '}
                <span className="text-zinc-400">
                  Baseado em {aggregated.sessions_analyzed} session
                  {aggregated.sessions_analyzed > 1 ? 's' : ''} com knowledge gerado.
                </span>
              </p>
            </div>
            <button
              onClick={() => api.aiKnowledgeAggregated().then(setAggregated)}
              className="px-3 py-1 rounded border border-[#30363d] text-xs hover:bg-[#0d1117] whitespace-nowrap"
            >
              ↻ refresh
            </button>
          </div>
          <AggregatedView agg={aggregated} onSelectSession={setSelectedId} />
        </section>
      )}

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

// AggregatedView renderiza as 5 visões cross-session em cards lado-a-lado.
// Cada card é collapsible (default expandido). Cliques em session_ids
// chamam onSelectSession pra mudar a session ativa.
function AggregatedView({
  agg,
  onSelectSession,
}: {
  agg: KnowledgeAggregate
  onSelectSession: (id: string) => void
}) {
  return (
    <div className="space-y-3">
      {/* Tech frequency — chips compactos */}
      {agg.tech_frequency.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-indigo-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">
            🔧 Tech frequency · {agg.tech_frequency.length} techs
          </h4>
          <div className="flex flex-wrap gap-1.5">
            {agg.tech_frequency.map((t) => (
              <span
                key={t.name}
                className="px-2 py-0.5 rounded bg-indigo-500/10 text-indigo-300 text-xs font-mono"
                title={`em ${t.count} session${t.count > 1 ? 's' : ''}`}
              >
                {t.name} · {t.count}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Top patterns */}
      {agg.top_patterns.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-cyan-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">
            ⚙️ Top code patterns · {agg.top_patterns.length}
          </h4>
          <ul className="space-y-1.5 text-xs">
            {agg.top_patterns.map((p, i) => (
              <li key={i} className="flex items-start gap-2">
                <span className="text-cyan-400 font-mono w-8 shrink-0">×{p.count}</span>
                <div className="flex-1">
                  <span className="text-zinc-200">{p.pattern}</span>
                  <SessionLinks ids={p.sessions} onSelect={onSelectSession} />
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Recurring problems */}
      {agg.recurring_problems.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-rose-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">
            🔁 Problemas recorrentes · {agg.recurring_problems.length} clusters
          </h4>
          <ul className="space-y-2 text-xs">
            {agg.recurring_problems.map((c, i) => (
              <li key={i}>
                <div className="flex items-start gap-2">
                  <span className="text-rose-400 font-mono w-8 shrink-0">×{c.count}</span>
                  <div className="flex-1">
                    <p className="text-zinc-200">{c.representative}</p>
                    {c.keywords.length > 0 && (
                      <p className="text-[10px] text-zinc-500 mt-0.5">
                        keywords:{' '}
                        {c.keywords.map((k) => (
                          <code key={k} className="mr-1">{k}</code>
                        ))}
                      </p>
                    )}
                    <SessionLinks ids={c.sessions} onSelect={onSelectSession} />
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Decision history — timeline */}
      {agg.decision_history.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-blue-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">
            ⚖️ Decision history · {agg.decision_history.length} decisões
          </h4>
          <ul className="space-y-2 text-xs">
            {agg.decision_history.slice(0, 15).map((d, i) => (
              <li key={i} className="border-l-2 border-zinc-700 pl-3">
                <p className="text-zinc-200 font-medium">{d.decision}</p>
                {d.rationale && (
                  <p className="text-zinc-400 text-[11px] mt-0.5">— {d.rationale}</p>
                )}
                <div className="flex items-center gap-2 mt-1 text-[10px] text-zinc-600">
                  <span>{new Date(d.generated_at * 1000).toLocaleDateString()}</span>
                  <SessionLinks ids={[d.session_id]} onSelect={onSelectSession} compact />
                </div>
              </li>
            ))}
            {agg.decision_history.length > 15 && (
              <li className="text-zinc-600 text-[11px]">+{agg.decision_history.length - 15} mais</li>
            )}
          </ul>
        </div>
      )}

      {/* Open questions */}
      {agg.open_questions.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-amber-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">
            ❓ Em aberto · {agg.open_questions.length} perguntas
          </h4>
          <ul className="space-y-1.5 text-xs">
            {agg.open_questions.map((q, i) => (
              <li key={i} className="flex items-start gap-2">
                <span
                  className={`font-mono w-12 shrink-0 text-[10px] ${
                    q.age_days > 14 ? 'text-rose-400' : q.age_days > 7 ? 'text-amber-400' : 'text-zinc-500'
                  }`}
                  title={`gerado há ${q.age_days} dias`}
                >
                  {q.age_days === 0 ? 'hoje' : `${q.age_days}d`}
                </span>
                <div className="flex-1">
                  <span className="text-zinc-200">{q.question}</span>
                  <SessionLinks ids={[q.session_id]} onSelect={onSelectSession} compact />
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

// SessionLinks renderiza session_ids como botões clicáveis pra mudar a
// session ativa no AI tab (e ver KnowledgeCard daquela).
function SessionLinks({
  ids,
  onSelect,
  compact = false,
}: {
  ids: string[]
  onSelect: (id: string) => void
  compact?: boolean
}) {
  if (ids.length === 0) return null
  return (
    <div className={`flex flex-wrap gap-1 ${compact ? '' : 'mt-1'}`}>
      {ids.map((id) => (
        <button
          key={id}
          onClick={() => onSelect(id)}
          className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-zinc-800/50 text-zinc-400 hover:bg-blue-500/20 hover:text-blue-300"
          title={`abrir session ${id}`}
        >
          {id.slice(0, 8)}
        </button>
      ))}
    </div>
  )
}

// KnowledgeCard renderiza o "segundo cérebro" extraído de uma session.
// 6 blocos: problem, solution, decisions, learnings, code_patterns,
// tech_used + open_questions. Cada um colapsa quando vazio.
function KnowledgeCard({ k }: { k: Knowledge }) {
  return (
    <div className="space-y-3 text-sm">
      <div className="grid md:grid-cols-2 gap-3">
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-amber-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-1">🎯 Problema</h4>
          <p className="text-zinc-200">{k.problem || <span className="italic text-zinc-500">não identificado</span>}</p>
        </div>
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-green-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-1">✅ Solução</h4>
          <p className="text-zinc-200">{k.solution || <span className="italic text-zinc-500">não identificado</span>}</p>
        </div>
      </div>

      {k.decisions.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-blue-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">⚖️ Decisões</h4>
          <ul className="space-y-2 text-xs">
            {k.decisions.map((d, i) => (
              <li key={i}>
                <span className="font-bold text-zinc-200">{d.decision}</span>
                {d.rationale && <span className="text-zinc-400"> — {d.rationale}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}

      {k.learnings.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-purple-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">💡 Learnings</h4>
          <ul className="list-disc list-inside text-xs text-zinc-300 space-y-1">
            {k.learnings.map((l, i) => (
              <li key={i}>{l}</li>
            ))}
          </ul>
        </div>
      )}

      {k.code_patterns.length > 0 && (
        <div className="bg-[#0d1117] rounded p-3 border-l-4 border-cyan-500">
          <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">⚙️ Code patterns</h4>
          <ul className="list-disc list-inside text-xs text-zinc-300 space-y-1 font-mono">
            {k.code_patterns.map((p, i) => (
              <li key={i}>{p}</li>
            ))}
          </ul>
        </div>
      )}

      <div className="grid md:grid-cols-2 gap-3">
        {k.tech_used.length > 0 && (
          <div className="bg-[#0d1117] rounded p-3 border-l-4 border-indigo-500">
            <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">🔧 Tech</h4>
            <div className="flex flex-wrap gap-1">
              {k.tech_used.map((t, i) => (
                <span
                  key={i}
                  className="px-2 py-0.5 rounded bg-indigo-500/10 text-indigo-300 text-xs font-mono"
                >
                  {t}
                </span>
              ))}
            </div>
          </div>
        )}
        {k.open_questions.length > 0 && (
          <div className="bg-[#0d1117] rounded p-3 border-l-4 border-rose-500">
            <h4 className="text-[10px] uppercase tracking-wide text-zinc-500 mb-2">❓ Em aberto</h4>
            <ul className="list-disc list-inside text-xs text-zinc-300 space-y-1">
              {k.open_questions.map((q, i) => (
                <li key={i}>{q}</li>
              ))}
            </ul>
          </div>
        )}
      </div>

      <p className="text-[10px] text-zinc-600 text-right">
        gerado em {new Date(k.generated_at * 1000).toLocaleString()}
      </p>
    </div>
  )
}
