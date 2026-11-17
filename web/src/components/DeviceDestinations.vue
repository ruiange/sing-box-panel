<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ArrowDown, RefreshCw } from 'lucide-vue-next'
import { api, APIError, errorMessage, type DeviceDestination, type DeviceDestinationPage } from '../api'

const props = defineProps<{
  deviceId: string
  deviceName: string
  timeZone?: string
}>()
const emit = defineEmits<{ expired: [] }>()
const items = ref<DeviceDestination[]>([])
const nextCursor = ref<string>()
const list = ref<HTMLElement>()
type LoadMode = 'refresh' | 'more'
const busy = ref<LoadMode | null>('refresh')
const error = ref<{ mode: LoadMode; message: string }>()
const timeFormatter = computed(() => new Intl.DateTimeFormat('zh-CN', {
  timeZone: props.timeZone,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
}))
let request: AbortController | undefined

async function loadDestinations(before?: string) {
  request?.abort()
  const controller = new AbortController()
  request = controller
  const mode: LoadMode = before ? 'more' : 'refresh'
  busy.value = mode
  error.value = undefined
  const parameters = new URLSearchParams({ limit: '100' })
  if (before) parameters.set('before', before)
  try {
    const result = await api<DeviceDestinationPage>(
      '/devices/' + encodeURIComponent(props.deviceId) + '/destinations?' + parameters,
      'GET', undefined, controller.signal,
    )
    if (controller.signal.aborted) return
    if (before) {
      items.value.push(...result.items)
    } else {
      items.value = result.items
      list.value?.scrollTo({ top: 0 })
    }
    nextCursor.value = result.nextCursor
  } catch (failure) {
    if (controller.signal.aborted) return
    if (failure instanceof APIError && failure.status === 401) emit('expired')
    else error.value = { mode, message: errorMessage(failure) }
  } finally {
    if (!controller.signal.aborted) busy.value = null
  }
}

onMounted(loadDestinations)
onBeforeUnmount(() => request?.abort())
</script>

<template>
  <div class="modal-form">
    <p class="muted">「{{ deviceName }}」经过节点的连接目标</p>
    <div v-if="items.length" ref="list" class="destinations-scroll" tabindex="0" role="region" aria-label="访问目标列表" :aria-busy="busy !== null">
      <table class="destinations-table">
        <thead>
          <tr>
            <th scope="col">目标地址</th>
            <th scope="col">访问时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td><bdi class="mono">{{ item.address }}</bdi></td>
            <td><time class="mono" :datetime="item.lastSeenAt">{{ timeFormatter.format(new Date(item.lastSeenAt)) }}</time></td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else-if="busy" class="destination-state muted" role="status">正在读取访问记录…</p>
    <p v-else-if="!error" class="destination-state muted" role="status">暂无访问记录</p>
    <p v-if="error" class="form-error" role="alert">{{ error.message }}</p>
    <footer class="modal-actions">
      <button type="button" class="button" :disabled="busy !== null" @click="loadDestinations()">
        <RefreshCw :size="15" :class="{ spinning: busy === 'refresh' }" aria-hidden="true" />{{ error?.mode === 'refresh' ? '重试刷新' : '刷新' }}
      </button>
      <button v-if="nextCursor" type="button" class="button" :disabled="busy !== null" @click="loadDestinations(nextCursor)">
        <RefreshCw v-if="busy === 'more'" :size="15" class="spinning" aria-hidden="true" />
        <ArrowDown v-else :size="15" aria-hidden="true" />{{ error?.mode === 'more' ? '重试加载' : '加载更早记录' }}
      </button>
    </footer>
  </div>
</template>
