<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Activity, ArrowDown, ArrowRight, ArrowUp, Cable, CircleAlert, Clipboard, FileCode, Gauge,
  Link2, LockKeyhole, LogOut, Plus, RadioTower, RefreshCw, Search, Smartphone, X,
} from 'lucide-vue-next'
import { api, APIError, errorMessage, type Device, type Traffic } from './api'
import { beginDeviceCreation, confirmDeviceCreations, hasPendingDeviceCreation, submitDeviceCreation, type DeviceCreateRequest } from './deviceCreation'
import { formatBytes, formatRate, formatTime, totalUsage } from './format'
import { useLiveTraffic } from './useLiveTraffic'
import DeviceTable from './components/DeviceTable.vue'
import LiveRateChart from './components/LiveRateChart.vue'
import SubscriptionQR from './components/SubscriptionQR.vue'
import TrafficChart from './components/TrafficChart.vue'

type View = 'overview' | 'devices' | 'traffic'
type Modal =
  | { type: 'create' }
  | { type: 'rename' | 'toggle' | 'subscription'; device: Device }

const tabs = [
  { id: 'overview', text: '运行概览', icon: Gauge },
  { id: 'devices', text: '设备管理', icon: Smartphone },
  { id: 'traffic', text: '流量统计', icon: Activity },
] as const
const periods = [
  { key: 'today', title: '今日用量' },
  { key: 'month', title: '本月用量' },
  { key: 'all', title: '累计用量' },
] as const
const formats = [
  { id: 'shadowrocket', text: '小火箭' },
  { id: 'clash', text: 'Clash' },
  { id: 'sing-box', text: 'sing-box' },
  { id: 'v2ray', text: 'v2rayN' },
] as const

const ranges = [
  { query: 'hours=6', label: '近 6 小时' },
  { query: 'hours=12', label: '近 12 小时' },
  { query: 'hours=24', label: '近 24 小时' },
  { query: 'hours=48', label: '近 48 小时' },
  { query: 'hours=72', label: '近 72 小时' },
  { query: 'days=7', label: '近 7 天' },
  { query: 'days=30', label: '近 30 天' },
] as const
type HistoryRange = (typeof ranges)[number]['query']

const liveLabels = {
  connecting: '正在连接实时速率…',
  waiting: '等待速率采样',
  live: '实时速率更新中',
  reconnecting: '正在重新连接实时速率…',
}

const stage = ref<'checking' | 'login' | 'ready'>('checking')
const view = ref<View>('overview')
const password = ref('')
const loginBusy = ref(false)
const loginError = ref('')
const loading = ref(false)
const deviceError = ref('')
const trafficError = ref('')
const scopedError = ref('')
const devices = ref<Device[]>([])
const traffic = ref<Traffic>()
const scopedTraffic = ref<{ deviceId: string; range: HistoryRange; traffic: Traffic }>()
const selectedDeviceId = ref('')
const range = ref<HistoryRange>('days=7')
const chartRange = ref<HistoryRange>()
const query = ref('')
const filter = ref<'all' | 'enabled' | 'disabled'>('all')
const toast = ref('')

const dialog = ref<HTMLDialogElement>()
const modal = ref<Modal>()
const modalBusy = ref(false)
const modalError = ref('')
const modalFeedback = ref('')
const deviceName = ref('')
const url = ref('')
const format = ref<(typeof formats)[number]['id']>('clash')
const confirmingRotation = ref(false)

let refreshId = 0
let modalId = 0
let submittedCreate: DeviceCreateRequest | undefined
let toastTimer: number | undefined
let poll: number | undefined

