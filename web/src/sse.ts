import { useEffect, useRef, useState } from 'react'

export function useSSE<T = unknown>(eventName: string): T | null {
  const [data, setData] = useState<T | null>(null)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    const es = new EventSource('/api/events')
    esRef.current = es
    es.addEventListener(eventName, (e) => {
      try {
        const parsed = JSON.parse((e as MessageEvent).data) as T
        setData(parsed)
      } catch {
        // ignore
      }
    })
    return () => {
      es.close()
    }
  }, [eventName])

  return data
}
