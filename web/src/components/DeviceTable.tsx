import { ArrowDownOutlined, ArrowUpOutlined, EllipsisOutlined, MobileOutlined } from '@ant-design/icons'
import { ProTable, type ProColumns } from '@ant-design/pro-components'
import { Avatar, Badge, Button, Dropdown, Empty, Flex, theme, Tooltip, Typography } from 'antd'
import type { ReactNode } from 'react'
import type { Device, DevicePresence, LiveRate, PeriodUsage } from '../api'
import { formatBytes, formatRate, formatTime, totalUsage } from '../format'

type DeviceRow = Device & { usage?: PeriodUsage }
type DeviceAction = (device: Device) => void

interface DeviceTableProps {
  devices: DeviceRow[]
  rates?: Record<string, LiveRate>
  presence?: Record<string, DevicePresence>
  lastActiveAt: ReadonlyMap<string, string>
  timeZone?: string
  selectedDeviceId: string
  loading: boolean
  emptyLabel: string
  emptyAction?: ReactNode
  onSubscription: DeviceAction
  onRename: DeviceAction
  onToggle: DeviceAction
  onTraffic: DeviceAction
  onDestinations: DeviceAction
}

const statuses = {
  active: { label: '活跃', detail: '最近 60 秒有代理流量', tone: 'success' },
  idle: { label: '暂无活动', detail: '最近 60 秒未观察到代理流量', tone: 'default' },
  disabled: { label: '已停用', detail: '设备已在后台停用', tone: 'default' },
  unknown: { label: '状态未知', detail: '当前采样不可用', tone: 'warning' },
} as const

function Directions({ upload, download, rate = false }: { upload: string; download: string; rate?: boolean }) {
  const uploadLabel = rate ? '上传速度' : '上传'
  const downloadLabel = rate ? '下载速度' : '下载'

  return (
    <Flex vertical style={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        <ArrowUpOutlined aria-label={uploadLabel} title={uploadLabel} /> {upload}
      </Typography.Text>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        <ArrowDownOutlined aria-label={downloadLabel} title={downloadLabel} /> {download}
      </Typography.Text>
    </Flex>
  )
}

export default function DeviceTable({
  devices, rates, presence, lastActiveAt, timeZone, selectedDeviceId, loading, emptyLabel, emptyAction,
  onSubscription, onRename, onToggle, onTraffic, onDestinations,
}: DeviceTableProps) {
  const { token } = theme.useToken()

  const columns: ProColumns<DeviceRow>[] = [
    {
      title: '设备',
      key: 'device',
      width: 200,
      render: (_, device) => (
        <Flex align="center" gap={10}>
          <Avatar
            shape="square"
            icon={<MobileOutlined />}
            style={{ flexShrink: 0, color: token.colorPrimary, background: token.colorPrimaryBg }}
          />
          <Flex vertical style={{ minWidth: 0 }}>
            <Typography.Text strong ellipsis={{ tooltip: device.name }}>{device.name}</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
              <time dateTime={device.createdAt}>{formatTime(device.createdAt, timeZone)}</time> 添加
            </Typography.Text>
          </Flex>
        </Flex>
      ),
    },
    {
      title: '状态',
      key: 'status',
      width: 178,
      render: (_, device) => {
        const state = device.enabled ? presence?.[device.id]?.state ?? 'unknown' : 'disabled'
        const status = statuses[state] ?? statuses.unknown
        const activeAt = lastActiveAt.get(device.id)

        return (
          <Flex vertical>
            <Tooltip title={status.detail}><span><Badge status={status.tone} text={status.label} /></span></Tooltip>
            <Typography.Text type="secondary" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
              {activeAt ? <>最后活跃 <time dateTime={activeAt}>{formatTime(activeAt, timeZone)}</time></> : '暂无活跃记录'}
            </Typography.Text>
          </Flex>
        )
      },
    },
    {
      title: '实时网速',
      key: 'rates',
      width: 122,
      render: (_, device) => {
        const rate = rates?.[device.id]
        return device.enabled && rate
          ? <Directions upload={formatRate(rate.upload)} download={formatRate(rate.download)} rate />
          : '—'
      },
    },
    {
      title: '今日流量',
      key: 'today',
      width: 104,
      render: (_, { usage }) => usage ? formatBytes(totalUsage(usage.today)) : '—',
    },
    {
      title: '本月流量',
      key: 'month',
      width: 112,
      render: (_, { usage }) => usage ? (
        <Flex vertical>
          <Typography.Text strong>{formatBytes(totalUsage(usage.month))}</Typography.Text>
          <Directions upload={formatBytes(usage.month.upload)} download={formatBytes(usage.month.download)} />
        </Flex>
      ) : '—',
    },
    {
      title: '累计流量',
      key: 'all',
      width: 104,
      render: (_, { usage }) => usage ? formatBytes(totalUsage(usage.all)) : '—',
    },
    {
      title: '操作',
      key: 'actions',
      width: 226,
      render: (_, device) => (
        <Flex align="center" gap={8}>
          <Button
            size="small" type="link" style={{ paddingInline: 0 }}
            aria-pressed={selectedDeviceId === device.id}
            aria-label={`查看 ${device.name} 的流量`}
            onClick={() => onTraffic(device)}
          >流量</Button>
          {device.enabled && (
            <Button size="small" type="link" style={{ paddingInline: 0 }} onClick={() => onSubscription(device)}>订阅</Button>
          )}
          <Button
            size="small" type="link" style={{ paddingInline: 0 }}
            aria-label={`查看 ${device.name} 的访问记录`}
            onClick={() => onDestinations(device)}
          >访问记录</Button>
          <Dropdown
            trigger={['click']}
            menu={{
              items: [
                { key: 'rename', label: '重命名设备', onClick: () => onRename(device) },
                {
                  key: 'toggle', label: device.enabled ? '停用设备' : '启用设备',
                  danger: device.enabled, onClick: () => onToggle(device),
                },
              ],
            }}
          >
            <Button size="small" type="text" icon={<EllipsisOutlined />} aria-label={`${device.name} 的更多操作`} />
          </Dropdown>
        </Flex>
      ),
    },
  ]

  return (
    <ProTable<DeviceRow>
      columns={columns}
      dataSource={devices}
      rowKey="id"
      search={false}
      options={false}
      toolBarRender={false}
      pagination={false}
      cardBordered={false}
      cardProps={false}
      size="middle"
      scroll={{ x: 1046 }}
      loading={loading && devices.length === 0}
      rowClassName={(device) => device.id === selectedDeviceId ? 'device-table-selected' : ''}
      onRow={(device) => device.id === selectedDeviceId ? { style: { background: token.colorPrimaryBg } } : {}}
      locale={{
        emptyText: (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={loading ? '正在读取设备…' : emptyLabel}
          >{loading ? null : emptyAction}</Empty>
        ),
      }}
    />
  )
}
