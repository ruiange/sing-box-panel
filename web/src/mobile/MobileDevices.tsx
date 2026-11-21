import { ArrowDownOutlined, ArrowUpOutlined, MobileOutlined } from '@ant-design/icons'
import { Button, Card, DotLoading, Empty, Input, SearchBar, Segmented, Tag, Toast } from 'antd-mobile'
import { useCallback, useEffect, useId, useMemo, useRef, useState, type FormEvent } from 'react'
import { api, APIError, errorMessage, type Device, type LiveRate, type PeriodUsage, type Traffic } from '../api'
import {
  beginDeviceCreation, confirmDeviceCreations, hasPendingDeviceCreation, submitDeviceCreation, type DeviceCreateRequest,
} from '../deviceCreation'
import { formatBytes, formatRate, formatTime, totalUsage } from '../format'
import type { useLiveTraffic } from '../useLiveTraffic'
import { MobileDeviceDestinations, MobileDeviceSubscription } from './MobileDeviceDetails'
import MobileDeviceSheet from './MobileDeviceSheet'
import './mobile-devices.css'

interface MobileDevicesProps {
  devices: Device[]
  traffic?: Traffic
  live: ReturnType<typeof useLiveTraffic>
  loading: boolean
  deviceError: string
  onTraffic: (deviceId: string) => void
  onExpired: () => void
  onSaved: (device: Device) => void
}

type TargetedAction = 'rename' | 'toggle' | 'subscription' | 'destinations' | 'more'
type DeviceAction = { type: 'create' } | { type: TargetedAction; deviceId: string }
type DeviceFilter = 'all' | 'enabled' | 'disabled'

const filters = [
  { value: 'all', label: '全部' },
  { value: 'enabled', label: '已启用' },
  { value: 'disabled', label: '已停用' },
]
const periods = [
  { key: 'today', title: '今日用量' },
  { key: 'month', title: '本月用量' },
  { key: 'all', title: '累计用量' },
] as const
const statuses = {
  active: { label: '活跃', color: '#08734f' },
  idle: { label: '暂无活动', color: '#50647c' },
  disabled: { label: '已停用', color: '#657084' },
  unknown: { label: '状态未知', color: '#8a4b05' },
}

interface MobileDeviceCardProps {
  device: Device
  usage?: PeriodUsage
  rate?: LiveRate
  status: keyof typeof statuses
  lastActiveAt?: string
  timeZone?: string
  onTraffic: MobileDevicesProps['onTraffic']
  onAction: (action: DeviceAction) => void
}

function MobileDeviceCard({
  device, usage, rate, status, lastActiveAt, timeZone, onTraffic, onAction,
}: MobileDeviceCardProps) {
  const titleId = useId()

  return (
    <article aria-labelledby={titleId}>
      <Card
        className="mobile-device-card"
        title={(
          <div className="mobile-device-card-title">
            <span className="mobile-device-icon"><MobileOutlined aria-hidden="true" /></span>
            <div>
              <h2 id={titleId}>{device.name}</h2>
              <p>
                {lastActiveAt ? <>最后活跃 <time dateTime={lastActiveAt}>{formatTime(lastActiveAt, timeZone)}</time></>
                  : '暂无活跃记录'}
              </p>
            </div>
          </div>
        )}
        extra={<Tag className="mobile-device-status" color={statuses[status].color} fill="outline" round>{statuses[status].label}</Tag>}
      >
        <dl className="mobile-device-usage">
          {periods.map(({ key, title }) => (
            <div key={key}>
              <dt>{title}</dt>
              <dd>{usage ? formatBytes(totalUsage(usage[key])) : '—'}</dd>
            </div>
          ))}
        </dl>
        <div className="mobile-device-rate">
          <span>实时网速</span>
          <span><ArrowDownOutlined aria-hidden="true" /><span className="mobile-device-visually-hidden">下载 </span>{rate ? formatRate(rate.download) : '—'}</span>
          <span><ArrowUpOutlined aria-hidden="true" /><span className="mobile-device-visually-hidden">上传 </span>{rate ? formatRate(rate.upload) : '—'}</span>
        </div>
        <div className="mobile-device-actions">
          <Button size="small" color="primary" fill="none" onClick={() => onTraffic(device.id)}>流量</Button>
          {device.enabled && (
            <Button size="small" fill="none" onClick={() => onAction({ type: 'subscription', deviceId: device.id })}>订阅</Button>
          )}
          <Button size="small" fill="none" onClick={() => onAction({ type: 'destinations', deviceId: device.id })}>访问记录</Button>
          <Button size="small" fill="none" onClick={() => onAction({ type: 'more', deviceId: device.id })}>更多</Button>
        </div>
      </Card>
    </article>
  )
}

