import {
  CloudServerOutlined, DashboardOutlined, LineChartOutlined, LogoutOutlined,
  MobileOutlined, PieChartOutlined, ReloadOutlined, SettingOutlined,
} from '@ant-design/icons'
import { Button, Card, Input, List, NavBar, NoticeBar, SpinLoading, TabBar, Tag, Toast } from 'antd-mobile'
import { lazy, Suspense, useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { errorMessage, type Device, type Traffic } from '../api'
import { formatTime } from '../format'
import { navigatePanel, pageNames, readPanelLocation, usePanelLocation, type View } from '../panelRoutes'
import { countDeviceStates } from '../trafficAnalysis'
import { defaultRange, liveLabels, type HistoryRange } from '../trafficOptions'
import { useLiveTraffic } from '../useLiveTraffic'
import { usePanelData } from '../usePanelData'
import { usePanelSession } from '../usePanelSession'
import './mobile-panel.css'

const MobileDevices = lazy(() => import('./MobileDevices'))
const MobileTraffic = lazy(() => import('./MobileTraffic'))
const MobileAnalysis = lazy(() => import('./MobileAnalysis'))
const MobileSettings = lazy(() => import('./MobileSettings'))

const tabs = [
  { view: 'overview', title: '概览', icon: <DashboardOutlined /> },
  { view: 'devices', title: '设备', icon: <MobileOutlined /> },
  { view: 'traffic', title: '流量', icon: <LineChartOutlined /> },
  { view: 'analysis', title: '分析', icon: <PieChartOutlined /> },
  { view: 'access', title: '设置', icon: <SettingOutlined /> },
] as const

function MobileLoading({ children }: { children: string }) {
  return <div className="mobile-loading" role="status"><SpinLoading />{children}</div>
}

function MobileLogin({
  siteTitle, checking, error, onLogin,
}: {
  siteTitle: string
  checking: boolean
  error: string
  onLogin: (password: string) => Promise<boolean>
}) {
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const pending = useRef(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!password || pending.current) return
    pending.current = true
    setSubmitting(true)
    try {
      await onLogin(password)
    } finally {
      pending.current = false
      setSubmitting(false)
    }
  }

  return (
    <main className="mobile-login" aria-label="管理员登录">
      <div className="mobile-login-brand">
        <span className="mobile-brand" aria-hidden="true"><CloudServerOutlined /></span>
        <h1>{siteTitle}</h1>
      </div>
      <Card title={<h2>管理员登录</h2>}>
        {checking ? <MobileLoading>正在确认登录状态…</MobileLoading> : (
          <form className="mobile-login-form" method="post" onSubmit={(event) => void submit(event)}>
            {error && <div role="alert"><NoticeBar color="error" wrap content={error} /></div>}
            <label htmlFor="mobile-password">管理员密码</label>
            <Input
              className="mobile-login-input" id="mobile-password" name="password" placeholder="输入管理员密码"
              type="password" autoComplete="current-password" enterKeyHint="done"
              value={password} onChange={setPassword} disabled={submitting}
              style={{ '--font-size': '16px' }}
            />
            <Button type="submit" color="primary" size="large" block loading={submitting} disabled={!password}>登录</Button>
          </form>
        )}
      </Card>
    </main>
  )
}

function MobileOverview({
  traffic, trafficError, devices, deviceError, live, loading, onNavigate,
}: {
  traffic?: Traffic
  trafficError?: string
  devices: Device[]
  deviceError: string
  live: ReturnType<typeof useLiveTraffic>
  loading: boolean
  onNavigate: (view: View) => void
}) {
  const status = trafficError ? 'unavailable' : traffic?.collectorStatus ?? 'waiting'
  const collectionLabels = {
    ok: '采集正常', waiting: '等待采集', error: '采集异常', unavailable: '采集状态不可用',
  }
  const counts = countDeviceStates(devices, live.state === 'live' ? live.snapshot?.presence : undefined)
  const enabled = devices.filter((device) => device.enabled).length
  const active = counts.find(({ status }) => status === 'active')?.count ?? 0
  const unknown = counts.find(({ status }) => status === 'unknown')?.count ?? 0
  const showDeviceCounts = !deviceError && (!loading || devices.length > 0)

  return (
    <div className="mobile-overview">
      <Card className="mobile-overview-status" title={<h2>运行状态</h2>} extra={
        <Tag color={status === 'ok' ? 'success' : status === 'waiting' ? 'default' : 'danger'} fill="outline">
          {loading && !traffic && !trafficError ? '正在读取' : collectionLabels[status]}
        </Tag>
      }>
        <dl className="mobile-overview-details">
          <dt>实时连接</dt>
          <dd>{liveLabels[live.state]}</dd>
          <dt>最近采集</dt>
          <dd>{traffic?.collectedAt
            ? <time dateTime={traffic.collectedAt}>{formatTime(traffic.collectedAt, traffic.timeZone)} · {traffic.timeZone}</time>
            : '—'}</dd>
        </dl>
        {trafficError && <div role="alert"><NoticeBar content={trafficError} color="error" wrap /></div>}
      </Card>

      <Card className="mobile-overview-devices" title={<h2>设备概况</h2>}>
        {deviceError ? <div role="alert"><NoticeBar content={deviceError} color="error" wrap /></div> :
          <dl className="mobile-overview-counts">
            <div><dt>全部设备</dt><dd>{showDeviceCounts ? devices.length : '—'}</dd></div>
            <div><dt>已启用</dt><dd>{showDeviceCounts ? enabled : '—'}</dd></div>
            <div><dt>近 60 秒活跃</dt><dd>{showDeviceCounts && !(enabled > 0 && unknown === enabled) ? active : '—'}</dd></div>
          </dl>}
      </Card>

      <List className="mobile-overview-links" mode="card">
        <List.Item prefix={<MobileOutlined aria-hidden="true" />} onClick={() => onNavigate('devices')}>管理设备</List.Item>
        <List.Item prefix={<LineChartOutlined aria-hidden="true" />} onClick={() => onNavigate('traffic')}>查看流量</List.Item>
      </List>
    </div>
  )
}

