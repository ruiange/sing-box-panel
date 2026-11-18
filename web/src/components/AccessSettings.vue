<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ArrowUpRight, CircleAlert, RefreshCw } from 'lucide-vue-next'
import { api, APIError, errorMessage, type AccessMode, type AccessSettings } from '../api'

const emit = defineEmits<{ expired: [] }>()
const endpoint = '/settings/access'
const maxPEMSize = 64 * 1024
const modeLabels = {
  off: '未开启',
  auto: '自动签发',
  manual: '自有证书',
} satisfies Record<AccessMode, string>

const settings = ref<AccessSettings>()
const mode = ref<AccessMode>('auto')
const domain = ref('')
const certificateInput = ref<HTMLInputElement>()
const privateKeyInput = ref<HTMLInputElement>()
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const feedback = ref('')
let request: AbortController | undefined

const canUpload = computed(() => settings.value?.manualUploadAllowed === true && window.location.protocol === 'https:')
const changesEntry = computed(() => settings.value?.httpsUrl &&
  (mode.value === 'off' || domain.value.trim().toLowerCase() !== settings.value.domain))
const expiresAt = computed(() => {
  if (!settings.value?.expiresAt) return ''
  const date = new Date(settings.value.expiresAt)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
})

function pageURL(address: string) {
  return address.replace(/\/$/, '') + '/#access'
}

function clearFiles() {
  if (certificateInput.value) certificateInput.value.value = ''
  if (privateKeyInput.value) privateKeyInput.value.value = ''
}

function handleError(failure: unknown, savingChanges = false) {
  if (failure instanceof APIError && failure.status === 401) {
    emit('expired')
  } else if (savingChanges && failure instanceof APIError && failure.status === 0 && settings.value?.fallbackUrl) {
    error.value = '连接已中断。可用原 IP 地址登录，查看配置是否生效。'
  } else {
    error.value = errorMessage(failure)
  }
}

