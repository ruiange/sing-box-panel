import { useCallback, useEffect, useRef, useState } from 'react'
import { api, APIError, errorMessage, type Device, type Traffic } from './api'
import { confirmDeviceCreations } from './deviceCreation'
import type { HistoryRange } from './trafficOptions'

interface TrafficQuery {
  deviceId: string
  range: HistoryRange
}

interface TrafficResult extends TrafficQuery {
  traffic?: Traffic
  error: string
  errorRange?: HistoryRange
}

interface PanelData {
  devices: Device[]
  deviceError: string
  traffic?: TrafficResult
  scopedTraffic?: TrafficResult
  loading: boolean
}

const initialData: PanelData = { devices: [], deviceError: '', loading: true }

export function usePanelData(
  enabled: boolean,
  deviceId: string,
  range: HistoryRange,
  onExpired: () => void,
  onMissingDevice: (deviceId: string) => void,
) {
  const [data, setData] = useState(initialData)
  const query = useRef<TrafficQuery>()
  const inFlight = useRef<AbortController>()

  const refresh = useCallback(async () => {
    const currentQuery = query.current
    if (!currentQuery) return
    const { deviceId, range } = currentQuery
    inFlight.current?.abort()
    const request = new AbortController()
    inFlight.current = request
    const { signal } = request
    const path = '/traffic?' + range
    setData((previous) => ({ ...previous, loading: true }))
    const [devices, traffic, scopedTraffic] = await Promise.allSettled([
      api<Device[]>('/devices', 'GET', undefined, signal),
      api<Traffic>(path, 'GET', undefined, signal),
      deviceId ? api<Traffic>(path + '&deviceId=' + encodeURIComponent(deviceId), 'GET', undefined, signal) : undefined,
    ])
    if (inFlight.current !== request || signal.aborted) return
    inFlight.current = undefined

    const expired = [devices, traffic, scopedTraffic].some((result) =>
      result.status === 'rejected' && result.reason instanceof APIError && result.reason.status === 401,
    )
    if (expired) {
      onExpired()
      return
    }

    const updatedDevices = devices.status === 'fulfilled' ? devices.value ?? [] : undefined
    if (updatedDevices) confirmDeviceCreations(updatedDevices)
    setData((previous) => ({
      devices: updatedDevices ?? previous.devices,
      deviceError: devices.status === 'rejected' ? errorMessage(devices.reason) : '',
      traffic: readTrafficResult(traffic, { deviceId: '', range }, previous.traffic),
      scopedTraffic: deviceId ? readTrafficResult(scopedTraffic, currentQuery, previous.scopedTraffic) : undefined,
      loading: false,
    }))

    if (deviceId && updatedDevices && !updatedDevices.some((device) => device.id === deviceId)) {
      onMissingDevice(deviceId)
    }
  }, [onExpired, onMissingDevice])

  useEffect(() => {
    if (!enabled) {
      setData(initialData)
      return
    }
    query.current = { deviceId, range }
    void refresh()
    return () => {
      query.current = undefined
      inFlight.current?.abort()
      inFlight.current = undefined
    }
  }, [enabled, deviceId, range, refresh])

  useEffect(() => {
    if (!enabled) return
    const poll = window.setInterval(() => {
      if (!document.hidden && !inFlight.current) void refresh()
    }, 30_000)
    return () => window.clearInterval(poll)
  }, [enabled, refresh])

  function updateDevice(device: Device) {
    setData((previous) => ({
      ...previous,
      devices: previous.devices.some((entry) => entry.id === device.id)
        ? previous.devices.map((entry) => entry.id === device.id ? device : entry)
        : [...previous.devices, device],
      deviceError: '',
    }))
  }

  return { ...data, refresh, updateDevice }
}

function readTrafficResult(
  result: PromiseSettledResult<Traffic | undefined>,
  query: TrafficQuery,
  previous?: TrafficResult,
): TrafficResult {
  if (result.status === 'fulfilled') return { ...query, traffic: result.value, error: '' }
  const last = previous?.deviceId === query.deviceId ? previous : undefined
  return {
    ...query,
    range: last?.range ?? query.range,
    traffic: last?.traffic,
    error: errorMessage(result.reason),
    errorRange: query.range,
  }
}
