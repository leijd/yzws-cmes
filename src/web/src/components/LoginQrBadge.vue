<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  QR_APP_COLORS, QR_APP_LOGIN, QR_APP_NAMES, type QrApp,
  createLoginQr, formatQrCountdown, getQrStatus, makeQrImage,
  moduleLoginUrl, qrLoginUrl,
} from '../qrLogin'
import TrialAccountDialog from './TrialAccountDialog.vue'
import { saveTrialSession } from '../trial'

const props = defineProps<{ app: QrApp }>()
const router = useRouter()
const appName = computed(() => QR_APP_NAMES[props.app] || '应用')
const color = computed(() => QR_APP_COLORS[props.app] || '#1d4ed8')

type ViewState = 'loading' | 'ready' | 'offline' | 'scanned' | 'confirmed' | 'expired' | 'cancelled'
const state = ref<ViewState>('loading')
const thumb = ref('')
const large = ref('')
const ticket = ref('')
const qrTarget = ref('')
const secondsLeft = ref(0)
const showModal = ref(false)
const showAccount = ref(false)
const hint = ref('')
const copied = ref(false)
let pollTimer: number | undefined
let pollFailures = 0

const countdown = computed(() => formatQrCountdown(secondsLeft.value))
const statusText = computed(() => ({
  loading: '正在生成二维码…',
  ready: '等待扫码',
  offline: '离线演示模式',
  scanned: '已扫码，请在手机上点击确认',
  confirmed: '登录成功，正在进入…',
  expired: '二维码已过期，请刷新',
  cancelled: '已取消，请刷新二维码',
}[state.value]))

function stopPolling() {
  if (pollTimer) { window.clearInterval(pollTimer); pollTimer = undefined }
}

async function setupQr() {
  stopPolling()
  pollFailures = 0
  state.value = 'loading'
  hint.value = ''
  ticket.value = ''
  let url: string
  try {
    const result = await createLoginQr(props.app)
    ticket.value = result.ticket
    secondsLeft.value = result.expires_in || 300
    url = qrLoginUrl(result.ticket, props.app)
    state.value = 'ready'
  } catch {
    /* 后端未启动时退化为「扫码直接打开该模块登录页」，二维码依然是真实可扫的 */
    url = moduleLoginUrl(props.app)
    state.value = 'offline'
    hint.value = `二维码服务未启动，扫码将直接打开「${appName.value}」登录页；也可改用账号密码登录。`
  }
  qrTarget.value = url
  /* 生成 2 倍分辨率再平滑缩放，避免卡片小图 / 弹层大图出现像素化影响识别 */
  large.value = await makeQrImage(url, color.value, 720)
  thumb.value = await makeQrImage(url, color.value, 360)
  if (showModal.value && state.value === 'ready') startPolling()
}

async function onConfirmed() {
  stopPolling()
  state.value = 'confirmed'
  saveTrialSession({
    phone: '扫码用户',
    ticket: `qr-${ticket.value}`,
    expiresAt: Date.now() + 30 * 60 * 1000,
    displayName: '扫码用户',
    via: 'qr',
  })
  await new Promise(r => setTimeout(r, 600))
  showModal.value = false
  await router.push(`/trial/home?app=${props.app}&via=qr`)
}

function startPolling() {
  stopPolling()
  pollFailures = 0
  pollTimer = window.setInterval(async () => {
    if (!ticket.value || state.value === 'confirmed') return
    if (secondsLeft.value > 0) secondsLeft.value--
    try {
      const result = await getQrStatus(ticket.value)
      pollFailures = 0
      secondsLeft.value = result.expires_in
      if (result.status === 'confirmed') { await onConfirmed(); return }
      if (result.status === 'expired') { state.value = 'expired'; stopPolling(); return }
      if (result.status === 'cancelled') state.value = 'cancelled'
      else if (result.status === 'scanned') state.value = 'scanned'
      else if (state.value === 'scanned' || state.value === 'cancelled') state.value = 'ready'
    } catch {
      /* 后端断开或票据丢失时，明确提示用户换用账号登录，避免一直显示「等待扫码」 */
      pollFailures++
      if (pollFailures >= 3) {
        stopPolling()
        hint.value = '二维码服务连接失败，请刷新二维码，或改用下方账号密码登录'
      }
    }
  }, 1000)
}

