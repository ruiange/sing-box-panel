import { ExportOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons'
import { ProCard } from '@ant-design/pro-components'
import { Alert, Button, Descriptions, Flex, Form, Input, Radio, Spin, Tag, Typography, Upload } from 'antd'
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, APIError, errorMessage, type AccessMode, type AccessSettings as AccessConfiguration } from '../api'

const endpoint = '/settings/access'
const maxPEMSize = 64 * 1024
const modeLabels = {
  off: '未开启',
  auto: '自动签发',
  manual: '自有证书',
} satisfies Record<AccessMode, string>

const modeOptions: Array<{ label: string; value: AccessMode }> = [
  { label: modeLabels.auto, value: 'auto' },
  { label: modeLabels.manual, value: 'manual' },
  { label: '关闭', value: 'off' },
]

interface CertificateFileProps {
  id: string
  label: string
  accept: string
  file?: File
  disabled: boolean
  onSelect: (file?: File) => void
  onError: (message: string) => void
}

function CertificateFile({ id, label, accept, file, disabled, onSelect, onError }: CertificateFileProps) {
  return (
    <Form.Item label={label} htmlFor={id} required>
      <Upload
        accept={accept}
        maxCount={1}
        disabled={disabled}
        fileList={file ? [{ uid: id, name: file.name }] : []}
        beforeUpload={(selected) => {
          if (selected.size > maxPEMSize) {
            onSelect(undefined)
            onError('每个 PEM 文件最多 64 KiB。')
            return Upload.LIST_IGNORE
          }
          onError('')
          onSelect(selected)
          return false
        }}
        onRemove={() => onSelect(undefined)}
      >
        <Button id={id} icon={<UploadOutlined />}>选择{label}文件</Button>
      </Upload>
    </Form.Item>
  )
}

function accessPageURL(address: string) {
  return address.replace(/\/$/, '') + '/#access'
}

function accessLink(address: string) {
  return (
    <Typography.Link href={accessPageURL(address)} target="_blank" rel="noopener noreferrer" style={{ overflowWrap: 'anywhere' }}>
      {address} <ExportOutlined aria-hidden="true" />
    </Typography.Link>
  )
}

