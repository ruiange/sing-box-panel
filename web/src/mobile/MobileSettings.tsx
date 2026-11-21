import { Button, Card, DotLoading, Input, Radio, Tag } from 'antd-mobile'
import type { ChangeEvent, ReactNode } from 'react'
import type { AccessMode } from '../api'
import { maxPEMSize, modeLabels, modeOptions, useAccessSettings } from '../useAccessSettings'
import { useSiteTitleForm, type SiteSettingsProps } from '../useSiteTitleForm'
import './mobile-settings.css'

function SettingsNotice({ tone, children }: { tone: 'info' | 'warning' | 'error' | 'success'; children: ReactNode }) {
  return (
    <p className="mobile-settings-notice" data-tone={tone} role={tone === 'error' ? 'alert' : 'status'}>
      {children}
    </p>
  )
}

function SettingsLoading({ children }: { children: string }) {
  return <div className="mobile-settings-loading" role="status"><DotLoading color="currentColor" />{children}</div>
}

function SettingsLink({ address, children }: { address: string; children?: ReactNode }) {
  return (
    <a href={address.replace(/\/$/, '') + '/m/access'} target="_blank" rel="noopener noreferrer">
      {children ?? address}
    </a>
  )
}

interface CertificatePickerProps {
  label: string
  accept: string
  file?: File
  disabled: boolean
  onSelect: (file?: File) => void
  onError: (value: string) => void
}

function CertificatePicker({ label, accept, file, disabled, onSelect, onError }: CertificatePickerProps) {
  function pick(event: ChangeEvent<HTMLInputElement>) {
    const selected = event.currentTarget.files?.[0]
    event.currentTarget.value = ''
    if (!selected) return
    if (selected.size > maxPEMSize) {
      onSelect(undefined)
      onError('每个 PEM 文件最多 64 KiB。')
      return
    }
    onError('')
    onSelect(selected)
  }

  return (
    <div className="mobile-settings-field">
      <span className="mobile-settings-label">{label}</span>
      <div className="mobile-settings-file">
        <label className="mobile-settings-picker" data-disabled={disabled || undefined}>
          {file ? `更换${label}` : `选择${label}`}
          <input type="file" accept={accept} disabled={disabled} onChange={pick} />
        </label>
        {file && (
          <>
            <span className="mobile-settings-filename">{file.name}</span>
            <Button
              fill="none"
              size="small"
              disabled={disabled}
              aria-label={`移除${label}`}
              onClick={() => { onSelect(undefined); onError('') }}
            >
              移除
            </Button>
          </>
        )}
      </div>
    </div>
  )
}

function TitleSettings(props: SiteSettingsProps) {
  const { siteTitle, titleLoading, titleError, onLoadTitle } = props
  const { draft, saving, saveError, saved, tooLong, canSave, changeTitle, save } = useSiteTitleForm(props)

  return (
    <Card title={<h2>网站标题</h2>}>
      {titleLoading ? <SettingsLoading>正在读取网站标题…</SettingsLoading> : siteTitle ? (
        <form
          className="mobile-settings-form"
          onSubmit={(event) => { event.preventDefault(); void save() }}
          aria-labelledby="mobile-title-label"
          aria-describedby="mobile-title-help"
        >
          <div className="mobile-settings-field">
            <label className="mobile-settings-label" id="mobile-title-label" htmlFor="mobile-site-title">标题</label>
            <Input
              id="mobile-site-title"
              value={draft}
              disabled={saving}
              enterKeyHint="done"
              data-invalid={tooLong || undefined}
              onChange={changeTitle}
            />
            <p className="mobile-settings-note" id="mobile-title-help">显示在登录页、后台和浏览器标签页，最多 80 个字符。</p>
          </div>
          {tooLong && <SettingsNotice tone="error">网站标题最多 80 个字符。</SettingsNotice>}
          {saveError && <SettingsNotice tone="error">{saveError}</SettingsNotice>}
          {saved && <SettingsNotice tone="success">网站标题已保存。</SettingsNotice>}
          <Button block color="primary" type="submit" loading={saving} disabled={!canSave}>保存标题</Button>
        </form>
      ) : (
        <div className="mobile-settings-form">
          {titleError && <SettingsNotice tone="error">{titleError}</SettingsNotice>}
          <Button block color="primary" fill="outline" onClick={onLoadTitle}>重新读取标题</Button>
        </div>
      )}
    </Card>
  )
}

