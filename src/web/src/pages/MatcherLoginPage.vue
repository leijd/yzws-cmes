<script setup lang="ts">
import { onBeforeUnmount, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import NetworkBackground from '../components/NetworkBackground.vue'
import { syncNeedPhone } from '../phoneGate'

const router = useRouter()
const mode = ref<'account' | 'trial' | 'sms'>('account')
const loading = ref(false)
const showPassword = ref(false)
const error = ref('')
const form = reactive({ login_name: '', password: '', node_id: 'web-local' })
async function login() {
  error.value = ''
  if (!form.login_name || !form.password) { error.value = '请填写账号和密码'; return }
  loading.value = true
  try {
    const result = await api<{ account_type: 'tenant' | 'platform'; access_token: string; refresh_token?: string; password_change_required?: boolean; need_contact_phone?: boolean; tenant?: { code?: string } }>('/auth/unified-login', { method: 'POST', body: JSON.stringify(form) })
    localStorage.removeItem('clothp_access_token')
    localStorage.removeItem('clothp_refresh_token')
    localStorage.removeItem('clothp_platform_access_token')
    if (result.account_type === 'platform') {
      localStorage.setItem('clothp_platform_access_token', result.access_token)
      await router.push('/platform/tenants')
      return
    }
    localStorage.setItem('clothp_access_token', result.access_token)
    if (result.refresh_token) localStorage.setItem('clothp_refresh_token', result.refresh_token)
    /* 旧数据租户的注册信息里没有手机号：厂管理员先完善手机号，再进工作台 */
    const needPhone = result.need_contact_phone === true
    syncNeedPhone(needPhone, result.tenant?.code)
    if (result.password_change_required) { await router.push('/change-password'); return }
    if (needPhone) { await router.push('/complete-phone'); return }
    await router.push('/workbench/A03')
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

/* 体验用户登录 */
const trialPhone = ref('')
const trialMsg = ref('')
const trialOk = ref(false)
const trialLoading = ref(false)
async function applyTrial() {
  trialMsg.value = ''
  trialOk.value = false
  if (!/^1\d{10}$/.test(trialPhone.value)) { trialMsg.value = '请输入正确的 11 位手机号'; return }
  trialLoading.value = true
  try {
    // TODO: 接入后端免费体验开通接口 /auth/trial-apply
    await new Promise(r => setTimeout(r, 600))
    trialOk.value = true
    trialMsg.value = '体验申请已提交！客服将尽快为您开通一个月免费体验，请留意短信通知。'
  } finally { trialLoading.value = false }
}

/* 短信登录 / 微信扫码登录（后端接口待接入） */
const smsPhone = ref('')
const smsCode = ref('')
const smsMsg = ref('')
const smsLoading = ref(false)
const codeCountdown = ref(0)
const showWechat = ref(false)
let codeTimer: ReturnType<typeof setInterval> | undefined
async function sendSmsCode() {
  smsMsg.value = ''
  if (!/^1\d{10}$/.test(smsPhone.value)) { smsMsg.value = '请输入正确的 11 位手机号'; return }
  // TODO: 接入短信验证码接口 /auth/sms-code
  codeCountdown.value = 60
  codeTimer && clearInterval(codeTimer)
  codeTimer = setInterval(() => { if (--codeCountdown.value <= 0) { clearInterval(codeTimer); codeCountdown.value = 0 } }, 1000)
  smsMsg.value = '验证码已发送（演示占位，短信网关待接入）'
}
async function smsLogin() {
  smsMsg.value = ''
  if (!smsPhone.value || !smsCode.value) { smsMsg.value = '请填写手机号和验证码'; return }
  smsLoading.value = true
  try {
    // TODO: 接入短信登录接口 /auth/sms-login
    await new Promise(r => setTimeout(r, 600))
    smsMsg.value = '短信登录功能即将上线，演示环境暂不支持。'
  } finally { smsLoading.value = false }
}
onBeforeUnmount(() => codeTimer && clearInterval(codeTimer))
</script>

<template>
  <div class="ch-login">
    <NetworkBackground theme="ocean" />
    <div class="ch-inner">
      <section class="ch-hero">
        <RouterLink to="/portal" class="ch-back">← 返回门户</RouterLink>
        <h1><span class="ch-dark">二</span><span class="ch-grad">撮合</span></h1>
        <p class="ch-en">Order Matching Platform</p>
        <p class="ch-slogan">订单客户与制衣厂双向撮合</p>
        <p class="ch-company">南宁一智万商科技有限责任公司</p>
      </section>
      <section class="ch-card-wrap">
        <form class="ch-card" @submit.prevent="mode === 'account' ? login() : applyTrial()">
          <div class="ch-inner-card">
          <div class="ch-tabs">
            <button type="button" :class="{ active: mode === 'account' }" @click="mode = 'account'">注册用户登录</button>
            <button type="button" :class="{ active: mode === 'trial' }" @click="mode = 'trial'">体验用户登录</button>
          </div>
          <template v-if="mode === 'account'">
            <div v-if="error" class="ch-error">{{ error }}</div>
            <div class="ch-field">
              <input v-model.trim="form.login_name" autocomplete="username" placeholder="账号" />
            </div>
            <div class="ch-field ch-pwd">
              <input v-model="form.password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="密码" />
              <button type="button" class="ch-eye" @click="showPassword = !showPassword">{{ showPassword ? '隐藏' : '显示' }}</button>
            </div>
            <button class="ch-submit" :disabled="loading">{{ loading ? '正在登录…' : '登 录' }}</button>
            <div class="ch-links">
              <span class="ch-muted">忘记密码</span>
              <RouterLink to="/portal#join">注册账号</RouterLink>
            </div>
          </template>
          <template v-else-if="mode === 'trial'">
            <p class="ch-trial-tip">手机号即可开通<strong>一个月免费体验</strong>，全功能开放，无需绑卡。</p>
            <div class="ch-field">
              <input v-model.trim="trialPhone" type="tel" maxlength="11" inputmode="numeric" placeholder="手机号" />
            </div>
            <p v-if="trialMsg" class="ch-trial-msg" :class="{ ok: trialOk }">{{ trialMsg }}</p>
            <button class="ch-submit" :disabled="trialLoading">{{ trialLoading ? '正在提交…' : '立即免费体验' }}</button>
          </template>
          <template v-else>
            <div class="ch-field">
              <input v-model.trim="smsPhone" type="tel" maxlength="11" inputmode="numeric" placeholder="手机号" />
            </div>
            <div class="ch-field ch-code">
              <input v-model.trim="smsCode" maxlength="6" inputmode="numeric" placeholder="验证码" @keyup.enter="smsLogin" />
              <button type="button" class="ch-code-btn" :disabled="codeCountdown > 0" @click="sendSmsCode">
                {{ codeCountdown > 0 ? codeCountdown + 's 后重发' : '获取验证码' }}
              </button>
            </div>
            <p v-if="smsMsg" class="ch-trial-msg">{{ smsMsg }}</p>
            <button class="ch-submit" :disabled="smsLoading" @click="smsLogin">{{ smsLoading ? '正在登录…' : '登 录' }}</button>
            <div class="ch-links"><span class="ch-muted" style="cursor:pointer" @click="mode = 'account'">返回账号登录</span></div>
          </template>
          <!-- 其他登录方式 -->
          <div class="ch-other">
            <div class="ch-other-divider"><span>其他登录方式</span></div>
            <div class="ch-other-btns">
              <button type="button" class="ch-wechat" @click="showWechat = true">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9.2 4C5 4 1.7 6.7 1.7 10c0 1.9 1 3.5 2.7 4.6l-.7 2.1 2.4-1.3c.9.3 1.9.4 2.9.4h.3c-.2-.5-.3-1.1-.3-1.7 0-3.1 3-5.6 6.8-5.6h.3C15.6 6.1 12.7 4 9.2 4Zm-2.3 3.4a.9.9 0 1 1 0 1.8.9.9 0 0 1 0-1.8Zm4.6 0a.9.9 0 1 1 0 1.8.9.9 0 0 1 0-1.8ZM14.6 10c-3 0-5.5 2-5.5 4.5S11.6 19 14.6 19c.8 0 1.5-.2 2.2-.4l2 1-.6-1.8c1.4-.9 2.3-2.2 2.3-3.8 0-2.5-2.4-4.5-5.5-4.5Zm-1.9 2.2a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6Zm3.8 0a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6Z"/></svg>
                <span>微信扫码</span>
              </button>
              <button type="button" class="ch-sms" @click="mode = 'sms'">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 5h16a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H8l-4 3V7a2 2 0 0 1 2-2Zm2 3v8.5l1.2-.9H18V8H6Zm2 2.5h8v1.8H8v-1.8Zm0 3h5v1.8H8v-1.8Z"/></svg>
                <span>短信登录</span>
              </button>
            </div>
          </div>
          </div>
        </form>
      </section>
    </div>

    <!-- 微信扫码登录弹层 -->
    <div v-if="showWechat" class="ch-overlay" @click.self="showWechat = false">
      <div class="ch-qrbox">
        <button type="button" class="ch-qr-close" aria-label="关闭" @click="showWechat = false">✕</button>
        <h3>微信扫码登录</h3>
        <div class="ch-qr">
          <svg viewBox="0 0 60 60" width="150" height="150">
            <rect width="60" height="60" fill="#fff"/>
            <g fill="#1b2a52">
              <rect x="6" y="6" width="14" height="14"/><rect x="40" y="6" width="14" height="14"/><rect x="6" y="40" width="14" height="14"/>
              <rect x="9" y="9" width="8" height="8" fill="#fff"/><rect x="43" y="9" width="8" height="8" fill="#fff"/><rect x="9" y="43" width="8" height="8" fill="#fff"/>
              <rect x="11" y="11" width="4" height="4"/><rect x="45" y="11" width="4" height="4"/><rect x="11" y="45" width="4" height="4"/>
              <rect x="26" y="6" width="4" height="4"/><rect x="26" y="12" width="4" height="10"/><rect x="32" y="16" width="4" height="4"/>
              <rect x="6" y="26" width="8" height="4"/><rect x="18" y="26" width="4" height="8"/><rect x="26" y="26" width="4" height="4"/><rect x="34" y="26" width="12" height="4"/><rect x="50" y="26" width="4" height="8"/>
              <rect x="6" y="34" width="4" height="6"/><rect x="14" y="36" width="6" height="4"/><rect x="26" y="34" width="10" height="4"/><rect x="40" y="34" width="4" height="8"/>
              <rect x="48" y="38" width="6" height="4"/><rect x="26" y="42" width="4" height="12"/><rect x="34" y="46" width="4" height="8"/><rect x="42" y="44" width="6" height="4"/><rect x="52" y="46" width="4" height="8"/>
            </g>
          </svg>
        </div>
        <p>请使用微信「扫一扫」</p>
        <p class="ch-qr-note">扫码确认后自动进入系统<br />（需接入微信开放平台登录，当前为演示占位）</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ch-login{min-height:100vh;display:flex;align-items:center;position:relative;overflow:hidden;
  background:#0f68c9;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif}
.ch-inner{position:relative;z-index:1;max-width:1100px;margin:0 auto;width:100%;padding:40px 24px;display:grid;grid-template-columns:1fr 1fr;gap:60px;align-items:center}
.ch-hero{color:#fff}
.ch-back{display:inline-block;font-size:13px;color:rgba(255,255,255,.75);margin-bottom:36px}
.ch-back:hover{color:#fff}
.ch-hero h1{font-size:56px;margin:0 0 10px;letter-spacing:.02em;color:#fff}
.ch-dark{color:#e8f4ff}
.ch-grad{background:linear-gradient(90deg,#a7f3d0,#34d399);-webkit-background-clip:text;background-clip:text;color:transparent}
.ch-en{font-size:15px;color:rgba(255,255,255,.75);letter-spacing:.04em;margin:0 0 26px}
.ch-slogan{font-size:19px;color:rgba(255,255,255,.95);margin:0 0 40px}
.ch-company{font-size:13px;color:rgba(255,255,255,.6);margin:0}
.ch-card-wrap{display:flex;justify-content:center}
.ch-card{width:min(430px,100%);background:rgba(255,255,255,.3);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);
  border:1px solid rgba(255,255,255,.55);border-radius:20px;padding:15px;
  box-shadow:0 0 26px rgba(255,255,255,.3),0 20px 50px rgba(6,40,80,.35)}
.ch-inner-card{background:rgba(255,255,255,.7);border-radius:14px;padding:24px}
.ch-card h2{font-size:22px;color:#17255f;margin:0 0 22px}
.ch-tabs{display:flex;border-bottom:1px solid #e3ecfb;margin-bottom:22px}
.ch-tabs button{flex:1;border:0;background:transparent;padding:12px 4px;font-size:15px;font-weight:600;color:#8b9ac0;cursor:pointer;border-bottom:2.5px solid transparent;transition:.15s}
.ch-tabs button:hover{color:#0d9488}
.ch-tabs button.active{color:#17255f;border-bottom-color:#0ea5a4}
.ch-trial-tip{font-size:13.5px;color:#7b8ab0;line-height:1.8;margin:0 0 18px}
.ch-trial-tip strong{color:#0d9488}
.ch-trial-msg{font-size:13px;color:#d92d20;margin:-6px 0 12px}
.ch-trial-msg.ok{color:#039855}
/* 短信登录与其他登录方式 */
.ch-code{display:flex;gap:8px}
.ch-code input{flex:1}
.ch-code-btn{border:1px solid #dbe4f3;background:#f0fdfa;color:#0d9488;border-radius:10px;padding:0 14px;font-size:13px;white-space:nowrap;cursor:pointer}
.ch-code-btn:disabled{opacity:.55;cursor:not-allowed}
.ch-other{margin-top:20px}
.ch-other-divider{display:flex;align-items:center;gap:12px;color:#8b9ac0;font-size:12px}
.ch-other-divider::before,.ch-other-divider::after{content:"";flex:1;height:1px;background:#e3ecfb}
.ch-other-btns{display:flex;justify-content:center;gap:22px;margin-top:14px}
.ch-other-btns button{display:flex;flex-direction:column;align-items:center;gap:5px;border:1px solid #e3ecfb;background:#fff;border-radius:14px;padding:10px 16px;cursor:pointer;font-size:12px;color:#41537e;transition:.18s}
.ch-other-btns button:hover{box-shadow:0 6px 16px rgba(23,37,95,.12)}
.ch-other-btns svg{width:24px;height:24px;fill:currentColor}
.ch-wechat{color:#07c160}
.ch-sms{color:#0d9488}
/* 微信扫码弹层 */
.ch-overlay{position:fixed;inset:0;background:rgba(6,20,40,.5);backdrop-filter:blur(4px);display:grid;place-items:center;z-index:100;padding:20px}
.ch-qrbox{position:relative;background:#fff;border-radius:18px;padding:32px 44px 28px;text-align:center;box-shadow:0 24px 60px rgba(6,20,40,.45)}
.ch-qrbox h3{margin:0 0 4px;font-size:19px;color:#17255f}
.ch-qr{margin:14px auto;width:168px;padding:8px;border:1px solid #eef2fa;border-radius:12px}
.ch-qrbox p{margin:0 0 4px;font-size:14px;color:#33477c}
.ch-qr-note{font-size:12px!important;color:#8b9ac0!important;line-height:1.7}
.ch-qr-close{position:absolute;top:10px;right:10px;width:30px;height:30px;border:0;background:#f2f5fb;color:#667085;border-radius:50%;cursor:pointer;font-size:14px}
.ch-qr-close:hover{background:#e7edf7}
.ch-error{background:#fef3f2;color:#b42318;border-radius:8px;padding:10px 14px;font-size:13px;margin-bottom:16px}
.ch-field{margin-bottom:16px;position:relative}
.ch-field input{width:100%;border:1px solid #dbe4f3;border-radius:10px;padding:13px 15px;font-size:14.5px;background:#fff;outline:none;transition:.15s}
.ch-field input:focus{border-color:#0ea5a4;box-shadow:0 0 0 3px rgba(14,165,164,.14)}
.ch-eye{position:absolute;right:10px;top:50%;transform:translateY(-50%);border:0;background:transparent;color:#8b9ac0;font-size:12.5px;cursor:pointer}
.ch-eye:hover{color:#0ea5a4}
.ch-submit{width:100%;border:0;border-radius:10px;padding:13px;font-size:16px;font-weight:600;color:#fff;cursor:pointer;margin-top:6px;
  background:linear-gradient(90deg,#2563eb,#22c55e);box-shadow:0 10px 24px rgba(14,116,144,.35);transition:.18s}
.ch-submit:hover{filter:brightness(1.07)}
.ch-submit:disabled{opacity:.55;cursor:not-allowed}
.ch-links{display:flex;justify-content:space-between;margin-top:18px;font-size:13px}
.ch-links a{color:#0d9488;font-weight:600}
.ch-muted{color:#8b9ac0}
@media(max-width:900px){
  .ch-inner{grid-template-columns:1fr;gap:36px}
  .ch-hero h1{font-size:40px}
  .ch-back{margin-bottom:20px}
  .ch-slogan{margin-bottom:20px}
}
</style>
