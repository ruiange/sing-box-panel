import { CopyOutlined, LinkOutlined } from '@ant-design/icons'
import { Alert, Button, Flex, Input, Modal, Segmented, Space, Typography, type InputRef } from 'antd'
import { useEffect, useId, useRef, useState, type FormEvent, type MouseEvent } from 'react'
import { api, APIError, errorMessage, type Device, type SubscriptionFormat } from '../api'
import {
  beginDeviceCreation, confirmDeviceCreations, hasPendingDeviceCreation, submitDeviceCreation, type DeviceCreateRequest,
} from '../deviceCreation'
import DeviceDestinations from './DeviceDestinations'
import SubscriptionQR from './SubscriptionQR'

type EditorDialog = { type: 'create' } | { type: 'rename' | 'toggle'; device: Device }
export type DeviceDialog = EditorDialog | { type: 'subscription'; device: Device } | { type: 'destinations'; device: Device }

interface DialogCallbacks {
  onClose: () => void
  onExpired: () => void
}

interface DeviceDialogsProps extends DialogCallbacks {
  dialog: DeviceDialog | undefined
  devices: Device[]
  timeZone?: string
  onSaved: (device: Device, notice: string) => void
}

const formats: Array<{ label: string; value: SubscriptionFormat }> = [
  { value: 'shadowrocket', label: '小火箭' },
  { value: 'clash', label: 'Clash' },
  { value: 'sing-box', label: 'sing-box' },
  { value: 'v2ray', label: 'v2rayN' },
]

function DeviceEditorDialog({
  dialog, devices, onClose, onExpired, onSaved,
}: DialogCallbacks & { dialog: EditorDialog; devices: Device[]; onSaved: DeviceDialogsProps['onSaved'] }) {
  const formId = useId()
  const nameId = useId()
  const [name, setName] = useState(dialog.type === 'rename' ? dialog.device.name : '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const mounted = useRef(false)
  const saving = useRef(false)
  const submitted = useRef<DeviceCreateRequest>()

  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  useEffect(() => {
    const request = submitted.current
    if (dialog.type !== 'create' || !request || request.name !== name.trim()) return
    const device = devices.find((item) => item.id === request.requestId)
    if (!device) return
    submitted.current = undefined
    saving.current = false
    confirmDeviceCreations([device])
    onClose()
    onSaved(device, '已创建设备')
  }, [devices, dialog.type, name, onClose, onSaved])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (saving.current) return
    const trimmed = name.trim()
    if (dialog.type !== 'toggle' && (!trimmed || Array.from(trimmed).length > 48)) {
      setError('请输入 1 至 48 个字符的设备名称。')
      return
    }
    saving.current = true
    setBusy(true)
    setError('')

    try {
      let device: Device
      if (dialog.type === 'create') {
        const request = beginDeviceCreation(trimmed)
        submitted.current = request
        device = await submitDeviceCreation(request, () => mounted.current && submitted.current === request)
        confirmDeviceCreations([device])
        if (submitted.current !== request) return
        submitted.current = undefined
      } else {
        device = await api<Device>('/devices/' + encodeURIComponent(dialog.device.id), 'PATCH',
          dialog.type === 'rename' ? { name: trimmed } : { enabled: !dialog.device.enabled })
      }
      if (!mounted.current) return
      onClose()
      onSaved(device, dialog.type === 'create' ? '已创建设备' : '已更新设备')
    } catch (failure) {
      if (!mounted.current || !saving.current) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else setError(errorMessage(failure))
    } finally {
      saving.current = false
      if (mounted.current) setBusy(false)
    }
  }

  const disabling = dialog.type === 'toggle' && dialog.device.enabled
  const title = dialog.type === 'create' ? '新增设备' : dialog.type === 'rename' ? '重命名设备' : disabling ? '停用设备' : '启用设备'
  const pending = dialog.type === 'create' && hasPendingDeviceCreation(name.trim())
  const confirmText = dialog.type === 'toggle' ? disabling ? '确认停用' : '确认启用' : pending ? '确认上次提交' : '保存'

  return (
    <Modal
      open
      title={title}
      okText={busy ? '正在保存…' : confirmText}
      cancelText="取消"
      confirmLoading={busy}
      okButtonProps={{ danger: disabling, htmlType: 'submit', form: formId }}
      cancelButtonProps={{ disabled: busy }}
      closable={!busy}
      keyboard={!busy}
      maskClosable={false}
      onCancel={() => { if (!saving.current) onClose() }}
    >
      <form id={formId} onSubmit={save}>
        <Flex vertical gap={12}>
          {dialog.type === 'toggle' ? (
            <Typography.Text>
              {disabling
                ? `停用「${dialog.device.name}」后，设备将无法连接，已有流量记录会保留。`
                : `启用「${dialog.device.name}」后，设备可继续使用原配置连接。`}
            </Typography.Text>
          ) : (
            <Flex vertical gap={6}>
              <label htmlFor={nameId}>设备名称</label>
              <Input
                id={nameId}
                value={name}
                placeholder="例如：我的手机"
                onChange={(event) => setName(event.target.value)}
                maxLength={96}
                disabled={busy}
                required
                autoFocus
              />
            </Flex>
          )}
          {error && <Alert type="error" message={error} showIcon />}
        </Flex>
      </form>
    </Modal>
  )
}

