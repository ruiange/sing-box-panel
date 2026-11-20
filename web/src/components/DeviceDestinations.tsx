import { ArrowDownOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Flex, Table, Typography, type GetRef, type TableColumnsType } from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, APIError, errorMessage, type DeviceDestination, type DeviceDestinationPage } from '../api'

interface DeviceDestinationsProps {
  deviceId: string
  deviceName: string
  timeZone?: string
  onExpired: () => void
}

type LoadMode = 'refresh' | 'more'

export default function DeviceDestinations({ deviceId, deviceName, timeZone, onExpired }: DeviceDestinationsProps) {
  const [items, setItems] = useState<DeviceDestination[]>([])
  const [nextCursor, setNextCursor] = useState<string>()
  const [busy, setBusy] = useState<LoadMode | null>('refresh')
  const [error, setError] = useState<{ mode: LoadMode; message: string }>()
  const request = useRef<AbortController>()
  const table = useRef<GetRef<typeof Table>>(null)
  const formatter = useMemo(() => new Intl.DateTimeFormat('zh-CN', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }), [timeZone])

  const load = useCallback(async (before?: string) => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    const mode: LoadMode = before ? 'more' : 'refresh'
    setBusy(mode)
    setError(undefined)
    const params = new URLSearchParams({ limit: '100' })
    if (before) params.set('before', before)

    try {
      const result = await api<DeviceDestinationPage>(
        '/devices/' + encodeURIComponent(deviceId) + '/destinations?' + params,
        'GET', undefined, controller.signal,
      )
      if (controller.signal.aborted) return
      setItems((current) => before ? [...current, ...result.items] : result.items)
      setNextCursor(result.nextCursor)
      if (!before) table.current?.scrollTo({ top: 0 })
    } catch (failure) {
      if (controller.signal.aborted) return
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else setError({ mode, message: errorMessage(failure) })
    } finally {
      if (!controller.signal.aborted) setBusy(null)
    }
  }, [deviceId, onExpired])

  useEffect(() => {
    void load()
    return () => request.current?.abort()
  }, [load])

  const columns: TableColumnsType<DeviceDestination> = [
    {
      title: '目标地址',
      key: 'address',
      render: (_, item) => <bdi style={{ overflowWrap: 'anywhere' }}>{item.address}</bdi>,
    },
    {
      title: '访问时间',
      key: 'lastSeenAt',
      width: 180,
      render: (_, item) => (
        <time dateTime={item.lastSeenAt} style={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
          {formatter.format(new Date(item.lastSeenAt))}
        </time>
      ),
    },
  ]

  return (
    <Flex vertical gap={12}>
      <Typography.Text type="secondary">「{deviceName}」经过节点的连接目标</Typography.Text>
      <div role="region" aria-label="访问目标列表" aria-busy={busy !== null} tabIndex={0}>
        <Table<DeviceDestination>
          ref={table}
          columns={columns}
          dataSource={items}
          rowKey="id"
          size="small"
          scroll={{ x: 420, y: 356 }}
          pagination={false}
          loading={busy !== null && items.length === 0}
          locale={{
            emptyText: error ? null : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={busy ? '正在读取访问记录…' : '暂无访问记录'} />,
          }}
        />
      </div>
      {error && <Alert type="error" showIcon message={error.message} />}
      <Flex wrap="wrap" justify="end" gap={8}>
        <Button
          icon={<ReloadOutlined />}
          loading={busy === 'refresh'}
          disabled={busy === 'more'}
          onClick={() => void load()}
        >{error?.mode === 'refresh' ? '重试刷新' : '刷新'}</Button>
        {nextCursor && (
          <Button
            type="primary"
            icon={<ArrowDownOutlined />}
            loading={busy === 'more'}
            disabled={busy === 'refresh'}
            onClick={() => void load(nextCursor)}
          >{error?.mode === 'more' ? '重试加载' : '加载更早记录'}</Button>
        )}
      </Flex>
    </Flex>
  )
}