export default function AccessSettings({ onExpired }: { onExpired: () => void }) {
  const [settings, setSettings] = useState<AccessConfiguration>()
  const [mode, setMode] = useState<AccessMode>('auto')
  const [domain, setDomain] = useState('')
  const [certificate, setCertificate] = useState<File>()
  const [privateKey, setPrivateKey] = useState<File>()
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [feedback, setFeedback] = useState('')
  const request = useRef<AbortController>()

  const canUpload = settings?.manualUploadAllowed === true && window.location.protocol === 'https:'
  const changesEntry = settings?.httpsUrl && (mode === 'off' || domain.trim().toLowerCase() !== settings.domain)
  const expiration = settings?.expiresAt ? new Date(settings.expiresAt) : undefined
  const expiresAt = expiration && Number.isFinite(expiration.getTime())
    ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(expiration)
    : undefined

  const load = useCallback(async () => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setLoading(true)
    setError('')
    try {
      const current = await api<AccessConfiguration>(endpoint, 'GET', undefined, controller.signal)
      if (controller.signal.aborted) return
      setSettings(current)
      setMode(current.mode === 'off' ? 'auto' : current.mode)
      setDomain(current.domain ?? '')
    } catch (failure) {
      if (controller.signal.aborted) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else setError(errorMessage(failure))
    } finally {
      if (!controller.signal.aborted) {
        request.current = undefined
        setLoading(false)
      }
    }
  }, [onExpired])

  useEffect(() => {
    void load()
    return () => request.current?.abort()
  }, [load])

  async function save() {
    if (request.current || !settings?.available || (mode === 'manual' && !canUpload)) return
    if (mode === 'off' && settings.mode === 'off' && !settings.warning) return
    const controller = new AbortController()
    request.current = controller
    setSaving(true)
    setError('')
    setFeedback('')
    try {
      const input: { mode: AccessMode; domain?: string; certificate?: string; privateKey?: string } = { mode }
      if (mode !== 'off') {
        input.domain = domain.trim()
        if (!input.domain) throw new Error('请填写域名。')
      }
      if (mode === 'manual') {
        if (!certificate || !privateKey) throw new Error('请选择证书和私钥文件。')
        if (certificate.size > maxPEMSize || privateKey.size > maxPEMSize) throw new Error('每个 PEM 文件最多 64 KiB。')
        const [certificatePEM, privateKeyPEM] = await Promise.all([certificate.text(), privateKey.text()])
        input.certificate = certificatePEM
        input.privateKey = privateKeyPEM
      }
      if (controller.signal.aborted) return
      const update = api<AccessConfiguration>(endpoint, 'PUT', input, controller.signal)
      delete input.privateKey
      delete input.certificate
      setCertificate(undefined)
      setPrivateKey(undefined)
      const current = await update
      if (controller.signal.aborted) return
      setSettings(current)
      setMode(current.mode)
      setDomain(current.domain ?? '')
      setFeedback(current.mode === 'off' ? '域名 HTTPS 已关闭。' : '域名 HTTPS 已启用。')
    } catch (failure) {
      if (controller.signal.aborted) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else if (failure instanceof APIError && failure.status === 0 && settings.fallbackUrl) {
        setError('连接已中断。可用原 IP 地址登录，查看配置是否生效。')
      } else setError(errorMessage(failure))
    } finally {
      if (!controller.signal.aborted) {
        request.current = undefined
        setCertificate(undefined)
        setPrivateKey(undefined)
        setSaving(false)
      }
    }
  }

  return (
    <Flex vertical gap="middle">
      <ProCard
        bordered
        title="域名与证书"
        extra={settings?.available && <Tag color={settings.mode === 'off' ? 'default' : 'success'}>{modeLabels[settings.mode]}</Tag>}
      >
        {loading ? <Spin size="small" aria-label="正在读取域名与证书配置" /> : settings ? (
          <Flex vertical gap="small">
            <Typography.Text type="secondary">为网页后台配置独立的 HTTPS 访问地址。</Typography.Text>
            <Descriptions size="small" column={1}>
              <Descriptions.Item label="HTTPS 入口">
                {settings.httpsUrl ? accessLink(settings.httpsUrl) : <Typography.Text type="secondary">未设置</Typography.Text>}
              </Descriptions.Item>
              {expiresAt && <Descriptions.Item label="证书到期"><time dateTime={settings.expiresAt}>{expiresAt}</time></Descriptions.Item>}
              {settings.fallbackUrl && <Descriptions.Item label="原 IP 入口">{accessLink(settings.fallbackUrl)}</Descriptions.Item>}
            </Descriptions>
            {!settings.available && <Alert type="info" showIcon message="当前部署模式由服务器配置管理，网页内无法修改。" />}
            {settings.available && settings.httpsUrl && !canUpload && (
              <Typography.Text type="secondary">打开 HTTPS 入口后，需要重新登录。</Typography.Text>
            )}
            {settings.warning && <Alert type="warning" showIcon message={settings.warning} />}
          </Flex>
        ) : (
          <Flex vertical align="start" gap="middle">
            {error && <Alert type="error" showIcon message={error} />}
            <Button icon={<ReloadOutlined />} onClick={() => void load()}>重试</Button>
          </Flex>
        )}
      </ProCard>

      {settings?.available && (
        <ProCard bordered title="配置 HTTPS">
          <Form layout="vertical" disabled={saving} onFinish={save}>
            <Form.Item label={<span id="access-mode-label">证书方式</span>}>
              <div role="radiogroup" aria-labelledby="access-mode-label">
                <Radio.Group
                  name="access-mode"
                  value={mode}
                  options={modeOptions}
                  onChange={(event) => {
                    setMode(event.target.value as AccessMode)
                    setCertificate(undefined)
                    setPrivateKey(undefined)
                    setError('')
                    setFeedback('')
                  }}
                />
              </div>
            </Form.Item>
            {mode !== 'off' && (
              <Form.Item label="面板域名" htmlFor="access-domain" required>
                <Input
                  id="access-domain"
                  value={domain}
                  maxLength={253}
                  placeholder="panel.example.com"
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  aria-required="true"
                  onChange={(event) => {
                    setDomain(event.target.value)
                    setError('')
                    setFeedback('')
                  }}
                />
              </Form.Item>
            )}
            {mode === 'manual' && canUpload && (
              <>
                <CertificateFile id="access-certificate" label="证书链" accept=".pem,.crt,.cer" file={certificate} disabled={saving} onSelect={setCertificate} onError={setError} />
                <CertificateFile id="access-private-key" label="私钥" accept=".pem,.key" file={privateKey} disabled={saving} onSelect={setPrivateKey} onError={setError} />
              </>
            )}
            <Flex vertical gap="middle" align="stretch">
              {mode === 'auto' && (
                <Typography.Text type="secondary">
                  将域名的 A / AAAA 记录指向本服务器，放行公网 TCP 443。使用 Cloudflare 时请选择「仅 DNS」。应用即同意 Let’s Encrypt 的
                  <Typography.Link href="https://letsencrypt.org/repository/" target="_blank" rel="noopener noreferrer">服务条款</Typography.Link>，证书将自动续期。
                </Typography.Text>
              )}
              {mode === 'manual' && (canUpload ? (
                <Typography.Text type="secondary">上传 PEM 格式的完整证书链和对应的未加密私钥。手动证书到期前需重新上传。</Typography.Text>
              ) : (
                <Alert
                  type="info"
                  showIcon
                  message={settings.httpsUrl ? (
                    <>请从 <Typography.Link href={accessPageURL(settings.httpsUrl)} target="_blank" rel="noopener noreferrer">HTTPS 入口</Typography.Link>登录后再上传私钥。</>
                  ) : '请先使用自动签发开启 HTTPS，再从 HTTPS 地址登录并上传私钥。'}
                />
              ))}
              {changesEntry && <Alert type="warning" showIcon message="应用后当前域名地址可能断开，可通过原 IP 入口重新登录。" />}
              {error && <Alert type="error" showIcon message={error} />}
              {feedback && <Alert type="success" showIcon role="status" message={feedback} />}
              {saving && (
                <Typography.Text type="secondary" role="status">
                  {mode === 'auto' ? '正在签发证书，可能需要一分钟，请保持页面打开。' : '正在应用配置…'}
                </Typography.Text>
              )}
              <Flex justify="end">
                <Button
                  type="primary"
                  htmlType="submit"
                  danger={mode === 'off'}
                  loading={saving}
                  disabled={(mode === 'manual' && !canUpload) || (mode === 'off' && settings.mode === 'off' && !settings.warning)}
                >
                  {mode === 'off' ? '关闭 HTTPS' : '应用配置'}
                </Button>
              </Flex>
            </Flex>
          </Form>
        </ProCard>
      )}
    </Flex>
  )
}
