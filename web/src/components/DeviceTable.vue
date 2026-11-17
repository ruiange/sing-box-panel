<script setup lang="ts">
import { computed } from 'vue'
import { Activity, ArrowDown, ArrowUp, History, Link2, Pencil, Power, Radio, Smartphone } from 'lucide-vue-next'
import type { Device, DevicePresence, LiveRate, PeriodUsage } from '../api'
import { formatBytes, formatRate, formatTime, totalUsage } from '../format'

const props = defineProps<{
  devices: Array<Device & { usage?: PeriodUsage }>
  rates?: Record<string, LiveRate>
  presence?: Record<string, DevicePresence>
  lastActiveAt: ReadonlyMap<string, string>
  timeZone?: string
  selectedDeviceId?: string
  loading: boolean
  emptyLabel: string
}>()
defineEmits<{
  subscription: [device: Device]
  rename: [device: Device]
  toggle: [device: Device]
  traffic: [device: Device]
  destinations: [device: Device]
}>()

const statuses = {
  active: { label: '活跃', detail: '最近 60 秒有代理流量', tone: 'enabled' },
  idle: { label: '暂无活动', detail: '最近 60 秒未观察到代理流量', tone: '' },
  disabled: { label: '已停用', detail: '设备已在后台停用', tone: '' },
  unknown: { label: '状态未知', detail: '当前采样不可用', tone: '' },
}

const rows = computed(() => props.devices.map((device) => {
  const state = device.enabled ? props.presence?.[device.id]?.state ?? 'unknown' : 'disabled'
  return { device, status: statuses[state] ?? statuses.unknown, activeAt: props.lastActiveAt.get(device.id) }
}))
</script>

<template>
  <div v-if="devices.length" class="table-scroll">
    <table class="device-table">
      <thead>
        <tr>
          <th scope="col">设备</th>
          <th scope="col">状态</th>
          <th scope="col">实时网速</th>
          <th scope="col">今日流量</th>
          <th scope="col">本月流量</th>
          <th scope="col">累计流量</th>
          <th scope="col" class="table-actions-heading">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="{ device, status, activeAt } in rows" :key="device.id" :class="{ 'device-selected': selectedDeviceId === device.id }">
          <th scope="row" class="device-column">
            <span class="device-icon"><Smartphone :size="16" aria-hidden="true" /></span>
            <span class="device-text">
              <strong>{{ device.name }}</strong>
              <small>{{ formatTime(device.createdAt, timeZone) }} 添加</small>
            </span>
          </th>
          <td class="device-status">
            <div class="device-activity">
              <span class="badge" :class="status.tone" :title="status.detail">
                <span class="status-dot" aria-hidden="true"></span>{{ status.label }}
              </span>
              <small v-if="activeAt" class="device-last-active">
                最后活跃 <time :datetime="activeAt">{{ formatTime(activeAt, timeZone) }}</time>
              </small>
              <small v-else class="device-last-active">暂无活跃记录</small>
            </div>
          </td>
          <td class="device-speed">
            <span class="mobile-label">实时网速</span>
            <div v-if="device.enabled && rates?.[device.id]" class="row-directions mono">
              <span title="上传速度"><ArrowUp :size="12" aria-label="上传速度" />{{ formatRate(rates[device.id].upload) }}</span>
              <span title="下载速度"><ArrowDown :size="12" aria-label="下载速度" />{{ formatRate(rates[device.id].download) }}</span>
            </div>
            <span v-else class="muted">—</span>
          </td>
          <td class="device-usage">
            <span class="mobile-label">今日流量</span>
            <span v-if="device.usage" class="mono">{{ formatBytes(totalUsage(device.usage.today)) }}</span>
            <span v-else class="muted">—</span>
          </td>
          <td class="device-usage">
            <span class="mobile-label">本月流量</span>
            <strong v-if="device.usage" class="mono usage-primary">{{ formatBytes(totalUsage(device.usage.month)) }}</strong>
            <span v-else class="muted">—</span>
            <div v-if="device.usage" class="row-directions">
              <span title="上传"><ArrowUp :size="12" aria-label="上传" />{{ formatBytes(device.usage.month.upload) }}</span>
              <span title="下载"><ArrowDown :size="12" aria-label="下载" />{{ formatBytes(device.usage.month.download) }}</span>
            </div>
          </td>
          <td class="device-usage">
            <span class="mobile-label">累计流量</span>
            <span v-if="device.usage" class="mono">{{ formatBytes(totalUsage(device.usage.all)) }}</span>
            <span v-else class="muted">—</span>
          </td>
          <td class="device-actions">
            <div class="table-actions">
              <button class="button compact" type="button" :aria-pressed="selectedDeviceId === device.id" :aria-label="'查看 ' + device.name + ' 的流量'" @click="$emit('traffic', device)">
                <Activity :size="14" aria-hidden="true" />流量
              </button>
              <button v-if="device.enabled" class="button compact" type="button" @click="$emit('subscription', device)">
                <Link2 :size="14" aria-hidden="true" />订阅
              </button>
              <button class="button compact" type="button" :aria-label="'查看 ' + device.name + ' 的访问记录'" @click="$emit('destinations', device)">
                <History :size="14" aria-hidden="true" />访问记录
              </button>
              <button class="icon-button" type="button" :title="'重命名 ' + device.name" :aria-label="'重命名 ' + device.name" @click="$emit('rename', device)">
                <Pencil :size="15" aria-hidden="true" />
              </button>
              <button class="icon-button" type="button" :title="(device.enabled ? '停用 ' : '启用 ') + device.name" :aria-label="(device.enabled ? '停用 ' : '启用 ') + device.name" @click="$emit('toggle', device)">
                <Power :size="15" aria-hidden="true" />
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <div v-else class="empty-state">
    <Radio :size="24" aria-hidden="true" />
    <p>{{ loading ? '正在读取设备…' : emptyLabel }}</p>
    <slot name="empty"></slot>
  </div>
</template>
