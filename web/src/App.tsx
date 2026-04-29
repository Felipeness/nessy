import { useEffect, useState } from 'react'
import { api } from './api'
import { useSSE } from './sse'
import { Layout } from './components/Layout'
import { RecentTab } from './tabs/RecentTab'
import { SearchTab } from './tabs/SearchTab'
import { StatsTab } from './tabs/StatsTab'
import { CostsTab } from './tabs/CostsTab'
import { TimelineTab } from './tabs/TimelineTab'
import { ToolsTab } from './tabs/ToolsTab'
import { BehaviorTab } from './tabs/BehaviorTab'
import { AITab } from './tabs/AITab'
import { CompareTab } from './tabs/CompareTab'
import type { ReindexStats, TabName } from './types'

const VALID_TABS: TabName[] = [
  'recent',
  'search',
  'stats',
  'costs',
  'timeline',
  'tools',
  'behavior',
  'ai',
  'compare',
]

function tabFromHash(): TabName {
  const h = window.location.hash.replace(/^#/, '').split('?')[0] as TabName
  return VALID_TABS.includes(h) ? h : 'recent'
}

export default function App() {
  const [tab, setTab] = useState<TabName>(() => tabFromHash())
  const [reindexCounter, setReindexCounter] = useState(0)
  const [status, setStatus] = useState<string>('')
  const reindexEvent = useSSE<ReindexStats>('reindex_done')

  useEffect(() => {
    const handler = () => setTab(tabFromHash())
    window.addEventListener('hashchange', handler)
    return () => window.removeEventListener('hashchange', handler)
  }, [])

  useEffect(() => {
    if (reindexEvent) {
      setStatus(
        `🔵 +${reindexEvent.New} new · ${reindexEvent.Updated} updated · ${reindexEvent.Removed} removed`,
      )
      setReindexCounter((c) => c + 1)
    }
  }, [reindexEvent])

  const handleTabChange = (t: TabName) => {
    window.location.hash = '#' + t
    setTab(t)
  }

  const handleRefresh = async () => {
    setStatus('refreshing…')
    try {
      const r = await api.refresh()
      setStatus(`refresh: +${r.New} new · ${r.Updated} updated`)
      setReindexCounter((c) => c + 1)
    } catch (err) {
      setStatus('refresh error: ' + String(err))
    }
  }

  return (
    <Layout active={tab} onTabChange={handleTabChange} status={status} onRefresh={handleRefresh}>
      {tab === 'recent' && <RecentTab reindexCounter={reindexCounter} />}
      {tab === 'search' && <SearchTab reindexCounter={reindexCounter} />}
      {tab === 'stats' && <StatsTab reindexCounter={reindexCounter} />}
      {tab === 'costs' && <CostsTab reindexCounter={reindexCounter} />}
      {tab === 'timeline' && <TimelineTab reindexCounter={reindexCounter} />}
      {tab === 'tools' && <ToolsTab reindexCounter={reindexCounter} />}
      {tab === 'behavior' && <BehaviorTab reindexCounter={reindexCounter} />}
      {tab === 'ai' && <AITab reindexCounter={reindexCounter} />}
      {tab === 'compare' && <CompareTab reindexCounter={reindexCounter} />}
    </Layout>
  )
}
