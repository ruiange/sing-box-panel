import {
  ApiOutlined, DashboardOutlined, LineChartOutlined, LogoutOutlined, MobileOutlined, PieChartOutlined,
  PlusOutlined, ReloadOutlined, SettingOutlined,
} from '@ant-design/icons'
import { PageContainer, ProCard, ProLayout } from '@ant-design/pro-components'
import { Alert, App as AntApp, Badge, Button, Input, Select, Spin, Tooltip } from 'antd'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { errorMessage, type Device, type LiveRatePoint } from './api'
import DeviceDialogs, { type DeviceDialog } from './components/DeviceDialogs'
import DeviceTable from './components/DeviceTable'
import TrafficPanels from './components/TrafficPanels'
import PanelLogin, { PanelBrand } from './pages/PanelLogin'
import PanelOverview from './pages/PanelOverview'
import SiteSettings from './pages/SiteSettings'
import { navigatePanel, pageNames, readPanelLocation, usePanelLocation } from './panelRoutes'
import { defaultRange, liveLabels, type HistoryRange } from './trafficOptions'
import { useLiveTraffic } from './useLiveTraffic'
import { usePanelData } from './usePanelData'
import { usePanelSession } from './usePanelSession'
import { useServerStatus } from './useServerStatus'

const TrafficAnalysis = lazy(() => import('./pages/TrafficAnalysis'))

const pages = {
  overview: { path: '/overview', name: pageNames.overview, icon: <DashboardOutlined /> },
  devices: { path: '/devices', name: pageNames.devices, icon: <MobileOutlined /> },
  traffic: { path: '/traffic', name: pageNames.traffic, icon: <LineChartOutlined /> },
  analysis: { path: '/analysis', name: pageNames.analysis, icon: <PieChartOutlined /> },
  access: { path: '/access', name: pageNames.access, icon: <SettingOutlined /> },
}
const layoutRoute = {
  path: '/',
  routes: [...Object.values(pages), { path: '/docs', name: '接口文档', icon: <ApiOutlined /> }],
}
const statusOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'enabled', label: '已启用' },
  { value: 'disabled', label: '已停用' },
]
const collectionStates = {
  loading: { status: 'processing', text: '正在读取采集状态…' },
  waiting: { status: 'default', text: '等待采集' },
  ok: { status: 'success', text: '采集正常' },
  error: { status: 'error', text: '采集异常' },
  unavailable: { status: 'error', text: '采集状态不可用' },
} as const
const emptyLiveHistory: LiveRatePoint[] = []

type DeviceFilter = 'all' | 'enabled' | 'disabled'

function showDeviceTraffic(deviceId: string) {
  navigatePanel('desktop', 'traffic', deviceId)
}

