<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  QR_APP_COLORS, QR_APP_NAMES, type QrApp, getQrStatus, getWechatStatus, postQrAction,
  qrAppFromShort, wechatDeviceID, wechatLogin, type WechatBoundAccount,
} from '../qrLogin'
import { TRIAL_RESEND_MS, formatCountdown, isValidPhone, maskPhone, sendTrialCode } from '../trial'

const route = useRoute()
/* 兼容短链 /q?t=...&a=z 与旧链接 /qr-login?ticket=...&app=zhisuan */
const ticket = String(route.query.t || route.query.ticket || '')
const shortApp = String(route.query.a || '')
const resolvedApp = shortApp ? qrAppFromShort(shortApp) : null
const app = (resolvedApp || String(route.query.app || 'zhisuan')) as QrApp
const appName = computed(() => QR_APP_NAMES[app] || '一智万商')
const color = computed(() => QR_APP_COLORS[app] || '#1d4ed8')

type PageState = 'loading' | 'bound' | 'form' | 'confirmed' | 'cancelled' | 'expired' | 'error'
const state = ref<PageState>('loading')
const message = ref('')
const busy = ref(false)

/* 本机微信身份：已绑定时只显示一键确认，不再要求密码或验证码 */
const deviceId = ref(wechatDeviceID())
const boundAccount = ref<WechatBoundAccount | null>(null)

/* 首次微信登录表单：手机号 + 验证码 + 厂管理员提供的密码（管理员本人免密码） */
const phone = ref('')
const code = ref('')
const password = ref('')
const shortCode = ref('')
const passwordNeeded = ref(false)
const sending = ref(false)
const demoCode = ref('')
const expiresAt = ref(0)
const resendAt = ref(0)
const now = ref(Date.now())
let timer: number | undefined

const countdown = computed(() => (now.value < expiresAt.value ? formatCountdown(expiresAt.value - now.value) : ''))
const resendLeft = computed(() => (now.value < resendAt.value ? Math.ceil((resendAt.value - now.value) / 1000) : 0))

function startTimer() {
  if (timer) return
  timer = window.setInterval(() => { now.value = Date.now() }, 1000)
}

async function enterWechat() {
  const status = await getWechatStatus(deviceId.value)
  if (status.bound && status.account) {
    boundAccount.value = status.account
    state.value = 'bound'
  } else {
    state.value = 'form'
  }
}

onMounted(async () => {
  startTimer()
  if (!ticket) {
    state.value = 'error'
    message.value = '二维码无效，请回到电脑端刷新后重新扫描'
    return
  }
  try {
    const result = await getQrStatus(ticket)
    if (result.status === 'expired') { state.value = 'expired'; return }
    if (result.status === 'confirmed') { state.value = 'confirmed'; return }
    if (result.status === 'cancelled') { state.value = 'cancelled'; return }
    await postQrAction('scan', ticket)
    await enterWechat()
  } catch (e) {
    state.value = 'error'
    message.value = (e as Error).message || '二维码服务未启动，请稍后重试'
  }
})

onBeforeUnmount(() => { if (timer) window.clearInterval(timer) })

async function sendCode() {
  if (sending.value) return
  if (!isValidPhone(phone.value.trim())) { message.value = '请输入正确的 11 位手机号'; return }
  sending.value = true
  message.value = ''
  try {
    const result = await sendTrialCode(phone.value.trim())
    expiresAt.value = result.expiresAt
    resendAt.value = Date.now() + TRIAL_RESEND_MS
    demoCode.value = result.demoCode
    message.value = result.smsSent ? '验证码已发送，请查看手机短信' : '演示模式：验证码已生成，请在下方直接使用'
    startTimer()
  } catch (e) {
    message.value = (e as Error).message || '验证码发送失败，请稍后重试'
  } finally { sending.value = false }
}

async function submitFirstLogin() {
  if (busy.value) return
  if (!isValidPhone(phone.value.trim())) { message.value = '请输入正确的 11 位手机号'; return }
  if (code.value.trim().length !== 6) { message.value = '请输入 6 位短信验证码'; return }
  busy.value = true
  message.value = ''
  try {
    await wechatLogin({
      ticket,
      device_id: deviceId.value,
      phone: phone.value.trim(),
      code: code.value.trim(),
      password: password.value,
      short_code: shortCode.value.trim(),
    })
    state.value = 'confirmed'
  } catch (e) {
    const error = e as Error & { code?: string }
    if (error.code === 'PASSWORD_REQUIRED') passwordNeeded.value = true
    message.value = error.message || '微信登录失败，请检查信息后重试'
  } finally { busy.value = false }
}