const pageTitle = computed(() => tabs.find((tab) => tab.id === view.value)?.text ?? tabs[0].text)
const activeCount = computed(() => devices.value.filter((device) => device.enabled).length)
const scope = computed(() => view.value === 'traffic' ? selectedDeviceId.value : '')
const selectedDevice = computed(() => devices.value.find((device) => device.id === scope.value))
const { snapshot: liveSnapshot, state: liveState, lastActiveAt } = useLiveTraffic(
  computed(() => stage.value === 'ready'),
  scope,
  () => showLogin('登录已过期，请重新登录。'),
)
const liveRates = computed(() => liveSnapshot.value?.at ? liveSnapshot.value.devices : undefined)
const livePresence = computed(() => liveSnapshot.value?.at ? liveSnapshot.value.presence : undefined)
const selectedRate = computed(() => selectedDevice.value?.enabled ? liveRates.value?.[scope.value] : undefined)
const liveHistory = computed(() => {
  const snapshot = liveSnapshot.value
  return snapshot && snapshot.deviceId === scope.value ? snapshot.history : []
})
const liveEnd = computed(() => {
  const at = liveSnapshot.value?.at
  return at ? Date.parse(at) : Date.now()
})
const deviceScope = computed({ get: () => selectedDeviceId.value, set: showDeviceTraffic })
const visibleTraffic = computed(() => {
  if (!scope.value) return traffic.value
  const snapshot = scopedTraffic.value
  if (snapshot && snapshot.deviceId === scope.value) return snapshot.traffic
  return undefined
})
const hourly = computed(() => range.value.startsWith('hours='))
const chartData = computed(() => {
  const loadedRange = scope.value ? scopedTraffic.value?.range : chartRange.value
  return loadedRange === range.value ? visibleTraffic.value : undefined
})
const visibleTrafficError = computed(() => scope.value ? scopedError.value : trafficError.value)
const rows = computed(() => {
  const usage = new Map((traffic.value?.devices ?? []).map((device) => [device.id, device]))
  return devices.value.map((device) => ({ ...device, usage: usage.get(device.id) }))
})
const filteredRows = computed(() => {
  const search = query.value.trim().toLocaleLowerCase()
  return rows.value.filter((device) =>
    device.name.toLocaleLowerCase().includes(search) &&
    (filter.value === 'all' || device.enabled === (filter.value === 'enabled')))
})
const collection = computed(() => {
  if (trafficError.value || traffic.value?.collectorStatus === 'error') return { tone: 'warn', label: '采集异常' }
  if (traffic.value?.collectorStatus === 'ok') return { tone: 'enabled', label: '采集正常' }
  return { tone: '', label: '等待采集' }
})
const formattedCollection = computed(() => visibleTraffic.value?.collectedAt
  ? formatTime(visibleTraffic.value.collectedAt, visibleTraffic.value.timeZone) : '')
const subscriptionURL = computed(() => url.value ? url.value.replace(/\/$/, '') + '/' + format.value : '')
const formatName = computed(() => formats.find((item) => item.id === format.value)?.text ?? '')
const modalTitle = computed(() => {
  switch (modal.value?.type) {
    case 'create': return '新增设备'
    case 'rename': return '重命名设备'
    case 'toggle': return modal.value.device.enabled ? '停用设备' : '启用设备'
    case 'subscription': return '设备订阅'
    default: return ''
  }
})

function readLocation() {
  const [target, params] = window.location.hash.slice(1).split('?')
  if (target === 'overview' || target === 'devices' || target === 'traffic') view.value = target
  else view.value = 'overview'
  selectedDeviceId.value = target === 'traffic' ? new URLSearchParams(params).get('deviceId') ?? '' : ''
}

function showDeviceTraffic(deviceId: string) {
  window.location.hash = deviceId ? 'traffic?deviceId=' + encodeURIComponent(deviceId) : 'traffic'
  readLocation()
}

async function openDeviceTraffic(device: Device) {
  showDeviceTraffic(device.id)
  await nextTick()
  document.getElementById('live-heading')?.focus()
}

function showToast(message: string) {
  toast.value = message
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => { toast.value = '' }, 3200)
}

function showLogin(message = '') {
  refreshId += 1
  loading.value = false
  devices.value = []
  traffic.value = undefined
  scopedTraffic.value = undefined
  chartRange.value = undefined
  deviceError.value = ''
  trafficError.value = ''
  scopedError.value = ''
  toast.value = ''
  closeModal()
  loginError.value = message
  stage.value = 'login'
}

async function restoreSession() {
  stage.value = 'checking'
  try {
    const result = await api<{ authenticated: boolean }>('/auth/session')
    if (!result.authenticated) {
      showLogin()
      return
    }
    stage.value = 'ready'
    await refresh()
  } catch (error) {
    showLogin(errorMessage(error))
  }
}

async function login() {
  if (loginBusy.value) return
  loginBusy.value = true
  loginError.value = ''
  try {
    const result = await api<{ authenticated: boolean }>('/auth/login', 'POST', { password: password.value })
    if (!result.authenticated) throw new Error('密码验证失败。')
    password.value = ''
    stage.value = 'ready'
    await refresh()
  } catch (error) {
    loginError.value = errorMessage(error)
  } finally {
    loginBusy.value = false
  }
}

