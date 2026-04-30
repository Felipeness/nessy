import type {
  AIHealth,
  AISummary,
  Behavioral,
  BehaviorAdvanced,
  ClusterInfo,
  Costs,
  Insight,
  Knowledge,
  Message,
  Profile,
  ReindexStats,
  SearchResponse,
  Session,
  SimilarResult,
  StatuslineComponentMeta,
  StatuslineConfig,
  StatuslineThemesResp,
  Stats,
  Timeline,
  ToolDrill,
  ToolStat,
} from './types'

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return (await res.json()) as T
}

async function post<T>(path: string): Promise<T> {
  const res = await fetch(path, { method: 'POST' })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return (await res.json()) as T
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return (await res.json()) as T
}

export const api = {
  sessions: () => get<Session[]>('/api/sessions'),
  sessionById: (id: string) => get<Session>(`/api/sessions/${id}`),
  sessionMessages: (id: string, n = 10) =>
    get<Message[]>(`/api/sessions/${id}/messages?n=${n}`),
  stats: () => get<Stats>('/api/stats'),
  behavioral: () => get<Behavioral>('/api/stats/behavioral'),
  behaviorAdvanced: () => get<BehaviorAdvanced>('/api/behavior/advanced'),
  costs: () => get<Costs>('/api/costs'),
  timeline: (from?: string, to?: string) => {
    const q = new URLSearchParams()
    if (from) q.set('from', from)
    if (to) q.set('to', to)
    return get<Timeline>(`/api/timeline${q.toString() ? '?' + q.toString() : ''}`)
  },
  tools: () => get<ToolStat[]>('/api/tools'),
  toolDrill: (name: string) =>
    get<ToolDrill[]>(`/api/tools/${encodeURIComponent(name)}/sessions`),
  search: (
    q: string,
    mode: 'hybrid' | 'metadata' | 'fts' | 'semantic' = 'hybrid',
    group = false, // default = todos hits; group=true dedupa por session
    fuzzy = false, // default = exato (LIKE filter); fuzzy = Porter stem
  ) => {
    const params = new URLSearchParams({ q, mode })
    if (group) params.set('group', 'true')
    if (fuzzy) params.set('fuzzy', 'true')
    return get<SearchResponse>(`/api/search?${params}`)
  },
  refresh: () => post<ReindexStats>('/api/refresh'),
  exportSession: (id: string) => `/api/export/${id}`,
  aiHealth: () => get<AIHealth>('/api/ai/health'),
  aiSummaries: () => get<AISummary[]>('/api/ai/summaries'),
  aiClusters: () => get<ClusterInfo[]>('/api/ai/clusters'),
  aiSimilar: (id: string, n = 10) => get<SimilarResult[]>(`/api/ai/similar/${id}?n=${n}`),
  aiGenerateAll: () => post<{ queued: number }>('/api/ai/generate-all'),
  aiGenerateOne: (id: string) => post<{ status: string }>(`/api/ai/generate/${id}`),
  aiRecomputeClusters: () => post<{ status: string }>('/api/ai/clusters/recompute'),
  aiInsights: () => get<Insight[]>('/api/ai/insights'),
  aiInsightsGenerate: () => post<{ status: string }>('/api/ai/insights/generate'),
  aiProfile: () => get<Profile>('/api/ai/profile'),
  aiProfileGenerate: () => post<{ status: string }>('/api/ai/profile/generate'),
  aiKnowledgeList: () => get<Knowledge[]>('/api/ai/knowledge'),
  aiKnowledgeOne: (id: string) => get<Knowledge>(`/api/ai/knowledge/${id}`),
  aiKnowledgeGenerate: (id: string) =>
    post<{ status: string }>(`/api/ai/knowledge/${id}`),
  aiKnowledgeGenerateAll: () =>
    post<{ status: string }>('/api/ai/knowledge/generate-all'),
  statuslineComponents: () => get<StatuslineComponentMeta[]>('/api/statusline/components'),
  statuslineThemes: () => get<StatuslineThemesResp>('/api/statusline/themes'),
  statuslineConfigGet: () => get<StatuslineConfig>('/api/statusline/config'),
  statuslineConfigSave: (cfg: StatuslineConfig) =>
    postJSON<{ status: string; path: string }>('/api/statusline/config', cfg),
  statuslineRender: (cfg: StatuslineConfig, mockInput?: unknown, mockHistory?: unknown) =>
    postJSON<{ ansi: string; html: string }>('/api/statusline/render', {
      config: cfg,
      mock_input: mockInput,
      mock_history: mockHistory,
    }),
  statuslinePresets: () =>
    get<{ names: string[]; presets: Record<string, StatuslineConfig> }>('/api/statusline/presets'),
}