async function confirmBound() {
  if (busy.value) return
  busy.value = true
  message.value = ''
  try {
    await wechatLogin({ ticket, device_id: deviceId.value })
    state.value = 'confirmed'
  } catch (e) {
    const error = e as Error & { code?: string }
    /* 服务端已解绑（如账号停用）：回到首次登录表单重新绑定 */
    boundAccount.value = null
    state.value = 'form'
    message.value = error.code === 'NO_ACCOUNT'
      ? '本机微信绑定已失效，请重新使用手机号验证登录'
      : (error.message || '确认失败，请刷新二维码后重试')
  } finally { busy.value = false }
}

async function cancel() {
  busy.value = true
  try {
    await postQrAction('cancel', ticket)
    state.value = 'cancelled'
  } catch (e) {
    message.value = (e as Error).message || '操作失败'
  } finally { busy.value = false }
}
</script>

<template>
  <div class="qr-page">
    <div class="qr-card">
      <div class="qr-brand"><span class="qr-brand-mark" :style="{ background: color }">智</span>一智万商</div>

      <template v-if="state === 'loading'">
        <div class="qr-state-icon">···</div>
        <h1>正在读取二维码…</h1>
        <p>请稍候</p>
      </template>

      <!-- 已绑定微信身份：以后扫码只需一键确认 -->
      <template v-else-if="state === 'bound'">
        <div class="qr-state-icon" :style="{ color }">▣</div>
        <h1>微信扫码登录</h1>
        <p v-if="boundAccount" class="qr-account">
          <strong>{{ boundAccount.user_name || maskPhone(boundAccount.phone) }}</strong> · {{ boundAccount.tenant_name }}
          <small>{{ maskPhone(boundAccount.phone) }} · 厂标识 {{ boundAccount.tenant_code }}</small>
        </p>
        <p class="qr-note">本机微信已绑定该账号，确认后电脑端将自动登录，无需密码和验证码。</p>
        <p v-if="message" class="qr-msg">{{ message }}</p>
        <button class="qr-primary" :style="{ background: color }" :disabled="busy" @click="confirmBound">
          {{ busy ? '正在登录…' : '确认登录' }}
        </button>
        <button class="qr-cancel" :disabled="busy" @click="cancel">取消登录</button>
      </template>

      <!-- 首次微信登录：手机号 + 验证码（+ 工厂首次密码） -->
      <template v-else-if="state === 'form'">
        <div class="qr-state-icon" :style="{ color }">▣</div>
        <h1>首次微信登录</h1>
        <p>正在登录电脑端的 <strong :style="{ color }">{{ appName }}</strong>，请先验证手机号。</p>
        <div class="qr-form">
          <input v-model="phone" type="tel" inputmode="numeric" maxlength="11" placeholder="手机号" />
          <div class="qr-code-row">
            <input v-model="code" type="text" inputmode="numeric" maxlength="6" placeholder="6 位短信验证码" />
            <button type="button" :disabled="sending || resendLeft > 0" @click="sendCode">
              {{ sending ? '发送中…' : resendLeft > 0 ? resendLeft + 's' : '获取验证码' }}
            </button>
          </div>
          <p v-if="demoCode" class="qr-demo-code">演示验证码：<strong>{{ demoCode }}</strong>（{{ countdown || '已过期' }}）</p>
          <input v-model="shortCode" type="text" maxlength="5" placeholder="厂标识（可选，5 位）" />
          <input v-model="password" type="password" autocomplete="current-password"
            :placeholder="passwordNeeded ? '厂管理员提供的密码（必填）' : '首次登录密码（普通员工必填，管理员可留空）'" />
        </div>
        <p v-if="message" class="qr-msg">{{ message }}</p>
        <p class="qr-note">普通员工首次微信登录需输入厂管理员提供的密码 + 本人手机验证码；成功后本机微信自动绑定，以后扫码即可直接登录。</p>
        <button class="qr-primary" :style="{ background: color }" :disabled="busy" @click="submitFirstLogin">
          {{ busy ? '正在登录…' : '验证并登录' }}
        </button>
        <button class="qr-cancel" :disabled="busy" @click="cancel">取消登录</button>
      </template>

      <template v-else-if="state === 'confirmed'">
        <div class="qr-state-icon ok">✓</div>
        <h1>已确认登录</h1>
        <p>请回到电脑端继续操作，此页面可以关闭。</p>
      </template>

      <template v-else-if="state === 'cancelled'">
        <div class="qr-state-icon warn">✕</div>
        <h1>已取消登录</h1>
        <p>请在电脑端刷新二维码后重新扫描。</p>
      </template>

      <template v-else-if="state === 'expired'">
        <div class="qr-state-icon warn">!</div>
        <h1>二维码已过期</h1>
        <p>二维码有效期为 5 分钟，请在电脑端刷新后重新扫描。</p>
      </template>

      <template v-else>
        <div class="qr-state-icon warn">!</div>
        <h1>无法完成扫码登录</h1>
        <p>{{ message }}</p>
      </template>

      <div class="qr-foot">一智万商 · 企业 AI 经营平台</div>
    </div>
  </div>
