import { useCallback, useEffect, useRef, useState } from 'react'
import { api, APIError, type ServerStatus } from './api'

export type ServerStatusState =
  | { state: 'loading' }
  | { state: 'error' }
  | { state: 'ready'; status: ServerStatus }

const loadingState: ServerStatusState = { state: 'loading' }
const pollInterval = 5_000
const requestTimeout = 8_000

export function useServerStatus(enabled: boolean, onExpired: () => void) {
  const [snapshot, setSnapshot] = useState<ServerStatusState>(loadingState)
  const inFlight = useRef<AbortController>()

  const refresh = useCallback(async () => {
    if (!enabled || inFlight.current) return
    const request = new AbortController()
    inFlight.current = request
    const timeout = window.setTimeout(() => {
      if (request.signal.aborted) return
      request.abort()
      setSnapshot({ state: 'error' })
    }, requestTimeout)

    try {
      const status = await api<ServerStatus>('/server/status', 'GET', undefined, request.signal)
      if (!request.signal.aborted) setSnapshot({ state: 'ready', status })
    } catch (error) {
      if (request.signal.aborted) return
      if (error instanceof APIError && error.status === 401) onExpired()
      else setSnapshot({ state: 'error' })
    } finally {
      window.clearTimeout(timeout)
      if (inFlight.current === request) inFlight.current = undefined
    }
  }, [enabled, onExpired])

  useEffect(() => {
    if (!enabled) {
      setSnapshot(loadingState)
      return
    }

    function refreshVisible() {
      if (!document.hidden) void refresh()
    }

    refreshVisible()
    const timer = window.setInterval(refreshVisible, pollInterval)
    document.addEventListener('visibilitychange', refreshVisible)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', refreshVisible)
      inFlight.current?.abort()
      inFlight.current = undefined
    }
  }, [enabled, refresh])

  return { snapshot, refresh }
}
