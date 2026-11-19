<script setup lang="ts">
/* 考勤屏（设计 6.10）：大二维码 + 刷新按钮 + 60 秒倒计时 + 最近登记滚动 */
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { toDataURL } from 'qrcode'
import { api, formatTime } from '../api'
import { toastError } from '../toast'

const point = ref(localStorage.getItem('clothp_att_point') || '厂门口')
const qrDataUrl = ref('')
const ticket = ref('')
const expiresAt = ref(0)
const remainSec = ref(0)
const error = ref('')
const refreshing = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

async function refresh() {
  if (!point.value.trim()) { error.value = '请先填写考勤点'; return }
  refreshing.value = true; error.value = ''
  try {
    localStorage.setItem('clothp_att_point', point.value.trim())
    const r = await api<{ ticket: string; expires_at: number }>('/attendance/qr/refresh', {
      method: 'POST', body: JSON.stringify({ point: point.value.trim() }),
    })
    ticket.value = r.ticket
    expiresAt.value = r.expires_at
    /* 二维码内容为扫码落地页链接，员工手机扫码后直接进入登记页 */
    const url = `${location.origin}/attendance/scan?ticket=${encodeURIComponent(r.ticket)}`
    qrDataUrl.value = await toDataURL(url, { width: 360, margin: 1 })
  } catch { qrDataUrl.value = '' } finally { refreshing.value = false }
}

const recent = ref<any[]>([])
async function loadRecent() {
  try {
    const r = await api<{ items: any[] }>('/attendance/records?point=' + encodeURIComponent(point.value.trim()), {}, { silent: true })
    recent.value = (r.items || []).slice(0, 8)
  } catch { /* 静默 */ }
}

onMounted(() => {
  timer = setInterval(() => {
    remainSec.value = Math.max(0, Math.floor((expiresAt.value - Date.now() * 1000) / 1e6))
    if (expiresAt.value && remainSec.value === 0) { qrDataUrl.value = ''; ticket.value = '' }
  }, 500)
  void loadRecent()
  setInterval(() => void loadRecent(), 10000)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <section class="page screen-page">
    <header class="page-header">
      <h2>考勤屏</h2>
      <div class="toolbar">
        <input v-model.trim="point" class="search-input" placeholder="考勤点（如 厂门口）" style="width:180px">
        <button class="button" :disabled="refreshing" @click="refresh">刷新二维码</button>
      </div>
    </header>
    <div class="qr-area">
      <p v-if="error" class="notice error">{{ error }}</p>
      <template v-if="qrDataUrl">
        <img :src="qrDataUrl" alt="考勤二维码" class="qr-img">
        <p class="qr-hint">请用手机扫码登记 · {{ remainSec }} 秒后失效</p>
        <div class="countdown"><div class="countdown-bar" :style="{ width: (remainSec / 60 * 100) + '%' }"></div></div>
      </template>
      <div v-else class="qr-empty">
        <p>二维码已失效或尚未生成</p>
        <p class="text-muted">请先点击【刷新二维码】生成新码，再扫码考勤</p>
      </div>
    </div>
    <div class="recent-area">
      <h3>最近登记</h3>
      <div v-if="!recent.length" class="text-muted">暂无记录</div>
      <div v-for="item in recent" :key="item.id" class="recent-row">
        <span class="recent-name">{{ item.employee_name || item.employee_no }}</span>
        <span class="status" :class="item.att_type === 'in' ? 'active' : 'disabled'">{{ item.att_type === 'in' ? '上班' : '下班' }}</span>
        <span class="text-muted">{{ formatTime(item.att_at) }}</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.screen-page{max-width:720px;margin:0 auto}
.qr-area{display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:420px;background:#fff;border-radius:16px;padding:32px;box-shadow:0 8px 24px rgba(15,32,68,.08)}
.qr-img{width:360px;height:360px}
.qr-hint{margin-top:16px;font-size:16px;color:#24365e}
.qr-empty{text-align:center;color:#8a97b8;font-size:16px;line-height:2}
.countdown{width:360px;height:6px;background:#e8eefb;border-radius:3px;margin-top:12px;overflow:hidden}
.countdown-bar{height:100%;background:#2563eb;transition:width .5s linear}
.recent-area{margin-top:20px;background:#fff;border-radius:16px;padding:20px;box-shadow:0 8px 24px rgba(15,32,68,.06)}
.recent-row{display:flex;align-items:center;gap:14px;padding:8px 0;border-bottom:1px solid #f0f3fa}
.recent-name{font-weight:600;min-width:80px}
</style>