async function logout() {
  if (loginBusy.value) return
  loginBusy.value = true
  try {
    await api('/auth/logout', 'POST')
    showLogin()
  } catch (error) {
    if (error instanceof APIError && error.status === 401) showLogin()
    else showToast(errorMessage(error))
  } finally {
    loginBusy.value = false
  }
}

async function refresh() {
  if (stage.value !== 'ready') return
  const id = ++refreshId
  const requestedRange = range.value
  const requestedDeviceId = scope.value
  const trafficPath = '/traffic?' + requestedRange
  loading.value = true
  const [deviceResponse, trafficResponse, scopedResponse] = await Promise.allSettled([
    api<Device[]>('/devices'),
    api<Traffic>(trafficPath),
    requestedDeviceId ? api<Traffic>(trafficPath + '&deviceId=' + encodeURIComponent(requestedDeviceId)) : undefined,
  ])
  if (refreshId !== id) return
  loading.value = false
  const expired = [deviceResponse, trafficResponse, scopedResponse].some((result) =>
    result.status === 'rejected' && result.reason instanceof APIError && result.reason.status === 401)
  if (expired) {
    showLogin('登录已过期，请重新登录。')
    return
  }
  if (deviceResponse.status === 'fulfilled') {
    devices.value = deviceResponse.value ?? []
    confirmDeviceCreations(devices.value)
    deviceError.value = ''
    const request = submittedCreate
    if (modal.value?.type === 'create' && request && request.name === deviceName.value.trim() &&
        devices.value.some((device) => device.id === request.requestId)) {
      closeModal()
      showToast('已创建设备')
    }
    if (requestedDeviceId && !devices.value.some((device) => device.id === requestedDeviceId)) {
      window.history.replaceState(window.history.state, '', '#traffic')
      readLocation()
      showToast('所选设备已不存在，已切换到全部设备')
    }
  } else deviceError.value = errorMessage(deviceResponse.reason)
  if (trafficResponse.status === 'fulfilled') {
    traffic.value = trafficResponse.value
    chartRange.value = requestedRange
    trafficError.value = ''
  } else trafficError.value = errorMessage(trafficResponse.reason)
  if (requestedDeviceId === scope.value) {
    if (scopedResponse.status === 'fulfilled') {
      const data = scopedResponse.value
      if (data) scopedTraffic.value = { deviceId: requestedDeviceId, range: requestedRange, traffic: data }
      scopedError.value = ''
    } else scopedError.value = errorMessage(scopedResponse.reason)
  }
}

watch([scope, range], () => {
  scopedError.value = ''
  void refresh()
})

async function openModal(value: Modal) {
  modalId += 1
  const id = modalId
  modal.value = value
  modalError.value = ''
  modalFeedback.value = ''
  modalBusy.value = false
  submittedCreate = undefined
  deviceName.value = value.type === 'rename' ? value.device.name : ''
  url.value = ''
  format.value = 'shadowrocket'
  confirmingRotation.value = false
  await nextTick()
  if (!dialog.value?.open) dialog.value?.showModal()

  if (value.type === 'subscription') {
    modalBusy.value = true
    try {
      const result = await api<{ url: string }>('/devices/' + encodeURIComponent(value.device.id) + '/subscription')
      if (id === modalId) url.value = result.url
    } catch (error) {
      if (id === modalId) handleModalError(error)
    } finally {
      if (id === modalId) modalBusy.value = false
    }
  }
}

function closeModal() {
  if (dialog.value?.open) dialog.value.close()
  clearModal()
}

function clearModal() {
  if (dialog.value?.open) return
  modalId += 1
  modal.value = undefined
  submittedCreate = undefined
  url.value = ''
}

function handleModalError(error: unknown) {
  if (error instanceof APIError && error.status === 401) showLogin('登录已过期，请重新登录。')
  else modalError.value = errorMessage(error)
}

