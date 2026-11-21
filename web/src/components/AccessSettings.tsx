import { ExportOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons'
import { ProCard } from '@ant-design/pro-components'
import { Alert, Button, Descriptions, Flex, Form, Input, Radio, Spin, Tag, Typography, Upload } from 'antd'
import type { AccessMode } from '../api'
import { maxPEMSize, modeLabels, modeOptions, useAccessSettings } from '../useAccessSettings'

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
  const {
    settings, mode, domain, certificate, privateKey, loading, saving, error, feedback, canUpload, changesEntry, expiresAt,
    cannotSave, changeMode, changeDomain, setCertificate, setPrivateKey, setError, load, save,
  } = useAccessSettings(onExpired)

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
                  onChange={(event) => changeMode(event.target.value as AccessMode)}
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
                  onChange={(event) => changeDomain(event.target.value)}
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
                  disabled={cannotSave}
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
