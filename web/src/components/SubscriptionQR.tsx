import { QrcodeOutlined } from '@ant-design/icons'
import { Alert, Button, Flex, Spin, theme, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { APIError, errorMessage, fetchSubscriptionQR, type SubscriptionFormat } from '../api'

interface SubscriptionQRProps {
  deviceId: string
  format: SubscriptionFormat
  formatName: string
  onExpired: () => void
}

export default function SubscriptionQR({ deviceId, format, formatName, onExpired }: SubscriptionQRProps) {
  const { token } = theme.useToken()
  const [attempt, setAttempt] = useState(0)
  const [image, setImage] = useState('')
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    let objectURL = ''

    async function load() {
      setBusy(true)
      setError('')
      setImage('')
      try {
        const png = await fetchSubscriptionQR(deviceId, format, controller.signal)
        if (controller.signal.aborted) return
        objectURL = URL.createObjectURL(png)
        setImage(objectURL)
      } catch (failure) {
        if (controller.signal.aborted) return
        if (failure instanceof APIError && failure.status === 401) onExpired()
        else setError(errorMessage(failure))
      } finally {
        if (!controller.signal.aborted) setBusy(false)
      }
    }

    void load()
    return () => {
      controller.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [attempt, deviceId, format, onExpired])

  return (
    <section aria-label="订阅二维码" aria-busy={busy}>
      <Flex wrap="wrap" align="center" gap={16}>
        <Flex
          align="center" justify="center"
          style={{
            width: 146, height: 146, flexShrink: 0,
            background: token.colorBgContainer, border: `1px solid ${token.colorBorderSecondary}`,
            borderRadius: token.borderRadiusLG,
          }}
        >
          {image ? <img src={image} alt={`${formatName}订阅二维码`} width={128} height={128} /> :
            busy ? <Spin /> : <QrcodeOutlined style={{ fontSize: 24, color: token.colorTextTertiary }} />}
        </Flex>
        <Flex vertical gap={6} align="start" style={{ flex: '1 1 160px' }}>
          <Typography.Text strong>扫码订阅</Typography.Text>
          {busy ? <Typography.Text type="secondary" role="status">正在获取二维码…</Typography.Text> : error ? (
            <>
              <Alert type="error" message={error} showIcon />
              <Button size="small" onClick={() => setAttempt((value) => value + 1)}>重试</Button>
            </>
          ) : <Typography.Text type="secondary">使用 {formatName} 扫描二维码导入订阅。</Typography.Text>}
        </Flex>
      </Flex>
    </section>
  )
}