</template>

<style scoped>
.qr-page{min-height:100vh;background:linear-gradient(160deg,#eaf3ff,#f5f9ff 55%,#eef8fc);
  display:grid;place-items:center;padding:20px;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;color:#17255f}
.qr-card{width:min(420px,100%);background:#fff;border-radius:20px;padding:34px 28px 24px;text-align:center;
  box-shadow:0 18px 48px rgba(23,37,95,.12)}
.qr-brand{display:flex;align-items:center;justify-content:center;gap:9px;font-weight:800;font-size:17px;margin-bottom:30px}
.qr-brand-mark{display:grid;place-items:center;width:30px;height:30px;border-radius:9px;color:#fff;font-size:14px}
.qr-state-icon{width:72px;height:72px;margin:0 auto 18px;border-radius:22px;background:#eff6ff;color:#2563eb;
  display:grid;place-items:center;font-size:34px;font-weight:700}
.qr-state-icon.ok{background:#ecfdf3;color:#039855}
.qr-state-icon.warn{background:#fef3f2;color:#d92d20}
.qr-card h1{font-size:23px;margin:0 0 12px}
.qr-card p{font-size:14.5px;line-height:1.8;color:#475467;margin:0 0 10px}
.qr-card p strong{font-weight:700}
.qr-note{font-size:12.5px!important;color:#98a2b3!important;margin:0 0 26px!important}
.qr-account{display:flex;flex-direction:column;gap:2px;padding:12px 14px;border-radius:12px;background:#f5f9ff}
.qr-account small{font-size:12px;color:#7b8aa5}
.qr-primary{width:100%;border:0;border-radius:11px;padding:14px;font-size:16px;font-weight:600;color:#fff;cursor:pointer}
.qr-primary:disabled{opacity:.55;cursor:not-allowed}
.qr-cancel{margin-top:14px;border:0;background:transparent;color:#667085;font-size:13.5px;cursor:pointer}
.qr-cancel:hover{color:#2563eb}
.qr-foot{margin-top:32px;padding-top:16px;border-top:1px solid #eef2f7;font-size:11.5px;color:#98a2b3}
.qr-form{display:flex;flex-direction:column;gap:10px;margin:18px 0 14px;text-align:left}
.qr-form input{width:100%;border:1px solid #d7e1f0;border-radius:10px;padding:12px 13px;font-size:14.5px;
  font-family:inherit;color:#17255f;outline:none;box-sizing:border-box}
.qr-form input:focus{border-color:#2563eb;box-shadow:0 0 0 3px rgba(37,99,235,.12)}
.qr-code-row{display:flex;gap:8px}
.qr-code-row input{flex:1}
.qr-code-row button{flex-shrink:0;border:1px solid #cfe0f8;border-radius:10px;padding:0 12px;font-size:13px;
  color:#2563eb;background:#f2f7ff;cursor:pointer;font-family:inherit}
.qr-code-row button:disabled{color:#98a2b3;background:#f5f7fa;border-color:#e4e9f2;cursor:not-allowed}
.qr-demo-code{margin:0!important;font-size:12.5px!important;color:#0f766e!important}
.qr-msg{font-size:13px!important;color:#d92d20!important;margin:0 0 12px!important}
@media(max-width:420px){.qr-card{padding:28px 20px 20px}}
</style>
