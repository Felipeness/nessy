import type {
  Behavioral,
  BehaviorAdvanced,
  Costs,
  Message,
  ReindexStats,
  SearchResponse,
  Session,
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
  search: (q: string, mode: 'metadata' | 'fts' = 'metadata') =>
    get<SearchResponse>(`/api/search?q=${encodeURIComponent(q)}&mode=${mode}`),
  refresh: () => post<ReindexStats>('/api/refresh'),
  exportSession: (id: string) => `/api/export/${id}`,
}
