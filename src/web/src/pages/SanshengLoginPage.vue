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
  <div class="sw-login">
    <NetworkBackground theme="night" />
    <div class="sw-inner">
      <section class="sw-hero">
        <RouterLink to="/portal" class="sw-back">← 返回门户</RouterLink>
        <h1 class="sw-title">三生万</h1>
        <p class="sw-en">Business Model Intelligence</p>
        <p class="sw-slogan">经营分析模型</p>
        <p class="sw-company">南宁一智万商科技有限责任公司</p>
      </section>
      <section class="sw-card-wrap">
        <form class="sw-card" @submit.prevent="mode === 'account' ? login() : applyTrial()">
          <div class="sw-inner-card">
          <div class="sw-tabs">
            <button type="button" :class="{ active: mode === 'account' }" @click="mode = 'account'">注册用户登录</button>
            <button type="button" :class="{ active: mode === 'trial' }" @click="mode = 'trial'">体验用户登录</button>
          </div>
          <template v-if="mode === 'account'">
            <div v-if="error" class="sw-error">{{ error }}</div>
            <div class="sw-field">
              <input v-model.trim="form.login_name" autocomplete="username" placeholder="账号" />
            </div>
            <div class="sw-field sw-pwd">
              <input v-model="form.password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="密码" />
              <button type="button" class="sw-eye" @click="showPassword = !showPassword">{{ showPassword ? '隐藏' : '显示' }}</button>
            </div>
            <button class="sw-submit" :disabled="loading">{{ loading ? '正在登录…' : '登 录' }}</button>
            <div class="sw-links">
              <span class="sw-muted">忘记密码</span>
              <RouterLink to="/portal#join">注册账号</RouterLink>
            </div>
          </template>
          <template v-else-if="mode === 'trial'">
            <p class="sw-trial-tip">手机号即可开通<strong>一个月免费体验</strong>，全功能开放，无需绑卡。</p>
            <div class="sw-field">
              <input v-model.trim="trialPhone" type="tel" maxlength="11" inputmode="numeric" placeholder="手机号" />
            </div>
            <p v-if="trialMsg" class="sw-trial-msg" :class="{ ok: trialOk }">{{ trialMsg }}</p>
            <button class="sw-submit" :disabled="trialLoading">{{ trialLoading ? '正在提交…' : '立即免费体验' }}</button>
          </template>
          <template v-else>
            <div class="sw-field">
              <input v-model.trim="smsPhone" type="tel" maxlength="11" inputmode="numeric" placeholder="手机号" />
            </div>
            <div class="sw-field sw-code">
              <input v-model.trim="smsCode" maxlength="6" inputmode="numeric" placeholder="验证码" @keyup.enter="smsLogin" />
              <button type="button" class="sw-code-btn" :disabled="codeCountdown > 0" @click="sendSmsCode">
                {{ codeCountdown > 0 ? codeCountdown + 's 后重发' : '获取验证码' }}
              </button>
            </div>
            <p v-if="smsMsg" class="sw-trial-msg">{{ smsMsg }}</p>
            <button class="sw-submit" :disabled="smsLoading" @click="smsLogin">{{ smsLoading ? '正在登录…' : '登 录' }}</button>
            <div class="sw-links"><span class="sw-muted" style="cursor:pointer" @click="mode = 'account'">返回账号登录</span></div>
          </template>
          <!-- 其他登录方式 -->
          <div class="sw-other">
            <div class="sw-other-divider"><span>其他登录方式</span></div>
            <div class="sw-other-btns">
              <button type="button" class="sw-wechat" @click="showWechat = true">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9.2 4C5 4 1.7 6.7 1.7 10c0 1.9 1 3.5 2.7 4.6l-.7 2.1 2.4-1.3c.9.3 1.9.4 2.9.4h.3c-.2-.5-.3-1.1-.3-1.7 0-3.1 3-5.6 6.8-5.6h.3C15.6 6.1 12.7 4 9.2 4Zm-2.3 3.4a.9.9 0 1 1 0 1.8.9.9 0 0 1 0-1.8Zm4.6 0a.9.9 0 1 1 0 1.8.9.9 0 0 1 0-1.8ZM14.6 10c-3 0-5.5 2-5.5 4.5S11.6 19 14.6 19c.8 0 1.5-.2 2.2-.4l2 1-.6-1.8c1.4-.9 2.3-2.2 2.3-3.8 0-2.5-2.4-4.5-5.5-4.5Zm-1.9 2.2a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6Zm3.8 0a.8.8 0 1 1 0 1.6.8.8 0 0 1 0-1.6Z"/></svg>
                <span>微信扫码</span>
              </button>
              <button type="button" class="sw-sms" @click="mode = 'sms'">
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
    <div v-if="showWechat" class="sw-overlay" @click.self="showWechat = false">
      <div class="sw-qrbox">
        <button type="button" class="sw-qr-close" aria-label="关闭" @click="showWechat = false">✕</button>
        <h3>微信扫码登录</h3>
        <div class="sw-qr">
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
        <p class="sw-qr-note">扫码确认后自动进入系统<br />（需接入微信开放平台登录，当前为演示占位）</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sw-login{min-height:100vh;display:flex;align-items:center;position:relative;overflow:hidden;
  background:#1a0e3a;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif}
.sw-inner{position:relative;z-index:1;max-width:1100px;margin:0 auto;width:100%;padding:40px 24px;display:grid;grid-template-columns:1fr 1fr;gap:60px;align-items:center}
.sw-hero{color:#fff}
.sw-back{display:inline-block;font-size:13px;color:rgba(255,255,255,.55);margin-bottom:36px}
.sw-back:hover{color:#fff}
.sw-title{font-size:56px;margin:0 0 10px;letter-spacing:.04em;
  background:linear-gradient(95deg,#c4b5fd 0%,#a78bfa 45%,#f0c868 110%);-webkit-background-clip:text;background-clip:text;color:transparent}
.sw-en{font-size:15px;color:rgba(196,181,253,.75);letter-spacing:.04em;margin:0 0 26px}
.sw-slogan{font-size:22px;color:#fff;margin:0 0 40px}
.sw-company{font-size:13px;color:rgba(255,255,255,.45);margin:0}
.sw-card-wrap{display:flex;justify-content:center}
.sw-card{width:min(430px,100%);background:rgba(255,255,255,.3);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);
  border:1px solid rgba(255,255,255,.55);border-radius:20px;padding:15px;
  box-shadow:0 0 28px rgba(167,139,250,.35),0 24px 60px rgba(0,0,0,.45)}
.sw-inner-card{background:rgba(255,255,255,.7);border-radius:14px;padding:24px}
.sw-card h2{font-size:22px;color:#1e1245;margin:0 0 22px}
.sw-tabs{display:flex;border-bottom:1px solid #ece7f8;margin-bottom:22px}
.sw-tabs button{flex:1;border:0;background:transparent;padding:12px 4px;font-size:15px;font-weight:600;color:#9a8fc0;cursor:pointer;border-bottom:2.5px solid transparent;transition:.15s}
.sw-tabs button:hover{color:#7c3aed}
.sw-tabs button.active{color:#1e1245;border-bottom-color:#7c3aed}
.sw-trial-tip{font-size:13.5px;color:#8b84ab;line-height:1.8;margin:0 0 18px}
.sw-trial-tip strong{color:#7c3aed}
.sw-trial-msg{font-size:13px;color:#d92d20;margin:-6px 0 12px}
.sw-trial-msg.ok{color:#039855}
/* 短信登录与其他登录方式 */
.sw-code{display:flex;gap:8px}
.sw-code input{flex:1}
.sw-code-btn{border:1px solid #e2ddf3;background:#faf5ff;color:#7c3aed;border-radius:10px;padding:0 14px;font-size:13px;white-space:nowrap;cursor:pointer}
.sw-code-btn:disabled{opacity:.55;cursor:not-allowed}
.sw-other{margin-top:20px}
.sw-other-divider{display:flex;align-items:center;gap:12px;color:#9a8fc0;font-size:12px}
.sw-other-divider::before,.sw-other-divider::after{content:"";flex:1;height:1px;background:#ece7f8}
.sw-other-btns{display:flex;justify-content:center;gap:22px;margin-top:14px}
.sw-other-btns button{display:flex;flex-direction:column;align-items:center;gap:5px;border:1px solid #ece7f8;background:#fff;border-radius:14px;padding:10px 16px;cursor:pointer;font-size:12px;color:#5a5380;transition:.18s}
.sw-other-btns button:hover{box-shadow:0 6px 16px rgba(30,18,69,.14)}
.sw-other-btns svg{width:24px;height:24px;fill:currentColor}
.sw-wechat{color:#07c160}
.sw-sms{color:#7c3aed}
/* 微信扫码弹层 */
.sw-overlay{position:fixed;inset:0;background:rgba(10,5,24,.55);backdrop-filter:blur(4px);display:grid;place-items:center;z-index:100;padding:20px}
.sw-qrbox{position:relative;background:#fff;border-radius:18px;padding:32px 44px 28px;text-align:center;box-shadow:0 24px 60px rgba(0,0,0,.5)}
.sw-qrbox h3{margin:0 0 4px;font-size:19px;color:#1e1245}
.sw-qr{margin:14px auto;width:168px;padding:8px;border:1px solid #f0ecf9;border-radius:12px}
.sw-qrbox p{margin:0 0 4px;font-size:14px;color:#33477c}
.sw-qr-note{font-size:12px!important;color:#9a8fc0!important;line-height:1.7}
.sw-qr-close{position:absolute;top:10px;right:10px;width:30px;height:30px;border:0;background:#f5f2fb;color:#667085;border-radius:50%;cursor:pointer;font-size:14px}
.sw-qr-close:hover{background:#eae4f6}
.sw-error{background:#fef3f2;color:#b42318;border-radius:8px;padding:10px 14px;font-size:13px;margin-bottom:16px}
.sw-field{margin-bottom:16px;position:relative}
.sw-field input{width:100%;border:1px solid #e2ddf3;border-radius:10px;padding:13px 15px;font-size:14.5px;background:#fff;outline:none;transition:.15s}
.sw-field input:focus{border-color:#7c3aed;box-shadow:0 0 0 3px rgba(124,58,237,.12)}
.sw-eye{position:absolute;right:10px;top:50%;transform:translateY(-50%);border:0;background:transparent;color:#9a8fc0;font-size:12.5px;cursor:pointer}
.sw-eye:hover{color:#7c3aed}
.sw-submit{width:100%;border:0;border-radius:10px;padding:13px;font-size:16px;font-weight:600;color:#fff;cursor:pointer;margin-top:6px;
  background:linear-gradient(90deg,#6d28d9,#a855f7);box-shadow:0 10px 26px rgba(124,58,237,.4);transition:.18s}
.sw-submit:hover{filter:brightness(1.08)}
.sw-submit:disabled{opacity:.55;cursor:not-allowed}
.sw-links{display:flex;justify-content:space-between;margin-top:18px;font-size:13px}
.sw-links a{color:#7c3aed;font-weight:600}
.sw-muted{color:#9a8fc0}
@media(max-width:900px){
  .sw-inner{grid-template-columns:1fr;gap:36px}
  .sw-title{font-size:40px}
  .sw-back{margin-bottom:20px}
  .sw-slogan{margin-bottom:20px}
}
</style>