type EditorAction = { type: 'create' } | { type: 'rename' | 'toggle'; device: Device }

interface MobileDeviceEditorProps {
  action: EditorAction
  devices: Device[]
  onClose: () => void
  onExpired: MobileDevicesProps['onExpired']
  onSaved: MobileDevicesProps['onSaved']
}

function MobileDeviceEditor({ action, devices, onClose, onExpired, onSaved }: MobileDeviceEditorProps) {
  const nameId = useId()
  const [name, setName] = useState(action.type === 'rename' ? action.device.name : '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const mounted = useRef(false)
  const saving = useRef(false)
  const submitted = useRef<DeviceCreateRequest>()
  const disabling = action.type === 'toggle' && action.device.enabled
  const title = action.type === 'create' ? '新增设备' : action.type === 'rename' ? '重命名设备' : disabling ? '停用设备' : '启用设备'
  const pending = action.type === 'create' && hasPendingDeviceCreation(name.trim())
  const submitLabel = action.type === 'toggle' ? disabling ? '确认停用' : '确认启用' : pending ? '确认上次提交' : '保存'

  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  const complete = useCallback((device: Device) => {
    submitted.current = undefined
    if (action.type === 'create') confirmDeviceCreations([device])
    onClose()
    onSaved(device)
    Toast.show({ content: action.type === 'create' ? '已创建设备' : '已更新设备' })
  }, [action.type, onClose, onSaved])

  useEffect(() => {
    const request = submitted.current
    if (action.type !== 'create' || !request || request.name !== name.trim()) return
    const device = devices.find((item) => item.id === request.requestId)
    if (device) complete(device)
  }, [action.type, complete, devices, name])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (saving.current) return
    const trimmed = name.trim()
    if (action.type !== 'toggle' && (!trimmed || Array.from(trimmed).length > 48)) {
      setError('请输入 1 至 48 个字符的设备名称。')
      return
    }
    saving.current = true
    setBusy(true)
    setError('')

    let request: DeviceCreateRequest | undefined
    try {
      let device: Device
      if (action.type === 'create') {
        const creation = beginDeviceCreation(trimmed)
        request = creation
        submitted.current = creation
        device = await submitDeviceCreation(creation, () => mounted.current && submitted.current === creation)
        if (submitted.current !== creation) return
      } else {
        const changes = action.type === 'rename' ? { name: trimmed } : { enabled: !action.device.enabled }
        device = await api<Device>('/devices/' + encodeURIComponent(action.device.id), 'PATCH', changes)
      }
      if (mounted.current) complete(device)
      else if (request) confirmDeviceCreations([device])
    } catch (failure) {
      if (!mounted.current || (request && submitted.current !== request)) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else setError(errorMessage(failure))
    } finally {
      saving.current = false
      if (mounted.current) setBusy(false)
    }
  }

  return (
    <MobileDeviceSheet title={title} locked={busy} onClose={() => { if (!saving.current) onClose() }}>
      <form className="mobile-device-form" onSubmit={save}>
        {action.type === 'toggle' ? (
          <p className="mobile-device-sheet-note">
            {disabling ? `停用「${action.device.name}」后，设备将无法连接，已有流量记录会保留。`
              : `启用「${action.device.name}」后，设备可继续使用原配置连接。`}
          </p>
        ) : (
          <label className="mobile-device-field" htmlFor={nameId}>
            设备名称
            <Input id={nameId} value={name} disabled={busy} onChange={setName}
              placeholder="例如：我的手机" maxLength={96} enterKeyHint="done" />
          </label>
        )}
        {error && <p className="mobile-device-error" role="alert">{error}</p>}
        <Button block type="submit" color={disabling ? 'danger' : 'primary'} loading={busy}>{submitLabel}</Button>
      </form>
    </MobileDeviceSheet>
  )
}

