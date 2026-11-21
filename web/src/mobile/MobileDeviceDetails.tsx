import { Button, DotLoading, Empty, Input, List, Segmented, Toast } from 'antd-mobile'
import { useCallback, useEffect, useId, useMemo, useRef, useState, type ComponentRef } from 'react'
import {
  api, APIError, errorMessage, fetchSubscriptionQR, type Device, type DeviceDestination,
  type DeviceDestinationPage, type SubscriptionFormat,
} from '../api'
import MobileDeviceSheet from './MobileDeviceSheet'

interface DeviceDetailProps {
  device: Device
  onClose: () => void
  onExpired: () => void
}

const formats = [
  { value: 'shadowrocket', label: '小火箭' },
  { value: 'clash', label: 'Clash' },
  { value: 'sing-box', label: 'sing-box' },
  { value: 'v2ray', label: 'v2rayN' },
] as const

function MobileDeviceQR({ deviceId, format, onExpired }: {
  deviceId: string
  format: SubscriptionFormat
  onExpired: DeviceDetailProps['onExpired']
}) {
  const [attempt, setAttempt] = useState(0)
  const [image, setImage] = useState('')
  const [error, setError] = useState('')
  const formatName = formats.find((item) => item.value === format)?.label ?? format

  useEffect(() => {
    const request = new AbortController()
    let imageUrl = ''
    setError('')
    setImage('')
    void fetchSubscriptionQR(deviceId, format, request.signal).then(
      (png) => {
        if (request.signal.aborted) return
        imageUrl = URL.createObjectURL(png)
        setImage(imageUrl)
      },
      (failure) => {
        if (request.signal.aborted) return
        if (failure instanceof APIError && failure.status === 401) onExpired()
        else setError(errorMessage(failure))
      },
    )
    return () => {
      request.abort()
      if (imageUrl) URL.revokeObjectURL(imageUrl)
    }
  }, [attempt, deviceId, format, onExpired])

  return (
    <div className="mobile-device-qr">
      {image ? (
        <>
          <img src={image} alt={`${formatName}订阅二维码`} width={160} height={160} />
          <p className="mobile-device-sheet-note">使用 {formatName} 扫码导入订阅</p>
        </>
      ) : error ? (
        <>
          <p className="mobile-device-error" role="alert">{error}</p>
          <Button size="small" onClick={() => setAttempt((number) => number + 1)}>重试二维码</Button>
        </>
      ) : (
        <div className="mobile-device-placeholder" role="status"><DotLoading color="primary" /> 正在获取二维码…</div>
      )}
    </div>
  )
}

export function MobileDeviceSubscription({ device, onClose, onExpired }: DeviceDetailProps) {
  const inputId = useId()
  const input = useRef<ComponentRef<typeof Input>>(null)
  const rotation = useRef<AbortController>()
  const [attempt, setAttempt] = useState(0)
  const [baseUrl, setBaseUrl] = useState('')
  const [format, setFormat] = useState<SubscriptionFormat>('shadowrocket')
  const [busy, setBusy] = useState<'loading' | 'rotating' | null>('loading')
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')
  const [copyNotice, setCopyNotice] = useState('')
  const devicePath = '/devices/' + encodeURIComponent(device.id)
  const url = baseUrl ? baseUrl.replace(/\/$/, '') + '/' + format : ''
  const rotateLocked = busy === 'rotating'

  useEffect(() => {
    const request = new AbortController()
    void api<{ url: string }>(devicePath + '/subscription', 'GET', undefined, request.signal).then(
      (result) => { if (!request.signal.aborted) setBaseUrl(result.url) },
      (failure) => {
        if (request.signal.aborted) return
        if (failure instanceof APIError && failure.status === 401) onExpired()
        else setError(errorMessage(failure))
      },
    ).finally(() => { if (!request.signal.aborted) setBusy(null) })
    return () => {
      request.abort()
      rotation.current?.abort()
    }
  }, [attempt, devicePath, onExpired])

  function reloadSubscription() {
    setBaseUrl('')
    setBusy('loading')
    setConfirming(false)
    setError('')
    setCopyNotice('')
    setAttempt((number) => number + 1)
  }

  async function rotateSubscription() {
    if (busy || rotation.current) return
    if (!confirming) {
      setConfirming(true)
      return
    }
    const request = new AbortController()
    rotation.current = request
    setBusy('rotating')
    setError('')
    setCopyNotice('')
    try {
      const result = await api<{ url: string }>(devicePath + '/rotate-subscription', 'POST', undefined, request.signal)
      if (request.signal.aborted) return
      setBaseUrl(result.url)
      setConfirming(false)
      Toast.show({ content: '已更新订阅链接' })
    } catch (failure) {
      if (request.signal.aborted) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else {
        setBaseUrl('')
        setConfirming(false)
        setError(errorMessage(failure))
      }
    } finally {
      rotation.current = undefined
      if (!request.signal.aborted) setBusy(null)
    }
  }

  async function copySubscription() {
    const field = input.current?.nativeElement
    if (!url || !field) return
    setCopyNotice('')
    try {
      await navigator.clipboard.writeText(url)
      if (field.isConnected && field.value === url) Toast.show({ content: '已复制订阅链接' })
    } catch {
      if (!field.isConnected || field.value !== url) return
      field.focus()
      field.select()
      field.setSelectionRange(0, url.length)
      let copied = false
      try {
        copied = document.execCommand('copy')
      } catch {
        // Keep the field selected when the browser restricts copy on plain HTTP.
      }
      if (copied) Toast.show({ content: '已复制订阅链接' })
      else setCopyNotice('已选中链接，请长按复制。')
    }
  }

  return (
    <MobileDeviceSheet title="设备订阅" description={device.name} locked={rotateLocked}
      onClose={() => { if (!rotation.current) onClose() }}>
      <div className="mobile-device-form">
        <Segmented
          block value={format} options={[...formats]} disabled={rotateLocked}
          aria-label="订阅客户端" onChange={(value) => {
            setFormat(value as SubscriptionFormat)
            setCopyNotice('')
          }}
        />
        <div className="mobile-device-field">
          <label htmlFor={inputId}>订阅链接</label>
          <div className="mobile-device-link-row">
            <Input id={inputId} ref={input} readOnly value={url} placeholder={busy ? '正在获取…' : '暂未获取到链接'} />
            <Button color="primary" fill="outline" disabled={!url || !!busy} onClick={copySubscription}>复制</Button>
          </div>
        </div>
        {copyNotice && <p className="mobile-device-sheet-note" role="status">{copyNotice}</p>}
        {url && !busy && <MobileDeviceQR key={url} deviceId={device.id} format={format} onExpired={onExpired} />}
        {error && (
          <>
            <p className="mobile-device-error" role="alert">{error}</p>
            <Button block onClick={reloadSubscription}>重新获取订阅链接</Button>
          </>
        )}
        {confirming && <p className="mobile-device-warning">更新后，旧订阅链接将失效。已导入的节点配置仍可使用。</p>}
        {url && (
          <div className="mobile-device-detail-actions">
            {confirming && <Button disabled={rotateLocked} onClick={() => setConfirming(false)}>取消</Button>}
            <Button color={confirming ? 'danger' : 'default'} loading={rotateLocked} onClick={rotateSubscription}>
              {confirming ? '确认更新' : '更新订阅链接'}
            </Button>
          </div>
        )}
      </div>
    </MobileDeviceSheet>
  )
}

