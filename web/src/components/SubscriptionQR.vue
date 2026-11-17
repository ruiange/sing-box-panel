<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { QrCode, RefreshCw } from 'lucide-vue-next'
import { APIError, errorMessage, fetchSubscriptionQR, type SubscriptionFormat } from '../api'

const props = defineProps<{
  deviceId: string
  format: SubscriptionFormat
  formatName: string
}>()
const emit = defineEmits<{ expired: [] }>()
const image = ref('')
const busy = ref(true)
const error = ref('')
let request: AbortController | undefined

async function loadQR() {
  request?.abort()
  const controller = new AbortController()
  request = controller
  busy.value = true
  error.value = ''
  try {
    const png = await fetchSubscriptionQR(props.deviceId, props.format, controller.signal)
    if (controller.signal.aborted) return
    if (image.value) URL.revokeObjectURL(image.value)
    image.value = URL.createObjectURL(png)
  } catch (failure) {
    if (controller.signal.aborted) return
    if (failure instanceof APIError && failure.status === 401) emit('expired')
    else error.value = errorMessage(failure)
  } finally {
    if (!controller.signal.aborted) busy.value = false
  }
}

onMounted(loadQR)
onBeforeUnmount(() => {
  request?.abort()
  if (image.value) URL.revokeObjectURL(image.value)
})
</script>

<template>
  <section class="subscription-qr" aria-label="订阅二维码" :aria-busy="busy">
    <div class="subscription-qr-preview">
      <img v-if="image" :src="image" :alt="formatName + '订阅二维码'" width="128" height="128" />
      <RefreshCw v-else-if="busy" :size="21" class="spinning" aria-hidden="true" />
      <QrCode v-else :size="24" aria-hidden="true" />
    </div>
    <div class="subscription-qr-caption">
      <h3>扫码订阅</h3>
      <p v-if="busy" class="muted" role="status">正在获取二维码…</p>
      <template v-else-if="error">
        <p class="form-error" role="alert">{{ error }}</p>
        <button type="button" class="button compact" @click="loadQR">重试</button>
      </template>
      <p v-else class="muted">使用 {{ formatName }} 的扫一扫导入订阅。</p>
    </div>
  </section>
</template>