export default function MobileDevices({
  devices, traffic, live, loading, deviceError, onTraffic, onExpired, onSaved,
}: MobileDevicesProps) {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<DeviceFilter>('all')
  const [action, setAction] = useState<DeviceAction>()
  const usageByDevice = useMemo(() => new Map(traffic?.devices.map((entry) => [entry.id, entry])), [traffic?.devices])
  const search = query.trim().toLocaleLowerCase()
  const visibleDevices = devices.filter((device) =>
    (filter === 'all' || device.enabled === (filter === 'enabled')) &&
    device.name.toLocaleLowerCase().includes(search),
  )
  const currentDevice = action && 'deviceId' in action ? devices.find((device) => device.id === action.deviceId) : undefined
  const snapshot = live.state === 'live' ? live.snapshot : undefined
  const closeSheet = useCallback(() => setAction(undefined), [])

  return (
    <div className="mobile-devices">
      <div className="mobile-device-tools">
        <div className="mobile-device-search" role="search" aria-label="设备搜索">
          <SearchBar value={query} onChange={setQuery} placeholder="搜索设备名称" />
        </div>
        <Button color="primary" onClick={() => setAction({ type: 'create' })}>新增设备</Button>
      </div>
      <div className="mobile-device-filters">
        <Segmented options={filters} value={filter} onChange={(value) => setFilter(value as DeviceFilter)} aria-label="设备启用状态" />
        <span>{visibleDevices.length} 台{visibleDevices.length !== devices.length && ` / 共 ${devices.length} 台`}</span>
      </div>

      {deviceError && <p className="mobile-device-error" role="alert">{deviceError}</p>}
      <div className="mobile-device-cards" aria-busy={loading && !devices.length}>
        {visibleDevices.map((device) => (
          <MobileDeviceCard
            key={device.id} device={device} usage={usageByDevice.get(device.id)} timeZone={traffic?.timeZone}
            status={device.enabled ? snapshot?.presence?.[device.id]?.state ?? 'unknown' : 'disabled'}
            rate={device.enabled ? snapshot?.devices[device.id] : undefined}
            lastActiveAt={live.lastActiveAt.get(device.id)}
            onTraffic={onTraffic} onAction={setAction}
          />
        ))}
        {!visibleDevices.length && !deviceError && (
          loading && !devices.length
            ? <div className="mobile-device-placeholder" role="status"><DotLoading color="primary" /> 正在读取设备…</div>
            : <Empty description={devices.length ? '没有符合条件的设备' : '还没有设备'} />
        )}
      </div>

      {action?.type === 'create' && (
        <MobileDeviceEditor action={action} devices={devices} onClose={closeSheet} onExpired={onExpired} onSaved={onSaved} />
      )}
      {currentDevice && (action?.type === 'rename' || action?.type === 'toggle') && (
        <MobileDeviceEditor
          key={`${action.type}-${currentDevice.id}`}
          action={{ type: action.type, device: currentDevice }}
          devices={devices} onClose={closeSheet} onExpired={onExpired} onSaved={onSaved}
        />
      )}
      {currentDevice && action?.type === 'subscription' && (
        <MobileDeviceSubscription key={currentDevice.id} device={currentDevice} onClose={closeSheet} onExpired={onExpired} />
      )}
      {currentDevice && action?.type === 'destinations' && (
        <MobileDeviceDestinations
          key={currentDevice.id} device={currentDevice} timeZone={traffic?.timeZone} onClose={closeSheet} onExpired={onExpired}
        />
      )}
      {currentDevice && action?.type === 'more' && (
        <MobileDeviceSheet title="设备操作" description={currentDevice.name} onClose={closeSheet}>
          <div className="mobile-device-form">
            <Button block onClick={() => setAction({ type: 'rename', deviceId: currentDevice.id })}>重命名设备</Button>
            <Button
              block color={currentDevice.enabled ? 'danger' : 'primary'} fill="outline"
              onClick={() => setAction({ type: 'toggle', deviceId: currentDevice.id })}
            >{currentDevice.enabled ? '停用设备' : '启用设备'}</Button>
          </div>
        </MobileDeviceSheet>
      )}
    </div>
  )
}