function DomainSettings({ onExpired }: { onExpired: () => void }) {
  const {
    settings, mode, domain, certificate, privateKey, loading, saving, error, feedback, canUpload, changesEntry, expiresAt,
    cannotSave, changeMode, changeDomain, setCertificate, setPrivateKey, setError, load, save,
  } = useAccessSettings(onExpired)

  return (
    <Card
      title={<h2>域名与证书</h2>}
      extra={settings?.available && (
        <Tag color={settings.mode === 'off' ? 'default' : '#147345'} fill="outline">{modeLabels[settings.mode]}</Tag>
      )}
    >
      {loading ? <SettingsLoading>正在读取域名与证书配置…</SettingsLoading> : settings ? (
        <div className="mobile-settings-form">
          <dl className="mobile-settings-details">
            <dt>HTTPS 入口</dt>
            <dd>{settings.httpsUrl ? <SettingsLink address={settings.httpsUrl} /> : '未设置'}</dd>
            {expiresAt && <>
              <dt>证书到期</dt>
              <dd><time dateTime={settings.expiresAt}>{expiresAt}</time></dd>
            </>}
            {settings.fallbackUrl && <>
              <dt>原 IP 入口</dt>
              <dd><SettingsLink address={settings.fallbackUrl} /></dd>
            </>}
          </dl>
          {settings.warning && <SettingsNotice tone="warning">{settings.warning}</SettingsNotice>}
          {settings.available && settings.httpsUrl && !canUpload && (
            <p className="mobile-settings-note">打开 HTTPS 入口后，需要重新登录。</p>
          )}
          {settings.available ? (
            <form
              className="mobile-settings-form mobile-settings-access-form"
              onSubmit={(event) => { event.preventDefault(); void save() }}
              aria-labelledby="mobile-certificate-mode"
            >
              <div className="mobile-settings-field">
                <span className="mobile-settings-label" id="mobile-certificate-mode">证书方式</span>
                <div className="mobile-settings-modes" role="radiogroup" aria-labelledby="mobile-certificate-mode">
                  <Radio.Group value={mode} disabled={saving} onChange={(value) => changeMode(value as AccessMode)}>
                    {modeOptions.map((option) => <Radio key={option.value} value={option.value}>{option.label}</Radio>)}
                  </Radio.Group>
                </div>
              </div>
              {mode !== 'off' && (
                <div className="mobile-settings-field">
                  <label className="mobile-settings-label" htmlFor="mobile-access-domain">面板域名</label>
                  <Input
                    id="mobile-access-domain"
                    value={domain}
                    onChange={changeDomain}
                    disabled={saving}
                    maxLength={253}
                    placeholder="panel.example.com"
                    inputMode="url"
                    autoComplete="off"
                    autoCapitalize="none"
                    autoCorrect="off"
                  />
                </div>
              )}
              {mode === 'auto' && (
                <p className="mobile-settings-note">
                  将域名的 A / AAAA 记录指向本服务器，放行公网 TCP 443。使用 Cloudflare 时请选择「仅 DNS」。应用即同意 Let’s Encrypt 的
                  <a href="https://letsencrypt.org/repository/" target="_blank" rel="noopener noreferrer">服务条款</a>，证书将自动续期。
                </p>
              )}
              {mode === 'manual' && (canUpload ? (
                <>
                  <CertificatePicker label="证书链" accept=".pem,.crt,.cer" file={certificate} disabled={saving} onSelect={setCertificate} onError={setError} />
                  <CertificatePicker label="私钥" accept=".pem,.key" file={privateKey} disabled={saving} onSelect={setPrivateKey} onError={setError} />
                  <p className="mobile-settings-note">上传 PEM 格式的完整证书链和对应的未加密私钥。手动证书到期前需重新上传。</p>
                </>
              ) : (
                <SettingsNotice tone="info">
                  {settings.httpsUrl ? (
                    <>请从 <SettingsLink address={settings.httpsUrl}>HTTPS 入口</SettingsLink>登录后再上传私钥。</>
                  ) : '请先使用自动签发开启 HTTPS，再从 HTTPS 地址登录并上传私钥。'}
                </SettingsNotice>
              ))}
              {changesEntry && <SettingsNotice tone="warning">应用后当前域名地址可能断开，可通过原 IP 入口重新登录。</SettingsNotice>}
              {error && <SettingsNotice tone="error">{error}</SettingsNotice>}
              {feedback && <SettingsNotice tone="success">{feedback}</SettingsNotice>}
              {saving && (
                <SettingsLoading>{mode === 'auto' ? '正在签发证书，可能需要一分钟，请保持页面打开。' : '正在应用配置…'}</SettingsLoading>
              )}
              <Button block type="submit" color={mode === 'off' ? 'danger' : 'primary'} loading={saving} disabled={cannotSave}>
                {mode === 'off' ? '关闭 HTTPS' : '应用配置'}
              </Button>
            </form>
          ) : (
            <SettingsNotice tone="info">当前部署模式由服务器配置管理，网页内无法修改。</SettingsNotice>
          )}
        </div>
      ) : (
        <div className="mobile-settings-form">
          {error && <SettingsNotice tone="error">{error}</SettingsNotice>}
          <Button block color="primary" fill="outline" onClick={load}>重新读取配置</Button>
        </div>
      )}
    </Card>
  )
}

export default function MobileSettings(props: SiteSettingsProps) {
  return (
    <div className="mobile-settings">
      <TitleSettings {...props} />
      <DomainSettings onExpired={props.onExpired} />
    </div>
  )
}
