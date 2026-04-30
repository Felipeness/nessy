// Tipos espelhados do backend Go.

export type Session = {
  session_id: string
  project_dir: string
  jsonl_path: string
  jsonl_mtime: string
  start_time: string
  end_time: string
  message_count: number
  user_messages: number
  assistant_messages: number
  first_user_msg: string
  last_user_msg: string
  git_branch: string
  claude_version: string
  model: string
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  tool_calls: Record<string, number>
}

export type Cost = {
  USD: number
  BRL: number
  InputUSD: number
  OutputUSD: number
  CacheCreationUSD: number
  CacheReadUSD: number
}

export type MonthCost = {
  Accumulated: number
  Today: number
  Projection: number
  Days: number
  DayOfMonth: number
}

export type WeekDelta = {
  ThisWeek: { Sessions: number; Msgs: number; CostUSD: number }
  LastWeek: { Sessions: number; Msgs: number; CostUSD: number }
}

export type Stats = {
  heatmap: number[][]
  heatmap_weeks: number
  model_distribution: { name: string; count: number }[]
  month_cost: MonthCost
  week_delta: WeekDelta
  top_projects: { project_dir: string; cost_usd: number }[]
  cache_savings_usd: number
  long_tail_cost: SessionSummary[]
  long_tail_duration: SessionSummary[]
  total_sessions: number
  total_msgs: number
  total_cost_usd: number
}

export type SessionSummary = {
  session_id: string
  project_dir: string
  start_time: string
  message_count: number
  duration_ns: number
  cost_usd: number
}

export type WordCount = { Word: string; Count: number }

export type Behavioral = {
  top_words: WordCount[]
  top_prefixes: WordCount[]
  error_rate: number
  error_hits: number
  error_total: number
  peak_hour: number[]
}

export type Costs = {
  by_day: { date: string; cost_usd: number }[]
  by_project: { project_dir: string; cost_usd: number }[]
  by_model: { model: string; cost_usd: number }[]
  cache_savings_usd: number
  month_cost: MonthCost
}

export type DayBucket = {
  date: string
  sessions: Session[]
}

export type Timeline = {
  days: DayBucket[]
}

export type ToolStat = {
  name: string
  total_calls: number
  num_sessions: number
}

export type ToolDrill = {
  session: Session
  count: number
}

export type SearchResult = {
  session: Session
  snippet?: string
  role?: string
  rank?: number
}

export type SearchResponse = {
  mode: string
  results: SearchResult[]
}

export type Message = {
  SessionID: string
  Role: string
  Content: string
}

export type ReindexStats = {
  Scanned: number
  New: number
  Updated: number
  Removed: number
}

export type TabName =
  | 'recent'
  | 'search'
  | 'stats'
  | 'costs'
  | 'timeline'
  | 'tools'
  | 'behavior'
  | 'ai'
  | 'compare'
  | 'studio'

export type StatuslineComponentMeta = {
  name: string
  label: string
  category: string
  description: string
  needs_history: boolean
  has_warn_at: boolean
}

export type StatuslineColor = { r: number; g: number; b: number }
export type StatuslineThemeSeg = { bg: StatuslineColor; fg: StatuslineColor }
export type StatuslineTheme = {
  name: string
  default: StatuslineThemeSeg
  segs: Record<string, StatuslineThemeSeg>
  status: { ok: StatuslineColor; warn: StatuslineColor; crit: StatuslineColor }
  muted: StatuslineColor
}
export type StatuslineThemesResp = {
  themes: StatuslineTheme[]
  styles: string[]
}

export type StatuslineLine = {
  components: string[]
  separator?: string
}

export type StatuslineComponentOpts = {
  warn_at?: number
  critical_at?: number
  format?: string
  hide?: boolean
}

export type StatuslineConfig = {
  theme: string
  style: string
  charset?: string
  auto_wrap?: boolean
  lines: StatuslineLine[]
  components?: Record<string, StatuslineComponentOpts>
  history?: { endpoint?: string; timeout?: string }
}

// StatuslineMock é o subset de campos que o user edita no Mock Data editor.
// Convertido em Input + HistoryData pelo Studio antes de mandar pro POST /render.
export type StatuslineMock = {
  cwd: string
  branch: string
  model: string
  context_pct: number
  cost_usd: number
  lines_added: number
  lines_removed: number
  rate_5h_pct: number
  rate_7d_pct: number
  vim_mode: '' | 'NORMAL' | 'INSERT'
  // History-side (simula daemon) — afetam burn_rate, cost_session badge, cluster.
  burn_rate_tpm: number
  cost_p90: number
  cost_today: number
  cluster_name: string
}

export type AIHealth = {
  enabled: boolean
  ollama_reachable: boolean
  gen_model: string
  embed_model: string
  cached: number
  total: number
  queued: number
}

export type AISummary = {
  session_id: string
  summary: string
  cluster: number
  label: string
}

export type ClusterInfo = {
  cluster_id: number
  label: string
  session_ids: string[]
}

export type SimilarResult = {
  session_id: string
  similarity: number
}

export type Insight = {
  ID: number
  Type: string
  Title: string
  Description: string
  Evidence: string
  SuggestedAction: string
  CreatedAt: number
}

export type Profile = {
  content: string
  generated_at: number
}

export type Decision = {
  decision: string
  rationale: string
}

export type Knowledge = {
  session_id: string
  problem: string
  solution: string
  decisions: Decision[]
  learnings: string[]
  code_patterns: string[]
  tech_used: string[]
  open_questions: string[]
  generated_at: number
}

export type PatternFrequency = {
  pattern: string
  count: number
  sessions: string[]
}

export type DecisionEntry = {
  decision: string
  rationale: string
  session_id: string
  generated_at: number
}

export type ProblemCluster = {
  representative: string
  sessions: string[]
  count: number
  keywords: string[]
}

export type TechFrequency = {
  name: string
  count: number
  sessions: string[]
}

export type OpenQuestionEntry = {
  question: string
  session_id: string
  generated_at: number
  age_days: number
}

export type KnowledgeAggregate = {
  sessions_analyzed: number
  top_patterns: PatternFrequency[]
  decision_history: DecisionEntry[]
  recurring_problems: ProblemCluster[]
  tech_frequency: TechFrequency[]
  open_questions: OpenQuestionEntry[]
}

export type Bigram = { A: string; B: string; Count: number }
export type Trigram = { A: string; B: string; C: string; Count: number }
export type CoOccur = { A: string; B: string; Count: number; PMI: number }
export type FlowHist = { Bucket: string; Count: number }
export type FlowSummary = { Hist: FlowHist[]; P50: number; P90: number; P99: number }
export type StyleStats = {
  AvgWordsUser: number
  AvgWordsAssistant: number
  UniqueWordsUser: number
  UniqueWordsAssistant: number
  TopWordsUser: WordCount[]
  TopWordsAssistant: WordCount[]
  AvgSentencesUser: number
  AvgSentencesAssistant: number
}
export type ErrorSession = {
  session: Session
  error_rate: number
  hits: number
  total: number
}
export type TimeCostPoint = {
  hour: number
  cost_usd: number
  model: string
  project_dir: string
}
export type BehaviorAdvanced = {
  bigrams: Bigram[]
  trigrams: Trigram[]
  co_occurrences: CoOccur[]
  flow: FlowSummary
  style: StyleStats
  high_error_sessions: ErrorSession[]
  time_cost_points: TimeCostPoint[]
}
