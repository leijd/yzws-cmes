<script setup lang="ts">
/* 扫码考勤（手机端，设计 6.10）：扫考勤屏二维码进入，选择上班/下班登记 */
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, formatTime } from '../api'

const route = useRoute()
const ticket = ref((route.query.ticket as string) || '')
const result = ref<any>(null)
const error = ref('')
const submitting = ref(false)

async function submit(attType: 'in' | 'out') {
  if (!ticket.value) { error.value = '二维码内容无效，请重新扫码'; return }
  submitting.value = true; error.value = ''; result.value = null
  try {
    result.value = await api('/attendance/scan', {
      method: 'POST',
      body: JSON.stringify({ ticket: ticket.value, att_type: attType, device_info: navigator.userAgent.slice(0, 200) }),
    }, { silent: true })
  } catch (e: any) {
    error.value = e.message || '登记失败'
  } finally { submitting.value = false }
}

onMounted(() => { /* 二维码链接已带 ticket */ })
</script>

<template>
  <div class="scan-page">
    <div class="scan-card">
      <h2>考勤登记</h2>
      <template v-if="!result">
        <p class="hint">请确认考勤类型</p>
        <div class="actions">
          <button class="big-btn in" :disabled="submitting" @click="submit('in')">上班考勤</button>
          <button class="big-btn out" :disabled="submitting" @click="submit('out')">下班考勤</button>
        </div>
        <p v-if="error" class="error-msg">{{ error }}</p>
        <p class="tip">二维码由考勤屏实时生成，60 秒有效、一码一次</p>
      </template>
      <template v-else>
        <div class="success-icon">✓</div>
        <h3>{{ result.att_type === 'in' ? '上班' : '下班' }}登记成功</h3>
        <p class="time">{{ formatTime(result.att_at) }}</p>
        <p class="text-muted">考勤点：{{ result.point }}</p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.scan-page{min-height:100vh;display:flex;align-items:center;justify-content:center;background:linear-gradient(160deg,#0f2044,#1d3a7a);padding:20px}
.scan-card{background:#fff;border-radius:20px;padding:36px 28px;width:100%;max-width:380px;text-align:center;box-shadow:0 20px 60px rgba(0,0,0,.3)}
.hint{color:#5a6a8f;margin-bottom:20px}
.actions{display:flex;flex-direction:column;gap:14px}
.big-btn{border:0;border-radius:14px;padding:18px;font-size:18px;font-weight:700;color:#fff;cursor:pointer}
.big-btn.in{background:#2563eb}
.big-btn.out{background:#0d9488}
.big-btn:disabled{opacity:.5}
.error-msg{color:#e11d48;margin-top:16px;font-size:14px}
.tip{margin-top:20px;color:#8a97b8;font-size:12px}
.success-icon{width:64px;height:64px;border-radius:50%;background:#dcfce7;color:#16a34a;font-size:36px;display:flex;align-items:center;justify-content:center;margin:0 auto 12px}
.time{font-size:20px;font-weight:600;color:#17255f;margin-top:8px}
</style>
