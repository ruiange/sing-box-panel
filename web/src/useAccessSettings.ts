import { useCallback, useEffect, useRef, useState } from 'react'
import { api, APIError, errorMessage, type AccessMode, type AccessSettings } from './api'

const endpoint = '/settings/access'
export const maxPEMSize = 64 * 1024
export const modeLabels = {
  off: '未开启',
  auto: '自动签发',
  manual: '自有证书',
} satisfies Record<AccessMode, string>

export const modeOptions: Array<{ label: string; value: AccessMode }> = [
  { label: modeLabels.auto, value: 'auto' },
  { label: modeLabels.manual, value: 'manual' },
  { label: '关闭', value: 'off' },
]

export function useAccessSettings(onExpired: () => void) {
  const [settings, setSettings] = useState<AccessSettings>()
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
  const cannotSave = (mode === 'manual' && !canUpload) || (mode === 'off' && settings?.mode === 'off' && !settings.warning)

  function changeMode(next: AccessMode) {
    setMode(next)
    setCertificate(undefined)
    setPrivateKey(undefined)
    setError('')
    setFeedback('')
  }

  function changeDomain(next: string) {
    setDomain(next)
    setError('')
    setFeedback('')
  }

  const load = useCallback(async () => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setLoading(true)
    setError('')
    try {
      const current = await api<AccessSettings>(endpoint, 'GET', undefined, controller.signal)
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
    if (request.current || !settings?.available || cannotSave) return
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
      const update = api<AccessSettings>(endpoint, 'PUT', input, controller.signal)
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

  return {
    settings, mode, domain, certificate, privateKey, loading, saving, error, feedback, canUpload, changesEntry, expiresAt,
    cannotSave, changeMode, changeDomain, setCertificate, setPrivateKey, setError, load, save,
  }
}