function openModal() {
  showModal.value = true
  if (state.value === 'ready') startPolling()
}
function closeModal() { showModal.value = false; stopPolling() }

async function copyAddress() {
  try {
    await navigator.clipboard.writeText(qrTarget.value)
    copied.value = true
    window.setTimeout(() => { copied.value = false }, 1600)
  } catch {
    hint.value = '复制失败，请手动选中下方地址复制'
  }
}

function openAccount() {
  closeModal()
  showAccount.value = true
}

onMounted(setupQr)
onBeforeUnmount(stopPolling)
</script>

<template>
  <button class="qr-badge" type="button" :title="`扫码登录 ${appName}（点击放大）`" @click="openModal">
    <img v-if="thumb" :src="thumb" :alt="`${appName}扫码登录二维码`" />
    <span v-else class="qr-ph">二维码</span>
  </button>

  <Teleport to="body">
    <div v-if="showModal" class="qr-overlay" @click.self="closeModal">
      <div class="qr-dialog" role="dialog" aria-modal="true" :aria-label="`扫码登录 ${appName}`">
        <button class="qr-close" type="button" aria-label="关闭" @click="closeModal">✕</button>
        <div class="qr-head">
          <span class="qr-mark" :style="{ background: color }">扫</span>
          <div>
            <h3>扫码登录 · {{ appName }}</h3>
            <p>请扫描下方放大后的二维码，确认后自动进入</p>
          </div>
        </div>

        <div class="qr-frame" :style="{ borderColor: color }">
          <img v-if="large" :src="large" :alt="`${appName}扫码登录二维码`" />
          <div v-else class="qr-loading">二维码生成中…</div>
        </div>

        <div class="qr-status" :class="state"><span class="qr-dot"></span>{{ statusText }}</div>
        <p v-if="hint" class="qr-hint">{{ hint }}</p>

        <div class="qr-meta">
          <span v-if="state === 'ready' || state === 'scanned'">有效期 {{ countdown }}</span>
          <span v-else></span>
          <button type="button" class="qr-refresh" @click="setupQr">刷新二维码</button>
        </div>
        <div class="qr-trouble">
          <p class="qr-trouble-title">扫码说明</p>
          <p class="qr-trouble-note">二维码已指向公网体验地址，手机无需与电脑同一网络。若打不开，请确认该域名的隧道服务在线；也可以直接改用账号密码登录。</p>
          <p class="qr-target" :title="qrTarget">{{ qrTarget || '二维码生成中…' }}</p>
          <div class="qr-trouble-actions">
            <button type="button" class="qr-copy" @click="copyAddress">{{ copied ? '已复制' : '复制地址' }}</button>
            <button type="button" class="qr-account" :style="{ background: color }" @click="openAccount">改用账号密码登录</button>
          </div>
        </div>
        <RouterLink :to="QR_APP_LOGIN[app]" class="qr-direct" @click="closeModal">
          直接打开「{{ appName }}」登录页 →
        </RouterLink>
      </div>
    </div>
    <TrialAccountDialog v-if="showAccount" :app="app" @close="showAccount = false" />
  </Teleport>
</template>

<style scoped>
.qr-badge{position:absolute;top:12px;right:12px;width:72px;height:72px;padding:3px;border-radius:8px;
  background:#fff;border:1px solid rgba(23,37,95,.08);box-shadow:0 2px 10px rgba(23,37,95,.14);
  cursor:pointer;display:grid;place-items:center;transition:transform .16s,box-shadow .16s;z-index:2}