function DeviceSubscriptionDialog({ device, onClose, onExpired }: DialogCallbacks & { device: Device }) {
  const inputId = useId()
  const input = useRef<InputRef>(null)
  const rotation = useRef<AbortController>()
  const [attempt, setAttempt] = useState(0)
  const [baseUrl, setBaseUrl] = useState('')
  const [format, setFormat] = useState<SubscriptionFormat>('shadowrocket')
  const [busy, setBusy] = useState<'loading' | 'rotating' | null>('loading')
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')
  const [feedback, setFeedback] = useState('')
  const devicePath = '/devices/' + encodeURIComponent(device.id)
  const subscriptionURL = baseUrl ? baseUrl.replace(/\/$/, '') + '/' + format : ''
  const formatName = formats.find((item) => item.value === format)?.label ?? format

  useEffect(() => {
    const controller = new AbortController()
    setBusy('loading')
    setError('')

    async function loadSubscription() {
      try {
        const result = await api<{ url: string }>(devicePath + '/subscription', 'GET', undefined, controller.signal)
        if (!controller.signal.aborted) setBaseUrl(result.url)
      } catch (failure) {
        if (controller.signal.aborted) return
        if (failure instanceof APIError && failure.status === 401) onExpired()
        else setError(errorMessage(failure))
      } finally {
        if (!controller.signal.aborted) setBusy(null)
      }
    }

    void loadSubscription()
    return () => {
      controller.abort()
      rotation.current?.abort()
    }
  }, [attempt, devicePath, onExpired])

  async function rotateSubscription() {
    if (busy || rotation.current) return
    if (!confirming) {
      setConfirming(true)
      return
    }
    const controller = new AbortController()
    rotation.current = controller
    setBusy('rotating')
    setError('')
    setFeedback('')

    try {
      const result = await api<{ url: string }>(devicePath + '/rotate-subscription', 'POST', undefined, controller.signal)
      if (controller.signal.aborted) return
      setBaseUrl(result.url)
      setConfirming(false)
      setFeedback('已更新订阅链接')
    } catch (failure) {
      if (controller.signal.aborted) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else setError(errorMessage(failure))
    } finally {
      rotation.current = undefined
      if (!controller.signal.aborted) setBusy(null)
    }
  }

  async function copySubscription(event: MouseEvent<HTMLElement>) {
    if (!subscriptionURL) return
    const button = event.currentTarget
    const field = input.current?.input
    setError('')
    setFeedback('')
    try {
      await navigator.clipboard.writeText(subscriptionURL)
      if (field?.isConnected && field.value === subscriptionURL) setFeedback('已复制订阅链接')
    } catch {
      if (!field?.isConnected || field.value !== subscriptionURL) return
      field.focus()
      field.select()
      let copied = false
      try {
        copied = document.execCommand('copy')
      } catch {
        // Keep the link selected for manual copying on plain HTTP.
      }
      if (copied) {
        button.focus()
        setFeedback('已复制订阅链接')
      } else setError('已选中链接，请手动复制。')
    }
  }

  return (
    <Modal
      open
      title="设备订阅"
      width={570}
      closable={busy === null}
      keyboard={busy === null}
      maskClosable={false}
      onCancel={() => { if (!busy && !rotation.current) onClose() }}
      footer={(
        <Flex justify="end" wrap="wrap" gap={8}>
          {confirming && <Button disabled={busy !== null} onClick={() => setConfirming(false)}>取消更新</Button>}
          <Button
            icon={<LinkOutlined />}
            danger={confirming}
            loading={busy === 'rotating'}
            disabled={!baseUrl || busy === 'loading'}
            onClick={() => void rotateSubscription()}
          >{confirming ? '确认更新链接' : '更新订阅链接'}</Button>
        </Flex>
      )}
    >
      <Flex vertical gap={12}>
        <Typography.Text type="secondary">选择「{device.name}」使用的客户端。</Typography.Text>
        <Segmented<SubscriptionFormat>
          block
          aria-label="客户端格式"
          options={formats}
          value={format}
          disabled={busy === 'rotating'}
          onChange={(value) => {
            setFormat(value)
            setFeedback('')
          }}
        />
        <Flex vertical gap={6}>
          <label htmlFor={inputId}>订阅链接</label>
          <Space.Compact block>
            <Input
              id={inputId}
              ref={input}
              value={subscriptionURL}
              placeholder={busy ? '正在获取…' : '订阅链接暂不可用'}
              readOnly
            />
            <Button
              icon={<CopyOutlined />}
              disabled={!subscriptionURL || busy !== null}
              onClick={(event) => void copySubscription(event)}
            >复制</Button>
          </Space.Compact>
        </Flex>
        {subscriptionURL && !busy && (
          <SubscriptionQR key={subscriptionURL} deviceId={device.id} format={format} formatName={formatName} onExpired={onExpired} />
        )}
        {confirming && <Alert type="warning" showIcon message="更新后，旧订阅链接将失效。已导入的节点配置仍可使用。" />}
        {error && (
          <Alert
            type="error"
            showIcon
            message={error}
            action={!baseUrl && !busy ? <Button size="small" onClick={() => setAttempt((value) => value + 1)}>重试</Button> : undefined}
          />
        )}
        {feedback && <Alert type="success" showIcon message={feedback} />}
      </Flex>
    </Modal>
  )
}

export default function DeviceDialogs({ dialog, devices, timeZone, onClose, onExpired, onSaved }: DeviceDialogsProps) {
  if (!dialog) return null
  if (dialog.type === 'destinations') {
    return (
      <Modal open title="访问记录" width={760} footer={null} onCancel={onClose}>
        <DeviceDestinations deviceId={dialog.device.id} deviceName={dialog.device.name} timeZone={timeZone} onExpired={onExpired} />
      </Modal>
    )
  }
  if (dialog.type === 'subscription') return <DeviceSubscriptionDialog device={dialog.device} onClose={onClose} onExpired={onExpired} />
  return <DeviceEditorDialog dialog={dialog} devices={devices} onClose={onClose} onExpired={onExpired} onSaved={onSaved} />
}