async function saveDevice() {
  const current = modal.value
  if (!current || current.type === 'subscription' || modalBusy.value) return
  const name = deviceName.value.trim()
  if (current.type !== 'toggle' && (!name || [...name].length > 48)) {
    modalError.value = '请输入 1 至 48 个字符的设备名称。'
    return
  }
  modalBusy.value = true
  modalError.value = ''
  const id = modalId
  try {
    if (current.type === 'create') {
      const request = beginDeviceCreation(name)
      submittedCreate = request
      const device = await submitDeviceCreation(request, () => id === modalId)
      confirmDeviceCreations([device])
      if (id === modalId && !devices.value.some((existing) => existing.id === device.id)) {
        devices.value = [...devices.value, device]
        deviceError.value = ''
      }
    } else {
      await api<Device>('/devices/' + encodeURIComponent(current.device.id), 'PATCH',
        current.type === 'rename' ? { name } : { enabled: !current.device.enabled })
    }
    if (id !== modalId) return
    closeModal()
    showToast(current.type === 'create' ? '已创建设备' : '已更新设备')
    await refresh()
  } catch (error) {
    if (id === modalId) handleModalError(error)
  } finally {
    if (id === modalId) modalBusy.value = false
  }
}

async function rotateSubscription() {
  const current = modal.value
  if (current?.type !== 'subscription' || modalBusy.value) return
  if (!confirmingRotation.value) {
    confirmingRotation.value = true
    return
  }
  const id = modalId
  modalBusy.value = true
  modalError.value = ''
  modalFeedback.value = ''
  try {
    const result = await api<{ url: string }>('/devices/' + encodeURIComponent(current.device.id) + '/rotate-subscription', 'POST')
    if (id === modalId) {
      url.value = result.url
      confirmingRotation.value = false
      modalFeedback.value = '已更新订阅链接'
    }
  } catch (error) {
    if (id === modalId) handleModalError(error)
  } finally {
    if (id === modalId) modalBusy.value = false
  }
}

async function copySubscription(event: Event) {
  if (!subscriptionURL.value) return
  modalError.value = ''
  modalFeedback.value = ''
  try {
    if (!navigator.clipboard) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(subscriptionURL.value)
    modalFeedback.value = '已复制订阅链接'
  } catch {
    const button = event.currentTarget as HTMLElement | null
    const input = dialog.value?.querySelector<HTMLInputElement>('.subscription-url')
    input?.focus()
    input?.select()
    let copied = false
    try {
      copied = !!input && document.execCommand('copy')
    } catch {
      // Preserve the selection for manual copying if the clipboard is unavailable.
    }
    if (copied) {
      button?.focus()
      modalFeedback.value = '已复制订阅链接'
    } else modalError.value = '已选中链接，请手动复制。'
  }
}

function cancelDialog(event: Event) {
  if (modalBusy.value) event.preventDefault()
}

onMounted(() => {
  readLocation()
  window.addEventListener('hashchange', readLocation)
  void restoreSession()
  poll = window.setInterval(() => {
    if (!document.hidden && !loading.value) void refresh()
  }, 30_000)
})
onBeforeUnmount(() => {
  window.removeEventListener('hashchange', readLocation)
  window.clearInterval(poll)
  window.clearTimeout(toastTimer)
})
</script>

