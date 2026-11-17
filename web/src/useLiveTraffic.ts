import { onBeforeUnmount, ref, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import type { LiveTraffic } from './api'

const liveSampleTimeout = 8000

export function useLiveTraffic(enabled: Readonly<Ref<boolean>>, deviceId: Readonly<Ref<string>>, expired: () => void) {
  const snapshot = shallowRef<LiveTraffic>()
  const state = ref<'connecting' | 'waiting' | 'live' | 'reconnecting'>('connecting')
  const lastActiveAt = shallowReactive(new Map<string, string>())
  let socket: WebSocket | undefined
  let retry: number | undefined
  let stale: number | undefined
  let retryDelay = 1000
  let lastSample: string | undefined

  function subscribe() {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'subscribe', deviceId: deviceId.value }))
    }
  }

  function clearData() {
    window.clearTimeout(stale)
    snapshot.value = undefined
  }

  function connect() {
    if (!enabled.value || socket) return
    const endpoint = new URL('/api/traffic/live', window.location.href)
    endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:'
    const connection = new WebSocket(endpoint)
    socket = connection
    connection.onopen = () => {
      if (socket !== connection) return
      state.value = 'waiting'
      subscribe()
    }
    connection.onmessage = ({ data }) => {
      if (socket !== connection || typeof data !== 'string') return
      let incoming: LiveTraffic
      try {
        const message = JSON.parse(data)
        if (message?.code !== 0) return
        incoming = message.data
        if (!incoming || typeof incoming.deviceId !== 'string' || !Array.isArray(incoming.history) ||
            !incoming.devices || (incoming.at !== null && !Number.isFinite(Date.parse(incoming.at)))) return
      } catch {
        return
      }
      for (const [id, presence] of Object.entries(incoming.presence ?? {})) {
        const reported = presence?.lastActiveAt
        if (typeof reported !== 'string') continue
        const timestamp = Date.parse(reported)
        const previous = lastActiveAt.get(id)
        if (Number.isFinite(timestamp) && (!previous || timestamp > Date.parse(previous))) {
          lastActiveAt.set(id, reported)
        }
      }
      retryDelay = 1000
      if (incoming.at === null) {
        clearData()
        state.value = 'waiting'
      } else if (incoming.at !== lastSample) {
        lastSample = incoming.at
        state.value = 'live'
        window.clearTimeout(stale)
        stale = window.setTimeout(() => {
          snapshot.value = undefined
          state.value = 'waiting'
        }, liveSampleTimeout)
      } else if (state.value !== 'live') {
        return
      }
      snapshot.value = incoming
    }
    connection.onclose = ({ code }) => {
      if (socket !== connection) return
      socket = undefined
      clearData()
      lastSample = undefined
      if (!enabled.value) return
      if (code === 4001) {
        expired()
        return
      }
      state.value = 'reconnecting'
      retry = window.setTimeout(connect, retryDelay)
      retryDelay = Math.min(retryDelay * 2, 15000)
    }
  }

  function stop() {
    window.clearTimeout(retry)
    clearData()
    lastActiveAt.clear()
    const connection = socket
    socket = undefined
    connection?.close()
    retryDelay = 1000
    lastSample = undefined
    state.value = 'connecting'
  }

  watch(enabled, (active) => active ? connect() : stop())
  watch(deviceId, subscribe)
  onBeforeUnmount(stop)

  return { snapshot, state, lastActiveAt }
}
