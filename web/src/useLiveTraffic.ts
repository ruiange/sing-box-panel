import { useEffect, useRef, useState } from 'react'
import type { LiveTraffic } from './api'

export type LiveState = 'connecting' | 'waiting' | 'live' | 'reconnecting'

const liveSampleTimeout = 8000
const emptyLastActiveAt: ReadonlyMap<string, string> = new Map()

function sendSubscription(socket: WebSocket | undefined, deviceId: string) {
  if (socket?.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify({ type: 'subscribe', deviceId }))
  }
}

export function useLiveTraffic(enabled: boolean, deviceId: string, onExpired: () => void) {
  const [snapshot, setSnapshot] = useState<LiveTraffic>()
  const [state, setState] = useState<LiveState>('connecting')
  const [lastActiveAt, setLastActiveAt] = useState(emptyLastActiveAt)
  const socket = useRef<WebSocket>()
  const scope = useRef(deviceId)

  useEffect(() => {
    scope.current = deviceId
    sendSubscription(socket.current, deviceId)
  }, [deviceId])

  useEffect(() => {
    if (!enabled) {
      setSnapshot(undefined)
      setState('connecting')
      setLastActiveAt(emptyLastActiveAt)
      return
    }

    let retry: number | undefined
    let stale: number | undefined
    let retryDelay = 1000
    let lastSample: string | undefined
    let sampleFresh = false
    const activity = new Map<string, string>()

    function clearSnapshot() {
      window.clearTimeout(stale)
      sampleFresh = false
      setSnapshot(undefined)
    }

    function connect() {
      const endpoint = new URL('/api/traffic/live', window.location.href)
      endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:'
      const connection = new WebSocket(endpoint)
      socket.current = connection

      connection.onopen = () => {
        if (socket.current !== connection) return
        setState('waiting')
        sendSubscription(connection, scope.current)
      }

      connection.onmessage = ({ data }) => {
        if (socket.current !== connection || typeof data !== 'string') return
        const incoming = parseSnapshot(data)
        if (!incoming) return

        let activityChanged = false
        for (const [id, presence] of Object.entries(incoming.presence ?? {})) {
          const reported = presence?.lastActiveAt
          if (typeof reported !== 'string') continue
          const timestamp = Date.parse(reported)
          const previous = activity.get(id)
          if (Number.isFinite(timestamp) && (!previous || timestamp > Date.parse(previous))) {
            activity.set(id, reported)
            activityChanged = true
          }
        }
        if (activityChanged) setLastActiveAt(new Map(activity))

        retryDelay = 1000
        if (incoming.at === null) {
          clearSnapshot()
          setState('waiting')
          return
        }
        if (incoming.at !== lastSample) {
          lastSample = incoming.at
          sampleFresh = true
          setState('live')
          window.clearTimeout(stale)
          stale = window.setTimeout(() => {
            clearSnapshot()
            setState('waiting')
          }, liveSampleTimeout)
        }
        if (sampleFresh) setSnapshot(incoming)
      }

      connection.onclose = ({ code }) => {
        if (socket.current !== connection) return
        socket.current = undefined
        clearSnapshot()
        lastSample = undefined
        if (code === 4001) {
          onExpired()
          return
        }
        setState('reconnecting')
        retry = window.setTimeout(connect, retryDelay)
        retryDelay = Math.min(retryDelay * 2, 15_000)
      }
    }

    connect()
    return () => {
      window.clearTimeout(retry)
      window.clearTimeout(stale)
      const connection = socket.current
      socket.current = undefined
      connection?.close()
    }
  }, [enabled, onExpired])

  return { snapshot, state, lastActiveAt }
}

function parseSnapshot(payload: string): LiveTraffic | undefined {
  try {
    const message = JSON.parse(payload)
    if (message?.code !== 0) return undefined
    const incoming: LiveTraffic = message.data
    if (!incoming || typeof incoming.deviceId !== 'string' || !Array.isArray(incoming.history) ||
        !incoming.devices || (incoming.at !== null && !Number.isFinite(Date.parse(incoming.at)))) return undefined
    return incoming
  } catch {
    return undefined
  }
}