<template>
  <div v-if="stage !== 'ready'" class="login-layout">
    <div class="login-visual" aria-hidden="true">
      <div class="login-grid"></div>
      <div class="signal-orbits"><i></i><i></i><i></i><span><RadioTower :size="62" /></span></div>
      <div class="login-brand"><span class="brand-mark"><Cable :size="23" /></span><strong>流量大师</strong></div>
    </div>
    <main class="login-main">
      <form v-if="stage === 'login'" class="login-form" @submit.prevent="login">
        <span class="login-lock"><LockKeyhole :size="21" aria-hidden="true" /></span>
        <h1>登录控制台</h1>
        <p>登录后管理设备、订阅和流量。</p>
        <label for="login-password">管理员密码</label>
        <input id="login-password" v-model="password" class="input" type="password" autocomplete="current-password" required autofocus />
        <p v-if="loginError" class="form-error" role="alert">{{ loginError }}</p>
        <button type="submit" class="button primary login-submit" :disabled="loginBusy">
          {{ loginBusy ? '正在登录…' : '登录' }}<ArrowRight :size="17" aria-hidden="true" />
        </button>
      </form>
      <p v-else class="login-pending" role="status"><RefreshCw :size="17" class="spinning" />正在连接面板…</p>
    </main>
  </div>
  <div v-else class="workspace">
    <aside class="sidebar">
      <a class="brand" href="#overview" aria-label="流量大师面板概览">
        <span class="brand-mark"><Cable :size="22" aria-hidden="true" /></span>
        <span><strong>流量大师</strong><small>设备控制台</small></span>
      </a>
      <nav class="nav" aria-label="面板导航">
        <a v-for="tab in tabs" :key="tab.id" :href="'#' + tab.id" :class="{ active: view === tab.id }" :aria-current="view === tab.id ? 'page' : undefined">
          <component :is="tab.icon" :size="18" aria-hidden="true" />{{ tab.text }}
        </a>
      </nav>
      <div class="sidebar-foot">
        <div class="node-profile">
          <span class="node-icon"><RadioTower :size="17" aria-hidden="true" /></span>
          <span><strong>本机节点</strong><small>代理服务</small></span>
        </div>
        <a href="/docs" class="sidebar-link"><FileCode :size="17" aria-hidden="true" />接口文档</a>
        <button type="button" class="sidebar-link" :disabled="loginBusy" @click="logout"><LogOut :size="17" aria-hidden="true" />退出登录</button>
      </div>
    </aside>

    <main class="main-pane">
      <header class="topbar">
        <span class="topbar-title"><span class="status-dot"></span>{{ pageTitle }}</span>
        <div class="topbar-actions">
          <span class="badge" :class="collection.tone"><span class="status-dot"></span>{{ collection.label }}</span>
          <button class="icon-button refresh" type="button" aria-label="刷新数据" title="刷新数据" :disabled="loading" @click="refresh">
            <RefreshCw :size="16" :class="{ spinning: loading }" aria-hidden="true" />
          </button>
          <a href="/docs" class="icon-button mobile-action" aria-label="接口文档" title="接口文档"><FileCode :size="16" aria-hidden="true" /></a>
          <button class="icon-button mobile-action" type="button" aria-label="退出登录" title="退出登录" :disabled="loginBusy" @click="logout">
            <LogOut :size="16" aria-hidden="true" />
          </button>
        </div>
      </header>
      <div class="content">
        <div class="page-heading">
          <div>
            <h1>{{ pageTitle }}</h1>
            <p v-if="view === 'overview'">查看本机节点的设备和代理流量。</p>
            <p v-else-if="view === 'devices'">为每台设备发放独立订阅，单独查阅用量。</p>
            <p v-else>按设备查看经过本机节点的上传和下载流量。</p>
          </div>
          <button v-if="view !== 'traffic'" class="button primary" type="button" @click="openModal({ type: 'create' })">
            <Plus :size="17" aria-hidden="true" />新增设备
          </button>
          <label v-else class="scope-field">
            <span>统计设备</span>
            <select v-model="deviceScope" class="input scope-select">
              <option value="">全部设备</option>
              <option v-if="scope && !selectedDevice" :value="scope">所选设备</option>
              <option v-for="device in devices" :key="device.id" :value="device.id">
                {{ device.name }}{{ device.enabled ? '' : '（已停用）' }}
              </option>
            </select>
          </label>
        </div>

        <div v-if="deviceError" class="notice" role="alert"><CircleAlert :size="17" /><span>{{ deviceError }}</span></div>
        <div v-if="view !== 'devices' && visibleTrafficError" class="notice" role="alert"><CircleAlert :size="17" /><span>{{ visibleTrafficError }}</span></div>

        <template v-if="view !== 'devices'">
          <section class="summary" :aria-label="selectedDevice ? selectedDevice.name + '的流量汇总' : '全部设备流量汇总'">
            <article v-for="period in periods" :key="period.key" class="metric">
              <p>{{ period.title }}</p>
              <strong class="metric-value mono">{{ visibleTraffic ? formatBytes(totalUsage(visibleTraffic.totals[period.key])) : '—' }}</strong>
              <div v-if="visibleTraffic" class="metric-directions">
                <span><ArrowUp :size="14" aria-label="上传" />{{ formatBytes(visibleTraffic.totals[period.key].upload) }}</span>
                <span><ArrowDown :size="14" aria-label="下载" />{{ formatBytes(visibleTraffic.totals[period.key].download) }}</span>
              </div>
              <div v-else class="metric-directions">{{ loading ? '正在读取流量…' : '等待流量数据' }}</div>
            </article>
          </section>

          <section v-if="scope" class="panel trend" aria-labelledby="live-heading">
            <header class="panel-head">
              <div>
                <h2 id="live-heading" tabindex="-1">最近 60 秒网速</h2>
                <p v-if="selectedDevice?.enabled === false">设备已停用</p>
                <p v-else>{{ liveLabels[liveState] }}</p>
              </div>
              <div class="metric-directions live-values mono">
                <span><ArrowUp :size="14" aria-label="上传速度" />{{ selectedRate ? formatRate(selectedRate.upload) : '—' }}</span>
                <span><ArrowDown :size="14" aria-label="下载速度" />{{ selectedRate ? formatRate(selectedRate.download) : '—' }}</span>
              </div>
            </header>
            <div class="chart-legend live-legend"><span><i></i>下载</span><span><i></i>上传</span></div>
            <LiveRateChart v-if="liveHistory.length" :history="liveHistory" :end="liveEnd" />
            <div v-else class="empty-state live-trend-empty">
              <Activity :size="24" aria-hidden="true" /><p>暂无速率样本</p>
            </div>
          </section>

          <section class="panel trend" aria-labelledby="trend-heading">
            <header class="panel-head">
              <div>
                <h2 id="trend-heading" tabindex="-1">{{ selectedDevice ? selectedDevice.name + ' · 流量趋势' : '流量趋势' }}</h2>
                <p v-if="formattedCollection">最近采集 {{ formattedCollection }} · {{ visibleTraffic?.timeZone }}</p>
                <p v-else>节点每次采集后更新</p>
              </div>
              <select v-model="range" class="input trend-range" aria-label="统计范围">
                <option v-for="option in ranges" :key="option.query" :value="option.query">{{ option.label }}</option>
              </select>
            </header>
            <p v-if="hourly && chartData?.hourlySince" class="history-coverage">
              小时统计自 {{ formatTime(chartData.hourlySince, chartData.timeZone) }} 起
            </p>
            <div class="chart-legend"><span><i></i>下载</span><span><i></i>上传</span></div>
            <TrafficChart
              v-if="hourly ? chartData?.hourly?.length : chartData?.daily?.length"
              :daily="hourly ? undefined : chartData?.daily"
              :hourly="hourly ? chartData?.hourly : undefined"
              :time-zone="chartData?.timeZone"
            />
            <div v-else class="empty-state trend-empty">
              <Activity :size="26" aria-hidden="true" />
              <p>{{ loading ? '正在读取趋势…' : hourly ? '所选时段暂无小时统计' : '暂无流量记录' }}</p>
            </div>
          </section>
        </template>

        <section class="panel devices" aria-labelledby="devices-heading">
          <header class="panel-head">
            <div>
              <h2 id="devices-heading">{{ view === 'traffic' ? '全部设备用量' : '设备列表' }}</h2>
              <p v-if="!deviceError">已启用 {{ activeCount }} 台 · 共 {{ devices.length }} 台<span v-if="devices.length"> · {{ liveLabels[liveState] }}</span></p>
              <p v-else>设备列表暂时不可用</p>
            </div>
            <a v-if="view === 'overview'" href="#devices" class="text-link">管理设备<ArrowRight :size="15" aria-hidden="true" /></a>
          </header>
          <div v-if="view === 'devices'" class="device-filter">
            <label class="search-field">
              <Search :size="16" aria-hidden="true" />
              <input v-model="query" type="search" aria-label="搜索设备名称" placeholder="搜索设备名称" />
            </label>
            <select v-model="filter" class="input filter-select" aria-label="按设备启用状态筛选">
              <option value="all">全部设备</option>
              <option value="enabled">已启用</option>
              <option value="disabled">已停用</option>
            </select>
          </div>
          <DeviceTable
            :devices="view === 'devices' ? filteredRows : view === 'overview' ? rows.slice(0, 5) : rows"
            :rates="liveRates"
            :presence="livePresence"
            :last-active-at="lastActiveAt"
            :time-zone="traffic?.timeZone"
            :selected-device-id="scope"
            :loading="loading"
            :empty-label="deviceError ? '设备列表加载失败' : devices.length ? '没有符合条件的设备' : '还没有设备'"
            @subscription="openModal({ type: 'subscription', device: $event })"
            @rename="openModal({ type: 'rename', device: $event })"
            @toggle="openModal({ type: 'toggle', device: $event })"
            @traffic="openDeviceTraffic"
          >
            <template #empty>
              <button v-if="!loading && !devices.length && !deviceError" class="button compact" type="button" @click="openModal({ type: 'create' })">
                <Plus :size="15" aria-hidden="true" />新增设备
              </button>
            </template>
          </DeviceTable>
        </section>
      </div>
    </main>
  </div>

  <dialog ref="dialog" class="modal" aria-labelledby="modal-title" @cancel="cancelDialog" @close="clearModal">
    <div v-if="modal" class="modal-body">
      <div class="modal-header">
        <h2 id="modal-title">{{ modalTitle }}</h2>
        <button type="button" class="icon-button" :disabled="modalBusy" title="关闭" aria-label="关闭" @click="closeModal"><X :size="18" aria-hidden="true" /></button>
      </div>
      <form v-if="modal.type === 'create' || modal.type === 'rename'" class="modal-form" @submit.prevent="saveDevice">
        <label for="device-name">设备名称</label>
        <input id="device-name" v-model="deviceName" type="text" class="input" placeholder="例如：我的手机" maxlength="96" :disabled="modalBusy" required autofocus />
        <p v-if="modalError" class="form-error" role="alert">{{ modalError }}</p>
        <footer class="modal-actions">
          <button type="button" class="button" :disabled="modalBusy" @click="closeModal">取消</button>
          <button type="submit" class="button primary" :disabled="modalBusy">
            {{ modalBusy ? '正在保存…' : modal.type === 'create' && hasPendingDeviceCreation(deviceName.trim()) ? '确认上次提交' : '保存' }}
          </button>
        </footer>
      </form>

      <form v-else-if="modal.type === 'toggle'" class="modal-form" @submit.prevent="saveDevice">
        <p v-if="modal.device.enabled" class="muted">停用「{{ modal.device.name }}」后，设备将无法连接，已有流量记录会保留。</p>
        <p v-else class="muted">启用「{{ modal.device.name }}」后，设备可继续使用原配置连接。</p>
        <p v-if="modalError" class="form-error" role="alert">{{ modalError }}</p>
        <footer class="modal-actions">
          <button type="button" class="button" :disabled="modalBusy" @click="closeModal">取消</button>
          <button type="submit" class="button" :class="modal.device.enabled ? 'danger' : 'primary'" :disabled="modalBusy">
            {{ modalBusy ? '正在保存…' : modal.device.enabled ? '确认停用' : '确认启用' }}
          </button>
        </footer>
      </form>

      <div v-else-if="modal.type === 'subscription'" class="modal-form">
        <p class="muted">选择「{{ modal.device.name }}」使用的客户端。</p>
        <div class="segmented formats" aria-label="客户端格式">
          <button v-for="item in formats" :key="item.id" type="button" :aria-pressed="format === item.id" @click="format = item.id">{{ item.text }}</button>
        </div>
        <label for="subscription-url">订阅链接</label>
        <div class="copy-field">
          <input id="subscription-url" :value="subscriptionURL" type="text" class="input mono subscription-url" :placeholder="modalBusy ? '正在获取…' : '订阅链接暂不可用'" readonly />
          <button class="button" type="button" :disabled="!subscriptionURL" @click="copySubscription"><Clipboard :size="15" aria-hidden="true" />复制</button>
        </div>
        <SubscriptionQR
          v-if="subscriptionURL && !modalBusy"
          :key="subscriptionURL"
          :device-id="modal.device.id"
          :format="format"
          :format-name="formatName"
          @expired="showLogin('登录已过期，请重新登录。')"
        />
        <p v-if="confirmingRotation" class="form-error" role="status">更新后，旧订阅链接将失效。已导入的节点配置仍可使用。</p>
        <p v-if="modalError" class="form-error" role="alert">{{ modalError }}</p>
        <p v-if="modalFeedback" class="form-feedback" role="status">{{ modalFeedback }}</p>
        <footer class="modal-actions">
          <button v-if="confirmingRotation" type="button" class="button" :disabled="modalBusy" @click="confirmingRotation = false">取消更新</button>
          <button type="button" class="button" :class="{ danger: confirmingRotation }" :disabled="modalBusy || !url" @click="rotateSubscription">
            <Link2 :size="15" aria-hidden="true" />{{ confirmingRotation ? '确认更新链接' : '更新订阅链接' }}
          </button>
        </footer>
      </div>
    </div>
  </dialog>
  <div v-if="toast" role="status" class="toast">{{ toast }}</div>
</template>