export default function MobilePanel() {
  const { view, scope } = usePanelLocation()
  const {
    stage, login, loginError, logout, loggingOut, onExpired,
    displayTitle, siteTitle, titleLoading, titleError, loadSiteTitle, applySiteTitle,
  } = usePanelSession()
  const [range, setRange] = useState<HistoryRange>(defaultRange)
  const [analysisRange, setAnalysisRange] = useState<HistoryRange>(defaultRange)
  const ready = stage === 'ready'
  const requestedRange = view === 'analysis' ? analysisRange : view === 'traffic' ? range : defaultRange

  const onMissingDevice = useCallback((deviceId: string) => {
    if (readPanelLocation().scope !== deviceId) return
    navigatePanel('mobile', 'traffic', '', true)
    Toast.show({ content: '所选设备已不存在，已切换到全部设备' })
  }, [])

  const { devices, traffic, scopedTraffic, loading, deviceError, refresh, updateDevice } = usePanelData(
    ready, scope, requestedRange, onExpired, onMissingDevice,
  )
  const live = useLiveTraffic(ready, scope, onExpired)
  const visibleTraffic = scope ? (scopedTraffic?.deviceId === scope ? scopedTraffic : undefined) : traffic
  const chartData = visibleTraffic?.range === requestedRange ? visibleTraffic.traffic : undefined

  useEffect(() => { window.scrollTo(0, 0) }, [view, ready])

  function onNavigate(nextView: View) {
    if (nextView !== view) navigatePanel('mobile', nextView)
  }

  function onTabChange(key: string) {
    const tab = tabs.find(({ view }) => view === key)
    if (tab) onNavigate(tab.view)
  }

  function onTraffic(deviceId: string) {
    navigatePanel('mobile', 'traffic', deviceId)
  }

  function onDeviceSaved(device: Device) {
    updateDevice(device)
    void refresh()
  }

  async function handleLogout() {
    try {
      await logout()
    } catch (error) {
      Toast.show({ content: errorMessage(error) })
    }
  }

  if (!ready) {
    return <MobileLogin siteTitle={displayTitle} checking={stage === 'checking'} error={loginError} onLogin={login} />
  }

  return (
    <div className="mobile-panel">
      <header className="mobile-panel-header">
        <NavBar
          back={null}
          backIcon={false}
          left={<span className="mobile-brand" aria-hidden="true"><CloudServerOutlined /></span>}
          right={view === 'access' ? (
            <Button className="mobile-nav-action" fill="none" aria-label="退出登录" loading={loggingOut} onClick={handleLogout}>
              <LogoutOutlined aria-hidden="true" />
            </Button>
          ) : (
            <Button className="mobile-nav-action" fill="none" aria-label="刷新数据" loading={loading} onClick={refresh}>
              <ReloadOutlined aria-hidden="true" />
            </Button>
          )}
        >
          <span className="mobile-site-title" title={displayTitle}>{displayTitle}</span>
        </NavBar>
      </header>

      <main className="mobile-panel-main" aria-labelledby="mobile-view-title">
        <h1 id="mobile-view-title">{pageNames[view]}</h1>
        <Suspense fallback={<MobileLoading>正在打开页面…</MobileLoading>}>
          {view === 'overview' && (
            <MobileOverview devices={devices} traffic={traffic?.traffic} trafficError={traffic?.error}
              deviceError={deviceError} live={live} loading={loading} onNavigate={onNavigate} />
          )}
          {view === 'devices' && (
            <MobileDevices devices={devices} traffic={traffic?.traffic} live={live} loading={loading}
              deviceError={deviceError} onTraffic={onTraffic} onExpired={onExpired} onSaved={onDeviceSaved} />
          )}
          {view === 'traffic' && (
            <MobileTraffic devices={devices} scope={scope} onTraffic={onTraffic} traffic={visibleTraffic?.traffic}
              chartData={chartData} range={range} onRangeChange={setRange} loading={loading}
              deviceError={deviceError} trafficError={visibleTraffic?.error} live={live} />
          )}
          {view === 'analysis' && (
            <MobileAnalysis devices={devices} onTraffic={onTraffic} traffic={traffic?.traffic} chartData={chartData}
              range={analysisRange} onRangeChange={setAnalysisRange} loading={loading} deviceError={deviceError}
              trafficError={traffic?.errorRange === analysisRange ? traffic.error : undefined} live={live} />
          )}
          {view === 'access' && (
            <>
              <MobileSettings siteTitle={siteTitle} titleLoading={titleLoading} titleError={titleError}
                onLoadTitle={loadSiteTitle} onTitleSaved={applySiteTitle} onExpired={onExpired} />
              <a className="mobile-docs-link" href="/m/docs">接口文档</a>
            </>
          )}
        </Suspense>
      </main>

      <nav className="mobile-panel-footer" aria-label="主导航">
        <TabBar activeKey={view} onChange={onTabChange} safeArea>
          {tabs.map(({ view: tabView, title, icon }) => (
            <TabBar.Item key={tabView} icon={icon} title={(
              <a href={`/m/${tabView}`} aria-label={pageNames[tabView]}
                aria-current={view === tabView ? 'page' : undefined}
                onClick={(event) => {
                  event.stopPropagation()
                  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                  event.preventDefault()
                  onNavigate(tabView)
                }}>{title}</a>
            )} />
          ))}
        </TabBar>
      </nav>
    </div>
  )
}