export default function App() {
  const { message } = AntApp.useApp()
  const { view, scope } = usePanelLocation()
  const {
    stage, login, loginError, logout, loggingOut, onExpired,
    displayTitle, siteTitle, titleLoading, titleError, loadSiteTitle, applySiteTitle,
  } = usePanelSession()
  const [range, setRange] = useState<HistoryRange>(defaultRange)
  const [analysisRange, setAnalysisRange] = useState<HistoryRange>(defaultRange)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<DeviceFilter>('all')
  const [dialog, setDialog] = useState<DeviceDialog>()
  const focusLiveHeading = useRef(false)

  const onMissingDevice = useCallback((deviceId: string) => {
    if (readPanelLocation().scope !== deviceId) return
    navigatePanel('desktop', 'traffic', '', true)
    void message.info('所选设备已不存在，已切换到全部设备')
  }, [message])

  const ready = stage === 'ready'
  const requestedRange = view === 'analysis' ? analysisRange : view === 'traffic' ? range : defaultRange
  const { devices, loading, deviceError, traffic, scopedTraffic, refresh, updateDevice } = usePanelData(
    ready, scope, requestedRange, onExpired, onMissingDevice,
  )
  const serverStatus = useServerStatus(ready && view === 'overview', onExpired)
  const live = useLiveTraffic(ready, scope, onExpired)
  const selectedDevice = devices.find((device) => device.id === scope)
  const visibleTraffic = scope ? (scopedTraffic?.deviceId === scope ? scopedTraffic : undefined) : traffic
  const chartData = visibleTraffic?.range === requestedRange ? visibleTraffic.traffic : undefined
  const analysisTrafficError = traffic?.errorRange === analysisRange ? traffic.error : undefined
  const currentHistory = live.snapshot && live.snapshot.deviceId === scope ? live.snapshot.history : emptyLiveHistory
  const visibleRows = useMemo(() => {
    if (view !== 'devices') return []
    const search = query.trim().toLocaleLowerCase()
    const usage = new Map(traffic?.traffic?.devices.map((entry) => [entry.id, entry]))
    return devices
      .filter((device) => device.name.toLocaleLowerCase().includes(search) &&
        (filter === 'all' || device.enabled === (filter === 'enabled')))
      .map((device) => ({ ...device, usage: usage.get(device.id) }))
  }, [view, devices, traffic?.traffic, query, filter])

  useEffect(() => {
    if (!ready) setDialog(undefined)
  }, [ready])

  useEffect(() => {
    if (!scope || !focusLiveHeading.current) return
    focusLiveHeading.current = false
    document.getElementById('live-heading')?.focus()
  }, [scope])

  function openDeviceTraffic(device: Device) {
    focusLiveHeading.current = true
    showDeviceTraffic(device.id)
  }

  function openCreate() {
    setDialog({ type: 'create' })
  }

  function deviceSaved(device: Device, notice: string) {
    updateDevice(device)
    void message.success(notice)
    void refresh()
  }

  function refreshData() {
    void serverStatus.refresh()
    return refresh()
  }

  if (!ready) {
    return <PanelLogin siteTitle={displayTitle} checking={stage === 'checking'} error={loginError} onLogin={login} />
  }

  const collectionStatus = traffic?.error
    ? 'unavailable'
    : traffic?.traffic?.collectorStatus ?? (loading ? 'loading' : 'waiting')
  const collection = collectionStates[collectionStatus]
  const collectionBadge = <Badge {...collection} />
  const createButton = <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新增设备</Button>
  const renderHomeLink = () => (
    <a className="panel-home-link" href="#overview" aria-label={`${displayTitle} · 面板概览`}>
      <PanelBrand />
      <span className="panel-home-title" title={displayTitle}>{displayTitle}</span>
    </a>
  )
  const scopeOptions = [
    { value: '', label: '全部设备' },
    ...(scope && !selectedDevice ? [{ value: scope, label: '所选设备' }] : []),
    ...devices.map((device) => ({ value: device.id, label: device.name + (device.enabled ? '' : '（已停用）') })),
  ]
  const pageAction = view === 'traffic' ? (
    <label className="panel-scope">
      <span>统计设备</span>
      <Select aria-label="统计设备" value={scope} options={scopeOptions} onChange={showDeviceTraffic} />
    </label>
  ) : view === 'devices' ? createButton : undefined

  return (
    <ProLayout
      className="panel-layout"
      title={displayTitle}
      logo={<PanelBrand />}
      layout="mix"
      contentWidth="Fluid"
      fixedHeader
      fixSiderbar
      siderWidth={216}
      contentStyle={{ padding: 0 }}
      route={layoutRoute}
      location={{ pathname: pages[view].path }}
      pageTitleRender={false}
      breadcrumbRender={false}
      headerTitleRender={renderHomeLink}
      menuHeaderRender={(_logo, _title, props) => props && !props.isMobile ? null : renderHomeLink()}
      menu={{ locale: false }}
      token={{
        header: { colorBgHeader: '#ffffff' },
        sider: {
          colorMenuBackground: '#ffffff',
          colorBgMenuItemSelected: '#eaf2ff',
          colorTextMenuSelected: '#145abb',
        },
        pageContainer: { paddingBlockPageContainerContent: 12, paddingInlinePageContainerContent: 24 },
      }}
      menuItemRender={(item, dom) => item.path && (
        <a href={item.path === '/docs' ? '/docs' : '#' + item.path.slice(1)}
          aria-current={item.path === pages[view].path ? 'page' : undefined}>{dom}</a>
      )}
      actionsRender={() => [
        view !== 'access' && view !== 'overview' && <span key="collection" className="panel-collection" title={collection.text} aria-label={collection.text}>
          {collectionBadge}
        </span>,
        view !== 'access' && <Tooltip key="refresh" title="刷新数据">
          <Button type="text" aria-label="刷新数据" icon={<ReloadOutlined />} loading={loading} onClick={refreshData} />
        </Tooltip>,
        <Tooltip key="logout" title="退出登录">
          <Button type="text" aria-label="退出登录" icon={<LogoutOutlined />} loading={loggingOut}
            onClick={() => void logout().catch((error) => message.error(errorMessage(error)))} />
        </Tooltip>,
      ]}
    >
      <PageContainer
        title={<h1 className="panel-page-title">{pages[view].name}</h1>}
        extra={pageAction}
      >
        {view === 'access' ? (
          <SiteSettings
            siteTitle={siteTitle} titleLoading={titleLoading} titleError={titleError}
            onLoadTitle={loadSiteTitle} onTitleSaved={applySiteTitle} onExpired={onExpired}
          />
        ) : view === 'analysis' ? (
          <Suspense fallback={(
            <div style={{ display: 'grid', placeItems: 'center', minHeight: 180 }} role="status">
              <Spin tip="正在加载流量分析…"><div style={{ height: 64 }} /></Spin>
            </div>
          )}>
            <TrafficAnalysis
              range={analysisRange}
              onRangeChange={setAnalysisRange}
              chartData={chartData}
              traffic={traffic?.traffic}
              devices={devices}
              presence={live.snapshot?.presence}
              liveState={live.state}
              loading={loading || (!chartData && !analysisTrafficError)}
              trafficError={analysisTrafficError}
              deviceError={deviceError}
            />
          </Suspense>
        ) : view === 'overview' ? (
          <PanelOverview
            serverStatus={serverStatus.snapshot}
            collectionBadge={collectionBadge} traffic={traffic?.traffic} liveState={live.state}
            devices={devices} presence={live.snapshot?.presence} loading={loading} deviceError={deviceError}
          />
        ) : view === 'traffic' ? (
          <div className="panel-stack">
            {deviceError && <Alert type="error" showIcon message={deviceError} />}
            {visibleTraffic?.error && <Alert type="error" showIcon message={visibleTraffic.error} />}
            <TrafficPanels
              traffic={visibleTraffic?.traffic}
              chartData={chartData}
              range={range}
              onRangeChange={setRange}
              loading={loading}
              device={selectedDevice}
              deviceId={scope}
              liveState={live.state}
              rate={selectedDevice?.enabled ? live.snapshot?.devices[scope] : undefined}
              history={currentHistory}
              liveEnd={live.snapshot?.at ? Date.parse(live.snapshot.at) : Date.now()}
            />
          </div>
        ) : (
          <div className="panel-stack">
            {deviceError && <Alert type="error" showIcon message={deviceError} />}
            <ProCard bordered title={<h2 className="panel-section-title">设备列表</h2>}>
              <div className="panel-device-meta">
                {deviceError ? '设备列表暂时不可用' : `已启用 ${devices.filter((device) => device.enabled).length} 台 · 共 ${devices.length} 台`}
                {!deviceError && devices.length > 0 && <> · {liveLabels[live.state]}</>}
              </div>
              <div className="panel-filters">
                <Input.Search aria-label="搜索设备名称" placeholder="搜索设备名称" allowClear
                  value={query} onChange={(event) => setQuery(event.target.value)} />
                <Select<DeviceFilter> aria-label="按设备启用状态筛选" value={filter} options={statusOptions} onChange={setFilter} />
              </div>
              <DeviceTable
                devices={visibleRows} rates={live.snapshot?.devices} presence={live.snapshot?.presence}
                lastActiveAt={live.lastActiveAt} timeZone={traffic?.traffic?.timeZone}
                loading={loading}
                emptyLabel={deviceError ? '设备列表加载失败' : devices.length ? '没有符合条件的设备' : '还没有设备'}
                emptyAction={!loading && !devices.length && !deviceError ? createButton : undefined}
                onSubscription={(device) => setDialog({ type: 'subscription', device })}
                onRename={(device) => setDialog({ type: 'rename', device })}
                onToggle={(device) => setDialog({ type: 'toggle', device })}
                onTraffic={openDeviceTraffic}
                onDestinations={(device) => setDialog({ type: 'destinations', device })}
              />
            </ProCard>
          </div>
        )}
      </PageContainer>
      {dialog && <DeviceDialogs
        key={dialog.type === 'create' ? 'create' : `${dialog.type}-${dialog.device.id}`}
        dialog={dialog}
        devices={devices}
        timeZone={traffic?.traffic?.timeZone}
        onClose={() => setDialog(undefined)}
        onExpired={onExpired}
        onSaved={deviceSaved}
      />}
    </ProLayout>
  )
}