.qr-badge:hover{transform:scale(1.06);box-shadow:0 6px 18px rgba(23,37,95,.22)}
.qr-badge img{width:100%;height:100%;display:block;image-rendering:auto}
.qr-ph{font-size:10px;color:#8b9ac0;line-height:1.2}
/* 弹层 */
.qr-overlay{position:fixed;inset:0;background:rgba(10,20,50,.48);backdrop-filter:blur(4px);
  display:grid;place-items:center;z-index:200;padding:20px}
.qr-dialog{position:relative;width:min(380px,100%);max-height:92vh;overflow-y:auto;background:#fff;border-radius:18px;padding:26px 28px 22px;
  box-shadow:0 24px 60px rgba(10,20,50,.35);font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;text-align:center}
.qr-close{position:absolute;top:10px;right:10px;width:30px;height:30px;border:0;background:#f2f5fb;
  color:#667085;border-radius:50%;cursor:pointer;font-size:14px}
.qr-close:hover{background:#e7edf7}
.qr-head{display:flex;align-items:center;gap:12px;text-align:left;margin-bottom:18px}
.qr-mark{display:grid;place-items:center;width:40px;height:40px;border-radius:11px;color:#fff;font-weight:700;font-size:16px;flex-shrink:0}
.qr-head h3{margin:0 0 4px;font-size:18px;color:#17255f}
.qr-head p{margin:0;font-size:12.5px;color:#8b9ac0}
.qr-frame{margin:0 auto;width:280px;padding:10px;border:2px solid;border-radius:14px;background:#fff}
.qr-frame img{display:block;width:100%;height:auto;image-rendering:auto}
.qr-loading{height:220px;display:grid;place-items:center;color:#8b9ac0;font-size:13px}
.qr-status{display:inline-flex;align-items:center;gap:7px;margin:16px 0 0;font-size:13.5px;font-weight:600;color:#41537e}
.qr-dot{width:8px;height:8px;border-radius:50%;background:#94a3c4}
.qr-status.scanned,.qr-status.confirmed{color:#039855}
.qr-status.scanned .qr-dot,.qr-status.confirmed .qr-dot{background:#12b76a}
.qr-status.expired,.qr-status.cancelled,.qr-status.offline{color:#d92d20}
.qr-status.expired .qr-dot,.qr-status.cancelled .qr-dot,.qr-status.offline .qr-dot{background:#f04438}
.qr-hint{margin:8px 0 0;font-size:12.5px;line-height:1.6;color:#b54708}
.qr-meta{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-top:14px;font-size:12.5px;color:#667085}
.qr-refresh{border:1px solid #dbe4f3;background:#f4f8ff;color:#2563eb;border-radius:8px;padding:7px 12px;font-size:12.5px;font-weight:600;cursor:pointer}
.qr-refresh:hover{border-color:#2563eb}
.qr-trouble{margin-top:16px;padding:12px 14px;border-radius:12px;background:#f6f9ff;border:1px solid #e3ecfb;text-align:left}
.qr-trouble-title{margin:0 0 6px;font-size:12.5px;font-weight:700;color:#17255f}
.qr-trouble-note{margin:0;font-size:11.5px;line-height:1.7;color:#667085}
.qr-target{margin:8px 0 0;padding:7px 9px;border-radius:8px;background:#fff;border:1px solid #e3ecfb;
  font-size:11px;line-height:1.5;color:#667085;word-break:break-all;max-height:44px;overflow:hidden}
.qr-trouble-actions{display:flex;gap:8px;margin-top:10px}
.qr-copy,.qr-account{flex:1;border-radius:8px;padding:8px 6px;font-size:12px;font-weight:700;cursor:pointer;font-family:inherit}
.qr-copy{border:1px solid #dbe4f3;background:#fff;color:#2563eb}
.qr-copy:hover{border-color:#2563eb}
.qr-account{border:0;color:#fff}
.qr-account:hover{filter:brightness(1.06)}
.qr-direct{display:inline-block;margin-top:12px;font-size:12.5px;color:#2563eb;font-weight:600}
.qr-direct:hover{text-decoration:underline}
@media(max-width:600px){
  .qr-dialog{padding:22px 18px 18px}
  .qr-frame{width:min(230px,80vw)}
}
</style>