type DestinationLoad = 'refresh' | 'more'

export function MobileDeviceDestinations({ device, timeZone, onClose, onExpired }: DeviceDetailProps & { timeZone?: string }) {
  const [items, setItems] = useState<DeviceDestination[]>([])
  const [nextCursor, setNextCursor] = useState<string>()
  const [busy, setBusy] = useState<DestinationLoad | null>('refresh')
  const [error, setError] = useState<{ mode: DestinationLoad; message: string }>()
  const request = useRef<AbortController>()
  const timeFormat = useMemo(() => new Intl.DateTimeFormat('zh-CN', {
    timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
  }), [timeZone])

  const load = useCallback(async (before?: string) => {
    request.current?.abort()
    const current = new AbortController()
    request.current = current
    const mode = before ? 'more' : 'refresh'
    setBusy(mode)
    setError(undefined)
    const params = new URLSearchParams({ limit: '30' })
    if (before) params.set('before', before)
    try {
      const result = await api<DeviceDestinationPage>(
        '/devices/' + encodeURIComponent(device.id) + '/destinations?' + params, 'GET', undefined, current.signal,
      )
      if (current.signal.aborted) return
      setItems((previous) => before ? [...previous, ...result.items] : result.items)
      setNextCursor(result.nextCursor)
    } catch (failure) {
      if (current.signal.aborted) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else setError({ mode, message: errorMessage(failure) })
    } finally {
      if (!current.signal.aborted) setBusy(null)
    }
  }, [device.id, onExpired])

  useEffect(() => {
    void load()
    return () => request.current?.abort()
  }, [load])

  return (
    <MobileDeviceSheet title="访问记录" description={device.name} onClose={onClose}>
      <div className="mobile-device-form">
        <div aria-busy={busy !== null}>
          {items.length ? (
            <List className="mobile-device-destination-list">
              {items.map((item) => (
                <List.Item key={item.id} description={<time dateTime={item.lastSeenAt}>{timeFormat.format(new Date(item.lastSeenAt))}</time>}>
                  <bdi>{item.address}</bdi>
                </List.Item>
              ))}
            </List>
          ) : busy ? (
            <div className="mobile-device-placeholder" role="status"><DotLoading color="primary" /> 正在读取访问记录…</div>
          ) : !error && <Empty description="暂无访问记录" />}
        </div>
        {error && <p className="mobile-device-error" role="alert">{error.message}</p>}
        <div className="mobile-device-detail-actions">
          <Button loading={busy === 'refresh'} disabled={busy === 'more'} onClick={() => load()}>
            {error?.mode === 'refresh' ? '重试刷新' : '刷新'}
          </Button>
          {nextCursor && (
            <Button color="primary" loading={busy === 'more'} disabled={busy === 'refresh'} onClick={() => load(nextCursor)}>
              {error?.mode === 'more' ? '重试加载' : '加载更早记录'}
            </Button>
          )}
        </div>
      </div>
    </MobileDeviceSheet>
  )
}