async function load() {
  const controller = new AbortController()
  request = controller
  loading.value = true
  error.value = ''
  try {
    const current = await api<AccessSettings>(endpoint, 'GET', undefined, controller.signal)
    if (controller.signal.aborted) return
    settings.value = current
    mode.value = current.mode === 'off' ? 'auto' : current.mode
    domain.value = current.domain ?? ''
  } catch (failure) {
    if (!controller.signal.aborted) handleError(failure)
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}

async function save() {
  if (saving.value || !settings.value?.available || (mode.value === 'manual' && !canUpload.value)) return
  const controller = new AbortController()
  request = controller
  saving.value = true
  error.value = ''
  feedback.value = ''
  try {
    const input: { mode: AccessMode; domain?: string; certificate?: string; privateKey?: string } = { mode: mode.value }
    if (input.mode !== 'off') {
      input.domain = domain.value.trim()
      if (!input.domain) throw new Error('请填写域名。')
    }
    if (input.mode === 'manual') {
      const certificate = certificateInput.value?.files?.item(0)
      const privateKey = privateKeyInput.value?.files?.item(0)
      if (!certificate || !privateKey) throw new Error('请选择证书和私钥文件。')
      if (certificate.size > maxPEMSize || privateKey.size > maxPEMSize) {
        throw new Error('每个 PEM 文件最多 64 KiB。')
      }
      input.certificate = await certificate.text()
      input.privateKey = await privateKey.text()
    }
    if (controller.signal.aborted) return
    const update = api<AccessSettings>(endpoint, 'PUT', input, controller.signal)
    delete input.privateKey
    delete input.certificate
    clearFiles()
    const current = await update
    if (controller.signal.aborted) return
    settings.value = current
    mode.value = current.mode
    domain.value = current.domain ?? ''
    feedback.value = current.mode === 'off' ? '域名 HTTPS 已关闭。' : '域名 HTTPS 已启用。'
  } catch (failure) {
    if (!controller.signal.aborted) handleError(failure, true)
  } finally {
    clearFiles()
    if (!controller.signal.aborted) saving.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => {
  request?.abort()
  clearFiles()
})
</script>

<template>
  <div class="access-settings">
    <section class="panel" aria-labelledby="access-status-heading">
      <header class="panel-head">
        <div>
          <h2 id="access-status-heading">域名与证书</h2>
          <p>为网页后台配置独立的 HTTPS 访问地址。</p>
        </div>
        <span v-if="settings?.available" class="badge" :class="{ enabled: settings.mode !== 'off' }">
          <span class="status-dot" aria-hidden="true"></span>{{ modeLabels[settings.mode] }}
        </span>
      </header>
      <div class="access-body">
        <p v-if="loading" class="access-progress muted" role="status">
          <RefreshCw :size="15" class="spinning" aria-hidden="true" />正在读取配置…
        </p>
        <template v-else-if="settings">
          <dl class="access-details">
            <div>
              <dt>HTTPS 入口</dt>
              <dd>
                <a v-if="settings.httpsUrl" class="text-link mono" :href="pageURL(settings.httpsUrl)" target="_blank" rel="noopener noreferrer">
                  {{ settings.httpsUrl }}<ArrowUpRight :size="14" aria-hidden="true" />
                </a>
                <span v-else class="muted">未设置</span>
              </dd>
            </div>
            <div v-if="expiresAt">
              <dt>证书到期</dt>
              <dd><time :datetime="settings.expiresAt">{{ expiresAt }}</time></dd>
            </div>
            <div v-if="settings.fallbackUrl">
              <dt>原 IP 入口</dt>
              <dd>
                <a class="text-link mono" :href="pageURL(settings.fallbackUrl)" target="_blank" rel="noopener noreferrer">
                  {{ settings.fallbackUrl }}<ArrowUpRight :size="14" aria-hidden="true" />
                </a>
              </dd>
            </div>
          </dl>
          <p v-if="!settings.available" class="muted">当前部署模式由服务器配置管理，网页内无法修改。</p>
          <p v-else-if="settings.httpsUrl && !canUpload" class="muted">打开 HTTPS 入口后，需要重新登录。</p>
          <p v-if="settings.warning" class="notice access-notice" role="status">
            <CircleAlert :size="17" aria-hidden="true" /><span>{{ settings.warning }}</span>
          </p>
        </template>
        <template v-else>
          <p class="form-error" role="alert">{{ error }}</p>
          <button type="button" class="button" @click="load"><RefreshCw :size="15" aria-hidden="true" />重试</button>
        </template>
      </div>
    </section>

    <section v-if="settings?.available" class="panel" aria-labelledby="access-config-heading">
      <header class="panel-head">
        <h2 id="access-config-heading">配置 HTTPS</h2>
      </header>
      <form class="access-body access-form" @submit.prevent="save">
        <fieldset :disabled="saving" class="access-fieldset">
          <legend>证书方式</legend>
          <div class="access-options">
            <label><input v-model="mode" type="radio" value="auto" />自动签发</label>
            <label><input v-model="mode" type="radio" value="manual" />自有证书</label>
            <label><input v-model="mode" type="radio" value="off" />关闭</label>
          </div>
          <template v-if="mode !== 'off'">
            <label class="access-field">
              <span>面板域名</span>
              <input v-model="domain" class="input mono" type="text" placeholder="panel.example.com" maxlength="253" autocomplete="off" autocapitalize="none" spellcheck="false" required />
            </label>
            <p v-if="mode === 'auto'" class="muted access-help">
              将域名的 A / AAAA 记录指向本服务器，放行公网 TCP 443。使用 Cloudflare 时请选择「仅 DNS」。
              应用即同意 Let’s Encrypt 的<a href="https://letsencrypt.org/repository/" target="_blank" rel="noopener noreferrer">服务条款</a>，证书将自动续期。
            </p>
            <template v-else-if="canUpload">
              <label class="access-field">
                <span>证书链</span>
                <input ref="certificateInput" class="input access-file" type="file" accept=".pem,.crt,.cer" required />
              </label>
              <label class="access-field">
                <span>私钥</span>
                <input ref="privateKeyInput" class="input access-file" type="file" accept=".pem,.key" required />
              </label>
              <p class="muted access-help">上传 PEM 格式的完整证书链和对应的未加密私钥。手动证书到期前需重新上传。</p>
            </template>
            <p v-else class="muted access-help">
              <template v-if="settings.httpsUrl">
                请从 <a :href="pageURL(settings.httpsUrl)" target="_blank" rel="noopener noreferrer">HTTPS 入口</a>登录后再上传私钥。
              </template>
              <template v-else>请先使用自动签发开启 HTTPS，再从 HTTPS 地址登录并上传私钥。</template>
            </p>
          </template>
          <p v-if="changesEntry" class="notice access-notice">
            <CircleAlert :size="17" aria-hidden="true" />
            <span>应用后当前域名地址可能断开，可通过原 IP 入口重新登录。</span>
          </p>
        </fieldset>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <p v-if="feedback" class="form-feedback" role="status">{{ feedback }}</p>
        <p v-if="saving" class="access-progress muted" role="status">
          <RefreshCw :size="15" class="spinning" aria-hidden="true" />
          {{ mode === 'auto' ? '正在签发证书，可能需要一分钟，请保持页面打开。' : '正在应用配置…' }}
        </p>
        <div class="access-actions">
          <button
            class="button"
            :class="mode === 'off' ? 'danger' : 'primary'"
            type="submit"
            :disabled="saving || (mode === 'manual' && !canUpload) || (mode === 'off' && settings.mode === 'off' && !settings.warning)"
          >{{ mode === 'off' ? '关闭 HTTPS' : '应用配置' }}</button>
        </div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.access-settings { max-width: 740px; }
.access-body { padding: 0 18px 18px; }
.access-body > :last-child { margin-bottom: 0; }
.access-details { display: grid; gap: 8px; margin: 0 0 12px; }
.access-details > div {
  display: grid;
  grid-template-columns: 7.25em minmax(0, 1fr);
  align-items: baseline;
  gap: 8px;
}
.access-details dt { color: var(--muted); }
.access-details dd { margin: 0; min-width: 0; }
.access-details .text-link { white-space: normal; overflow-wrap: anywhere; }
.access-progress { display: flex; align-items: center; gap: 8px; }
.access-form, .access-fieldset, .access-field { display: grid; gap: 10px; }
.access-fieldset { min-width: 0; margin: 0; border: 0; padding: 0; gap: 14px; }
.access-fieldset legend { margin: 0 0 8px; padding: 0; }
.access-fieldset legend, .access-field > span { font-size: 13px; font-weight: 600; }
.access-options { display: flex; flex-wrap: wrap; gap: 6px 16px; }
.access-options label { display: inline-flex; align-items: center; gap: 5px; min-height: 32px; }
.access-options input { margin: 0; accent-color: var(--accent); }
.access-field { gap: 5px; }
.access-file { min-width: 0; width: 100%; padding: 6px 9px; }
.access-file::file-selector-button {
  margin-right: 9px;
  border: 1px solid var(--line-bright);
  border-radius: 5px;
  padding: 3px 7px;
  color: var(--text);
  background: var(--surface-raised);
}
.access-form p { margin: 0; line-height: 1.5; }
.access-help a { color: var(--accent); text-decoration: underline; text-underline-offset: 3px; }
.access-actions { display: flex; justify-content: flex-end; margin-top: 4px; }
@media (max-width: 740px) {
  .access-body { padding: 0 12px 13px; }
  .access-details > div { grid-template-columns: 5.75em minmax(0, 1fr); }
}
</style>
