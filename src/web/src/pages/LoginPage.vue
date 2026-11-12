<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import NetworkBackground from '../components/NetworkBackground.vue'
import {
  TRIAL_RESEND_MS, clearTrialSession, formatCountdown, isValidPhone, maskPhone,
  phoneLogin, saveTrialSession, sendTrialCode, verifyTrialCode, workspaceHome,
  type TrialTenantEntry, type TrialVia,
} from '../trial'
import { createLoginQr, exchangeQrLogin, getQrStatus, makeQrImage, moduleLoginUrl, qrLoginUrl } from '../qrLogin'
import { syncNeedPhone } from '../phoneGate'

const router = useRouter()
const route = useRoute()

type Mode = 'account' | 'register' | 'sms' | 'select-tenant'
/* 默认手机短信登录（需求口径）；记住密码的自动登录不受影响（onMounted 独立触发） */
const mode = ref<Mode>('sms')
const panelTitle = computed(() =>
  mode.value === 'register' ? '极速注册'
    : mode.value === 'sms' ? '短信登录'
    : mode.value === 'select-tenant' ? '选择制衣厂'
    : '账号登录')
const loading = ref(false)
const showPassword = ref(false)
const error = ref('')
const notice = ref('')
const form = reactive({ tenant_code: '', login_name: '', password: '', node_id: 'web-local' })
const TENANT_CODE_KEY = 'clothp_last_tenant_code'

/* ===== 账号密码登录：厂编码 + 工号/手机号（设计 5.6.1，固定错误顺序 5.6.3） =====
 * 工号与手机号均为租户内唯一，必须带厂编码；平台管理员请走 /platform/login。 */
const REMEMBER_KEY = 'clothp_login_remember'
const HISTORY_KEY = 'clothp_login_history'
const AUTO_TRY_KEY = 'clothp_autologin_tried'
const remember = ref(false)
const autoLogin = ref(false)
const history = ref<string[]>([])
const showHistory = ref(false)
const showSettings = ref(false)

function persistCredentials() {
  const list = [form.login_name, ...history.value.filter(name => name !== form.login_name)].slice(0, 5)
  history.value = list
  try { localStorage.setItem(HISTORY_KEY, JSON.stringify(list)) } catch { /* 忽略隐私模式 */ }
  try {
    if (remember.value) {
      localStorage.setItem(REMEMBER_KEY, JSON.stringify({
        login_name: form.login_name, password: form.password, autoLogin: autoLogin.value,
      }))
    } else {
      localStorage.removeItem(REMEMBER_KEY)
    }
  } catch { /* 忽略隐私模式 */ }
}

async function login(silent = false) {
  error.value = ''
  if (!form.tenant_code || !form.login_name || !form.password) {
    if (!silent) error.value = '请填写厂编码、账号和密码'
    return
  }
  loading.value = true
  try {
    const result = await api<{ access_token: string; refresh_token?: string; password_change_required?: boolean; need_contact_phone?: boolean; permissions?: string[]; tenant?: { code: string; name: string; edition?: string; contact_phone?: string } }>(
      '/auth/account-login',
      { method: 'POST', body: JSON.stringify({ tenant_code: form.tenant_code, account: form.login_name, password: form.password, client_type: 'pc', node_id: form.node_id }) },
      silent ? { silent: true } : {},
    )
    persistCredentials()
    localStorage.removeItem('clothp_access_token')
    localStorage.removeItem('clothp_refresh_token')
    localStorage.removeItem('clothp_platform_access_token')
    try { localStorage.setItem(TENANT_CODE_KEY, form.tenant_code) } catch { /* 忽略隐私模式 */ }
    /* 账号登录（注册用户）也清理本机遗留体验会话，避免门户误显示为体验版 */
    clearTrialSession()
    localStorage.setItem('clothp_access_token', result.access_token)
    if (result.refresh_token) localStorage.setItem('clothp_refresh_token', result.refresh_token)
    /* 旧数据租户的注册信息里没有手机号：厂管理员先完善手机号，再进系统 */
    const needPhone = result.need_contact_phone === true
    syncNeedPhone(needPhone, result.tenant?.code)
    if (result.password_change_required) {
      await router.push('/change-password')
      return
    }
    if (needPhone) {
      await router.push('/complete-phone')
      return
    }
    const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/') ? route.query.redirect : ''
    if (redirect) {
      await router.push(redirect)
      return
    }
    const permissionRoutes: Array<[string, string]> = [['workbench:read', '/workbench/A03'], ['work_order:read', '/orders/C03'], ['task:read', '/tasks/D01'], ['report:read', '/reports/D06'], ['payroll:read', '/payroll/F09'], ['progress:read', '/progress/F01'], ['product:read', '/products/B01'], ['masterdata:read', '/masterdata/B06'], ['identity:read', '/identity/G05'], ['settings:read', '/settings/G01']]
    const granted = new Set(result.permissions || [])
    /* 制衣厂用户登录后统一进入本厂门户（体验用户显示“开始体验”，注册用户显示“进入工作”） */
    const tenantCode = result.tenant?.code
    const home = tenantCode ? `/portal/home?tenant=${encodeURIComponent(tenantCode)}`
      : (permissionRoutes.find(([permission]) => granted.has(permission))?.[1] || '/identity/G05')
    await router.push(home)
  } catch (e) {
    /* 自动登录失败不打扰用户 */
    if (!silent) error.value = (e as Error).message
  } finally { loading.value = false }
}

function chooseHistory(name: string) {
  form.login_name = name
  showHistory.value = false
}
function clearSaved() {
  try {
    localStorage.removeItem(REMEMBER_KEY)
    localStorage.removeItem(HISTORY_KEY)
  } catch { /* 忽略隐私模式 */ }
  history.value = []
  remember.value = false
  autoLogin.value = false
  form.login_name = ''
  form.password = ''
  showSettings.value = false
  notice.value = '已清除本机保存的账号与密码'
}

/* ===== 多途径：极速注册 / 短信登录共用验证码流程 ===== */
const phone = ref('')
const code = ref('')
const demoCode = ref('')
/* 普通员工首次短信登录需要厂管理员提供的密码 */
const smsPassword = ref('')
const needFactoryPassword = ref(false)
const expiresAt = ref(0)
const resendAt = ref(0)
const nowTs = ref(Date.now())
const flowMsg = ref('')
const flowOk = ref(false)
const flowLoading = ref(false)
let flowTimer: number | undefined

const codeMsLeft = computed(() => Math.max(0, expiresAt.value - nowTs.value))
const codeSeconds = computed(() => Math.ceil(codeMsLeft.value / 1000))
const resendSeconds = computed(() => Math.max(0, Math.ceil((resendAt.value - nowTs.value) / 1000)))
const codeCountdown = computed(() => formatCountdown(codeMsLeft.value))

function enterDemoHome(phoneValue: string, via: TrialVia = 'sms', displayName = '', app = '') {
  saveTrialSession({
    phone: phoneValue,
    ticket: `${via === 'guest' ? 'guest' : via === 'qr' ? 'qr' : 'demo'}-${Date.now()}-${Math.random().toString(36).slice(2)}`,
    expiresAt: Date.now() + 30 * 60 * 1000,
    displayName: displayName || undefined,
    via,
  })
  const query = app ? `?app=${app}&via=${via}` : via === 'guest' ? '?via=guest' : ''
  void router.push(`/trial/home${query}`)
}

function switchMode(next: Mode) {
  mode.value = next
  error.value = ''
  notice.value = ''
  flowMsg.value = ''
  needFactoryPassword.value = false
  smsPassword.value = ''
  showHistory.value = false
  showSettings.value = false
}

async function sendCode() {
  flowMsg.value = ''
  flowOk.value = false
  if (!isValidPhone(phone.value)) { flowMsg.value = '请输入正确的 11 位手机号'; return }
  if (resendSeconds.value > 0) { flowMsg.value = `请 ${resendSeconds.value} 秒后再获取验证码`; return }
  flowLoading.value = true
  try {
    await new Promise(r => setTimeout(r, 350))
    const result = await sendTrialCode(phone.value)
    expiresAt.value = result.expiresAt
    resendAt.value = Date.now() + TRIAL_RESEND_MS
    demoCode.value = result.demoCode
    code.value = ''
    nowTs.value = Date.now()
    flowOk.value = true
    flowMsg.value = result.smsSent
      ? `验证码已发送至 ${maskPhone(phone.value)}，3 分钟内有效`
      : '演示模式：请查看下方演示验证码，3 分钟内有效'
  } catch (e) {
    flowMsg.value = (e as Error).message || '验证码发送失败，请稍后重试'
  } finally { flowLoading.value = false }
}

/* ===== 手机号选厂：一个手机号最多占用一个体验厂，但可绑定多个制衣厂；多厂时弹出选择 ===== */
const tenantChoices = ref<TrialTenantEntry[]>([])
const pendingTicket = ref('')
const pickingTenant = ref(false)

/** 选中一个厂：体验厂直接进体验主页；制衣厂凭 ticket 换真实会话进入该用户的主页 */
async function enterByEntry(entry: TrialTenantEntry, ticket: string) {
  if (entry.kind === 'trial') {
    saveTrialSession({
      phone: phone.value,
      ticket: ticket || `sms-${Date.now()}`,
      expiresAt: entry.trial_ends_at ? Math.floor(entry.trial_ends_at / 1000) : Date.now() + 30 * 60 * 1000,
      via: 'sms',
      displayName: entry.name,
      tenantCode: entry.code,
      trialName: entry.name,
      adminLogin: entry.admin_login_name || phone.value,
      entryToken: entry.entry_token || ticket || undefined,
      status: entry.registration_status || entry.status || 'approved',
    })
    /* 带 enter=1 回体验主页：主页挂载后自动点“开始体验”进入工作台 */
    void router.push(route.query.enter === '1' ? '/trial/home?enter=1' : '/trial/home')
    return
  }
  pickingTenant.value = true
  try {
    const result = await phoneLogin(ticket, entry.code, smsPassword.value)
    /* 以注册用户身份进入收费厂：清掉本机遗留的体验会话，避免门户误显示成体验版 */
    clearTrialSession()
    localStorage.removeItem('clothp_platform_access_token')
    localStorage.setItem('clothp_access_token', result.access_token)
    if (result.refresh_token) localStorage.setItem('clothp_refresh_token', result.refresh_token)
    /* 旧数据租户未登记注册手机号：厂管理员先完善手机号再进门户 */
    syncNeedPhone(result.need_contact_phone, entry.code)
    if (result.need_contact_phone) {
      await router.push('/complete-phone')
      return
    }
    const granted = new Set(result.permissions || [])
    const permissionRoutes: Array<[string, string]> = [['workbench:read', '/workbench/A03'], ['work_order:read', '/orders/C03'], ['task:read', '/tasks/D01'], ['report:read', '/reports/D06'], ['payroll:read', '/payroll/F09'], ['progress:read', '/progress/F01'], ['product:read', '/products/B01'], ['masterdata:read', '/masterdata/B06'], ['identity:read', '/identity/G05'], ['settings:read', '/settings/G01']]
    /* 手机号+制衣厂登录：同样先进入本厂门户（注册用户按钮为“进入工作”） */
    const home = `/portal/home?tenant=${encodeURIComponent(entry.code)}`
    void permissionRoutes
    void granted
    await router.push(result.password_change_required ? '/change-password' : home)
  } catch (e) {
    const error = e as Error & { code?: string }
    if (error.code === 'PASSWORD_REQUIRED') {
      /* 首次短信登录：保留选厂状态与 ticket，补填厂管理员提供的密码后重试 */
      needFactoryPassword.value = true
      tenantChoices.value = [entry]
      pendingTicket.value = ticket
      mode.value = 'select-tenant'
      flowMsg.value = error.message
    } else {
      flowMsg.value = error.message || '登录失败，请稍后重试'
      mode.value = tenantChoices.value.length ? 'select-tenant' : 'sms'
    }
  } finally { pickingTenant.value = false }
}

async function verifyCode() {
  flowMsg.value = ''
  flowOk.value = false
  if (!/^\d{6}$/.test(code.value)) { flowMsg.value = '请输入 6 位数字验证码'; return }
  if (codeSeconds.value <= 0) {
    expiresAt.value = 0
    demoCode.value = ''
    flowMsg.value = '验证码已超过 3 分钟有效期，请重新获取'
    return
  }
  flowLoading.value = true
  try {
    const result = await verifyTrialCode(phone.value, code.value, demoCode.value)
    if (!result.ok) { flowMsg.value = result.message || '验证码不正确'; return }
    /* 记录手机号码：该手机号关联的所有厂（体验厂 + 制衣厂绑定账号） */
    const entries: TrialTenantEntry[] = result.tenants?.length
      ? result.tenants
      : (result.tenant
        ? [{ kind: 'trial', code: result.tenant.code, name: result.tenant.name, short_code: result.tenant.short_code,
            admin_login_name: result.tenant.admin_login_name, trial_ends_at: result.tenant.trial_ends_at }]
        : [])
    if (entries.length === 1) {
      await enterByEntry(entries[0], result.ticket || '')
      return
    }
    if (entries.length > 1) {
      pendingTicket.value = result.ticket || ''
      tenantChoices.value = entries
      mode.value = 'select-tenant'
      return
    }
    enterDemoHome(phone.value, 'sms')
  } finally { flowLoading.value = false }
}

function submit() {
  if (mode.value === 'account') void login()
  else void verifyCode()
}

/* ===== 扫码 / 游客 / 第三方 ===== */
const showQr = ref(false)
const qrImage = ref('')
const qrTarget = ref('')
const qrState = ref<'loading' | 'ready' | 'scanned' | 'confirmed' | 'expired' | 'cancelled' | 'offline'>('loading')
const qrHint = ref('')
let qrTicket = ''
let qrTimer: number | undefined

function stopQrPolling() {
  if (qrTimer) { window.clearInterval(qrTimer); qrTimer = undefined }
}

async function pollQr() {
  if (!qrTicket) return
  try {
    const result = await getQrStatus(qrTicket)
    if (result.status === 'confirmed') {
      stopQrPolling()
      qrState.value = 'confirmed'
      showQr.value = false
      /* 手机端已完成真实身份校验：一次性取回 JWT 并按权限进入工作台 */
      if (result.login_ticket) {
        try {
          const session = await exchangeQrLogin(result.login_ticket)
          localStorage.removeItem('clothp_platform_access_token')
          localStorage.setItem('clothp_access_token', session.access_token)
          if (session.refresh_token) localStorage.setItem('clothp_refresh_token', session.refresh_token)
          await router.push(session.password_change_required ? '/change-password' : workspaceHome(session.permissions || []))
          return
        } catch (e) {
          qrHint.value = (e as Error).message || '扫码登录结果已失效，请刷新二维码后重新扫码'
          qrState.value = 'expired'
          return
        }
      }
      /* 未接入真实账号时的演示降级 */
      enterDemoHome('扫码用户', 'qr', '扫码用户', 'zhisuan')
      return
    }
    if (result.status === 'expired') { qrState.value = 'expired'; stopQrPolling(); return }
    if (result.status === 'cancelled') { qrState.value = 'cancelled'; stopQrPolling(); return }
    if (result.status === 'scanned') qrState.value = 'scanned'
  } catch { /* 网络抖动，下一轮继续 */ }
}

async function openQr() {
  showQr.value = true
  qrState.value = 'loading'
  qrHint.value = ''
  qrImage.value = ''
  qrTicket = ''
  stopQrPolling()
  let url: string
  try {
    const result = await createLoginQr('zhisuan')
    qrTicket = result.ticket
    url = qrLoginUrl(result.ticket, 'zhisuan')
    qrState.value = 'ready'
  } catch {
    url = moduleLoginUrl('zhisuan')
    qrState.value = 'offline'
    qrHint.value = '二维码服务未启动，扫码将打开一智算登录页'
  }
  qrTarget.value = url
  qrImage.value = await makeQrImage(url, '#1d4ed8', 720)
  if (showQr.value && qrTicket) qrTimer = window.setInterval(pollQr, 1200)
}

function closeQr() {
  showQr.value = false
  stopQrPolling()
}

function guestLogin() { enterDemoHome('游客用户', 'guest', '游客') }
function thirdParty(name: string) {
  notice.value = `${name}登录需要接入开放平台授权，当前为演示占位`
}
function forgot() { notice.value = '忘记账号或密码请联系厂内管理员重置' }
function offline() { notice.value = '脱机模式待接入（本地离线数据同步开发中）' }
function serverSettings() {
  showSettings.value = false
  notice.value = '服务器设置待接入（当前使用本机默认节点）'
}

onMounted(() => {
  /* 体验主页“开始体验”凭证失效时跳回这里：预填手机号并切到短信登录，验证通过后自动回体验主页 */
  if (route.query.mode === 'sms') {
    mode.value = 'sms'
    const qPhone = typeof route.query.phone === 'string' ? route.query.phone : ''
    if (isValidPhone(qPhone)) phone.value = qPhone
  }
  try {
    form.tenant_code = localStorage.getItem(TENANT_CODE_KEY) || ''
    const saved = JSON.parse(localStorage.getItem(REMEMBER_KEY) || 'null')
    if (saved) {
      form.login_name = saved.login_name || ''
      if (saved.password) { form.password = saved.password; remember.value = true }
      autoLogin.value = !!saved.autoLogin
    }
    const list = JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]')
    if (Array.isArray(list)) history.value = list.filter(item => typeof item === 'string').slice(0, 5)
  } catch { /* 配置损坏时忽略 */ }

  /* 自动登录只尝试一次，避免 401 重定向后反复重试 */
  if (autoLogin.value && form.login_name && form.password && sessionStorage.getItem(AUTO_TRY_KEY) !== '1') {
    sessionStorage.setItem(AUTO_TRY_KEY, '1')
    void login(true)
  }
  nowTs.value = Date.now()
  flowTimer = window.setInterval(() => {
    nowTs.value = Date.now()
    if (mode.value !== 'account' && expiresAt.value > 0 && expiresAt.value <= nowTs.value) {
      expiresAt.value = 0
      demoCode.value = ''
      code.value = ''
      flowMsg.value = '验证码已超过 3 分钟有效期，请重新获取'
    }
  }, 1000)
})
onBeforeUnmount(() => { if (flowTimer) window.clearInterval(flowTimer); stopQrPolling() })
</script>

<template>
  <div class="zs-login">
    <NetworkBackground theme="aurora" />
    <div class="zs-inner">
      <section class="zs-hero">
        <RouterLink to="/portal" class="zs-back">← 返回门户</RouterLink>
        <h1><span class="zs-dark">一智</span><span class="zs-grad">算</span></h1>
        <p class="zs-en">AI Accounting &amp; Settlement</p>
        <p class="zs-slogan">算账结算省心</p>
        <p class="zs-desc">贯通产品、工单、派工、报工、审批和工资，用一致的数据口径连接管理端与生产现场。</p>
        <p class="zs-company">南宁一智万商科技有限责任公司</p>
      </section>

      <section class="zs-card-wrap">
        <form class="zs-card" @submit.prevent="submit">
          <div class="zs-inner-card">
            <div class="zs-panel-grid">
              <!-- 左侧：主登录表单 -->
              <div class="zs-main">
                <div class="zs-panel-head">
                  <h2>{{ panelTitle }}</h2>
                  <button v-if="mode !== 'account'" type="button" class="zs-back-account" @click="switchMode('account')">← 账号登录</button>
                </div>

                <template v-if="mode === 'account'">
                  <div v-if="error" class="zs-error">{{ error }}</div>
                  <div class="zs-field zs-with-icon">
                    <span class="zs-field-icon" aria-hidden="true">
                      <svg viewBox="0 0 24 24"><path d="M3 21h18M5 21V7l7-4 7 4v14M9 21v-6h6v6"/></svg>
                    </span>
                    <input v-model.trim="form.tenant_code" autocomplete="organization" placeholder="厂编码（如 YZWS001）" />
                  </div>
                  <div class="zs-field zs-with-icon">
                    <span class="zs-field-icon" aria-hidden="true">
                      <svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="3.4"/><path d="M5 20c0-3.4 3.1-5.6 7-5.6s7 2.2 7 5.6"/></svg>
                    </span>
                    <input v-model.trim="form.login_name" autocomplete="username" placeholder="账号 / 手机号" />
                    <button type="button" class="zs-history-toggle" aria-label="历史账号" @click="showHistory = !showHistory">▾</button>
                    <div v-if="showHistory" class="zs-history">
                      <button v-for="name in history" :key="name" type="button" @click="chooseHistory(name)">{{ name }}</button>
                      <p v-if="!history.length">暂无历史账号</p>
                    </div>
                  </div>
                  <div class="zs-field zs-with-icon">
                    <span class="zs-field-icon" aria-hidden="true">
                      <svg viewBox="0 0 24 24"><rect x="5" y="10" width="14" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></svg>
                    </span>
                    <input v-model="form.password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="密码" />
                    <button type="button" class="zs-eye" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword">
                      <svg v-if="!showPassword" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5C7 5 2.7 8.1 1 12c1.7 3.9 6 7 11 7s9.3-3.1 11-7c-1.7-3.9-6-7-11-7Zm0 11.5A4.5 4.5 0 1 1 12 7.5a4.5 4.5 0 0 1 0 9Zm0-7A2.5 2.5 0 1 0 12 14.5 2.5 2.5 0 0 0 12 9.5Z"/></svg>
                      <svg v-else viewBox="0 0 24 24" aria-hidden="true"><path d="M3.3 2 1.9 3.4l3 3C3.1 7.8 1.7 9.8 1 12c1.7 3.9 6 7 11 7 2 0 3.9-.5 5.5-1.3l3.3 3.3 1.4-1.4L3.3 2ZM12 17c-4.1 0-7.7-2.4-9.3-5 1-1.8 2.6-3.3 4.5-4.2l2.2 2.2a4.5 4.5 0 0 0 5.8 5.8l1.5 1.5c-1.4.4-2.9.7-4.7.7Zm10.8-5c-.8 1.8-2.2 3.4-4 4.6l-1.5-1.5a9.3 9.3 0 0 0 2.8-3.1c-1.5-2.4-4-4-7.6-4-.5 0-1 .1-1.5.2l-1.7-1.7c1-.3 2.1-.5 3.2-.5 4.5 0 8.4 2.6 10.3 6Z"/></svg>
                    </button>
                  </div>
                  <div class="zs-options">
                    <label><input v-model="remember" type="checkbox" /> 保存密码</label>
                    <label><input v-model="autoLogin" type="checkbox" /> 自动登录</label>
                  </div>
                  <button class="zs-submit" type="submit" :disabled="loading">{{ loading ? '正在登录…' : '登 录' }}</button>
                  <p class="zs-safe-tip">保存密码仅适用于本人专用设备</p>
                  <div class="zs-links">
                    <button type="button" @click="forgot">忘记账号/密码?</button>
                    <div class="zs-settings">
                      <button type="button" @click="showSettings = !showSettings">设置 ▾</button>
                      <div v-if="showSettings" class="zs-settings-menu">
                        <button type="button" @click="clearSaved">清除本机保存的账号</button>
                        <button type="button" @click="serverSettings">服务器设置（待接入）</button>
                      </div>
                    </div>
                    <button type="button" @click="offline">脱机</button>
                  </div>
                  <RouterLink to="/platform/login" class="zs-platform">平台管理入口 →</RouterLink>
                </template>

                <!-- 多厂选择：该手机号同时关联体验厂与/或多个制衣厂 -->
                <template v-else-if="mode === 'select-tenant'">
                  <p class="zs-flow-tip">
                    手机号 <strong>{{ maskPhone(phone) }}</strong> 关联了 {{ tenantChoices.length }} 个厂，请选择要进入的厂。
                  </p>
                  <div v-if="needFactoryPassword" class="zs-field zs-with-icon">
                    <span class="zs-field-icon" aria-hidden="true">
                      <svg viewBox="0 0 24 24"><rect x="5" y="10" width="14" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></svg>
                    </span>
                    <input v-model="smsPassword" type="password" autocomplete="current-password" placeholder="首次登录密码（厂管理员提供）" />
                  </div>
                  <p v-if="needFactoryPassword" class="zs-flow-msg">该手机号在本厂首次登录，请输入厂管理员提供的密码；验证成功后，以后只需手机验证码。</p>
                  <div class="zs-tenant-list">
                    <button v-for="t in tenantChoices" :key="t.kind + t.code" type="button" class="zs-tenant"
                      :class="{ trial: t.kind === 'trial' }" :disabled="pickingTenant" @click="enterByEntry(t, pendingTicket)">
                      <span class="zs-tenant-badge">{{ t.kind === 'trial' ? '体验厂' : '制衣厂' }}</span>
                      <span class="zs-tenant-body">
                        <strong>{{ t.name }}</strong>
                        <small v-if="t.kind === 'trial'">体验主页 · 到期前随时可用 · {{ t.code }}</small>
                        <small v-else>以 {{ t.user_name || t.login_name }} 的身份进入正式系统 · {{ t.code }}</small>
                      </span>
                      <span class="zs-tenant-go">{{ pickingTenant ? '…' : '进入 →' }}</span>
                    </button>
                  </div>
                  <p v-if="flowMsg" class="zs-flow-msg">{{ flowMsg }}</p>
                  <button type="button" class="zs-submit ghost" @click="switchMode('sms')">← 返回短信登录</button>
                </template>

                <template v-else>
                  <p class="zs-flow-tip">
                    {{ mode === 'register' ? '手机号验证码即可极速开通体验账号' : '已注册用户可用短信验证码直接登录' }}，
                    <strong>验证码 3 分钟内有效</strong>。
                  </p>
                  <div class="zs-field">
                    <input v-model.trim="phone" type="tel" maxlength="11" inputmode="numeric" placeholder="请输入 11 位手机号" />
                  </div>
                  <div class="zs-field zs-code">
                    <input v-model.trim="code" maxlength="6" inputmode="numeric" placeholder="6 位验证码" />
                    <button type="button" class="zs-code-btn" :disabled="resendSeconds > 0 || flowLoading" @click="sendCode">
                      {{ resendSeconds > 0 ? resendSeconds + 's 后重发' : expiresAt > 0 ? '重新发送' : '获取验证码' }}
                    </button>
                  </div>
                  <p v-if="expiresAt > 0" class="zs-count">验证码剩余 {{ codeCountdown }}</p>
                  <p v-if="demoCode" class="zs-demo-code">
                    演示验证码：<strong>{{ demoCode }}</strong>
                    <button type="button" @click="code = demoCode">自动填入</button>
                  </p>
                  <p v-if="flowMsg" class="zs-flow-msg" :class="{ ok: flowOk }">{{ flowMsg }}</p>
                  <button v-if="expiresAt === 0" class="zs-submit" type="button" :disabled="flowLoading" @click="sendCode">
                    {{ flowLoading ? '正在发送…' : '获取验证码' }}
                  </button>
                  <button v-else class="zs-submit" type="submit" :disabled="flowLoading">
                    {{ flowLoading ? '正在验证…' : mode === 'register' ? '注册并进入' : '登录并进入' }}
                  </button>
                </template>
              </div>

              <!-- 竖分隔线 -->
              <div class="zs-divider" aria-hidden="true"></div>

              <!-- 右侧：其他登录方式 -->
              <aside class="zs-other">
                <p class="zs-other-title">其他登录方式：</p>
                <div class="zs-other-btns">
                  <button type="button" @click="router.push('/register?mode=trial')">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="9" cy="8" r="3.2"/><path d="M3.5 20c0-3 2.4-5 5.5-5s5.5 2 5.5 5M18 7v6M15 10h6"/></svg>
                    <span>极速注册</span>
                  </button>
                  <button type="button" @click="switchMode('sms')">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3.5 6.5 8.5 6 8.5-6"/></svg>
                    <span>短信登录</span>
                  </button>
                  <button type="button" @click="openQr">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 9V6a2 2 0 0 1 2-2h3M15 4h3a2 2 0 0 1 2 2v3M20 15v3a2 2 0 0 1-2 2h-3M9 20H6a2 2 0 0 1-2-2v-3M4 12h16"/></svg>
                    <span>扫码登录</span>
                  </button>
                  <button type="button" @click="guestLogin">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="8" r="3.2"/><path d="M5.5 20c0-3.3 2.9-5.5 6.5-5.5s6.5 2.2 6.5 5.5"/></svg>
                    <span>游客浏览</span>
                  </button>
                </div>
                <div class="zs-third">
                  <button type="button" class="zs-third-wx" title="微信登录" @click="thirdParty('微信')">微</button>
                  <button type="button" class="zs-third-qq" title="QQ登录" @click="thirdParty('QQ')">Q</button>
                  <button type="button" class="zs-third-wb" title="微博登录" @click="thirdParty('微博')">博</button>
                </div>
                <p v-if="notice" class="zs-notice">{{ notice }}</p>
                <RouterLink to="/register" class="zs-portal-join">制衣厂注册（平台确认后启用） →</RouterLink>
                <RouterLink to="/register?mode=trial" class="zs-portal-join">免费体验一个月 →</RouterLink>
              </aside>
            </div>
          </div>
        </form>
      </section>
    </div>

    <!-- 扫码登录弹层：真实二维码，手机确认后 PC 端自动进入体验页 -->
    <div v-if="showQr" class="zs-overlay" @click.self="closeQr">
      <div class="zs-qrbox">
        <button type="button" class="zs-qr-close" aria-label="关闭" @click="closeQr">✕</button>
        <h3>扫码登录</h3>
        <div class="zs-qr">
          <img v-if="qrImage" :src="qrImage" alt="一智算扫码登录二维码" />
          <div v-else class="zs-qr-loading">二维码生成中…</div>
        </div>
        <p v-if="qrState === 'scanned'">已扫码，请在手机上点击确认</p>
        <p v-else-if="qrState === 'expired'">二维码已过期，请点下方刷新</p>
        <p v-else-if="qrState === 'cancelled'">已取消登录，请刷新后重试</p>
        <p v-else-if="qrState === 'offline'">二维码服务未启动，扫码将打开登录页</p>
        <p v-else>请使用微信「扫一扫」扫描下方大图</p>
        <p class="zs-qr-note">二维码已指向公网体验地址，手机无需与电脑同一网络；若微信内打不开，请复制下方地址到手机浏览器</p>
        <p v-if="qrHint" class="zs-qr-warn">{{ qrHint }}</p>
        <p v-if="qrTarget" class="zs-qr-target">{{ qrTarget }}</p>
        <button type="button" class="zs-qr-mock" @click="openQr">刷新二维码</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.zs-login{min-height:100vh;display:flex;align-items:center;position:relative;overflow:hidden;
  background:#ecf4ff;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif}
.zs-inner{position:relative;z-index:1;max-width:1240px;margin:0 auto;width:100%;padding:40px 24px;
  display:grid;grid-template-columns:.82fr 1.18fr;gap:44px;align-items:center}
.zs-hero{color:#17255f}
.zs-back{display:inline-block;font-size:13px;color:#5e7bb8;margin-bottom:36px}
.zs-back:hover{color:#2563eb}
.zs-hero h1{font-size:56px;margin:0 0 10px;letter-spacing:.02em}
.zs-dark{color:#17255f}
.zs-grad{background:linear-gradient(90deg,#2563eb,#06b6d4);-webkit-background-clip:text;background-clip:text;color:transparent}
.zs-en{font-size:15px;color:#5e7bb8;letter-spacing:.04em;margin:0 0 26px}
.zs-slogan{font-size:19px;color:#33477c;margin:0 0 16px}
.zs-desc{font-size:14px;color:#7b8ab0;line-height:1.9;max-width:400px;margin:0 0 40px}
.zs-company{font-size:13px;color:#8b9ac0;margin:0}
.zs-card-wrap{display:flex;justify-content:center}
.zs-card{width:min(700px,100%);background:rgba(255,255,255,.3);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);
  border:1px solid rgba(255,255,255,.55);border-radius:20px;padding:15px;
  box-shadow:0 0 26px rgba(255,255,255,.35),0 20px 50px rgba(23,37,95,.12)}
.zs-inner-card{background:rgba(255,255,255,.78);border-radius:14px;padding:26px}
.zs-panel-grid{display:grid;grid-template-columns:minmax(0,1fr) 1px 176px;gap:22px;align-items:start}
.zs-panel-head{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:20px}
.zs-panel-head h2{font-size:20px;color:#17255f;margin:0}
.zs-back-account{border:0;background:transparent;color:#2563eb;font-size:12.5px;font-weight:600;cursor:pointer;padding:0}
.zs-back-account:hover{text-decoration:underline}
/* 输入框 */
.zs-field{margin-bottom:14px;position:relative}
.zs-field input{width:100%;border:1px solid #cfd6e4;border-radius:6px;padding:11px 12px;font-size:14.5px;background:#fff;outline:none;transition:.15s}
.zs-with-icon input{padding-left:40px}
.zs-field input:focus{border-color:#2563eb;box-shadow:0 0 0 3px rgba(37,99,235,.12)}
.zs-field-icon{position:absolute;left:12px;top:50%;transform:translateY(-50%);color:#9aa6bb;display:grid;place-items:center;pointer-events:none}
.zs-field-icon svg{width:17px;height:17px;fill:none;stroke:currentColor;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.zs-history-toggle{position:absolute;right:6px;top:50%;transform:translateY(-50%);border:0;background:transparent;color:#8b9ac0;font-size:14px;cursor:pointer;padding:6px 8px}
.zs-history-toggle:hover{color:#2563eb}
.zs-history{position:absolute;top:calc(100% + 4px);left:0;right:0;background:#fff;border:1px solid #dbe4f3;border-radius:8px;
  box-shadow:0 12px 26px rgba(23,37,95,.16);z-index:20;padding:4px;max-height:180px;overflow:auto}
.zs-history button{display:block;width:100%;text-align:left;border:0;background:transparent;padding:8px 10px;font-size:13.5px;color:#33477c;border-radius:6px;cursor:pointer}
.zs-history button:hover{background:#f0f5ff;color:#2563eb}
.zs-history p{margin:0;padding:8px 10px;font-size:12.5px;color:#8b9ac0}
.zs-eye{position:absolute;right:8px;top:50%;transform:translateY(-50%);border:0;background:transparent;color:#8b9ac0;padding:6px;cursor:pointer;display:grid;place-items:center}
.zs-eye:hover{color:#2563eb}
.zs-eye svg{width:20px;height:20px;fill:currentColor}
/* 选项与按钮 */
.zs-options{display:flex;justify-content:space-between;gap:12px;margin:2px 0 16px;font-size:13.5px;color:#475467}
.zs-options label{display:inline-flex;align-items:center;gap:6px;cursor:pointer}
.zs-options input{width:15px;height:15px;accent-color:#2563eb;cursor:pointer}
.zs-submit{width:100%;border:0;border-radius:6px;padding:12px;font-size:17px;font-weight:600;letter-spacing:.16em;color:#fff;cursor:pointer;
  background:linear-gradient(90deg,#2563eb,#06b6d4);box-shadow:0 8px 20px rgba(37,99,235,.25);transition:.18s}
.zs-submit:hover{filter:brightness(1.06)}
.zs-submit:disabled{opacity:.55;cursor:not-allowed}
.zs-submit.ghost{background:#fff;border:1px solid #cbd5e1;color:#475569;box-shadow:none;letter-spacing:.05em;font-size:14px}
/* 多厂选择列表 */
.zs-tenant-list{display:flex;flex-direction:column;gap:10px;margin:4px 0 12px}
.zs-tenant{display:flex;align-items:center;gap:12px;width:100%;text-align:left;border:1px solid #e1e7f0;background:#fff;
  border-radius:10px;padding:12px 14px;cursor:pointer;font-family:inherit;transition:.15s}
.zs-tenant:hover{border-color:#2563eb;background:#f5f9ff}
.zs-tenant:disabled{opacity:.6;cursor:wait}
.zs-tenant.trial{border-color:#bfdbfe;background:#f0f7ff}
.zs-tenant-badge{flex:none;font-size:11px;font-weight:700;padding:3px 8px;border-radius:999px;color:#1d4ed8;background:#dbeafe}
.zs-tenant.trial .zs-tenant-badge{color:#065f46;background:#d1fae5}
.zs-tenant-body{display:flex;flex-direction:column;gap:2px;min-width:0}
.zs-tenant-body strong{font-size:14px;color:#0f172a;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.zs-tenant-body small{font-size:11.5px;color:#667085}
.zs-tenant-go{margin-left:auto;flex:none;font-size:12.5px;font-weight:600;color:#2563eb}
.zs-safe-tip{margin:8px 0 0;font-size:11.5px;color:#98a2b3}
.zs-links{display:flex;justify-content:space-between;align-items:center;gap:8px;margin-top:16px;font-size:13px}
.zs-links>button{border:0;background:transparent;color:#667085;font-size:13px;cursor:pointer;padding:0}
.zs-links>button:hover{color:#2563eb}
.zs-settings{position:relative}
.zs-settings>button{border:0;background:transparent;color:#667085;font-size:13px;cursor:pointer;padding:0}
.zs-settings>button:hover{color:#2563eb}
.zs-settings-menu{position:absolute;top:calc(100% + 6px);left:50%;transform:translateX(-50%);width:190px;background:#fff;
  border:1px solid #dbe4f3;border-radius:10px;box-shadow:0 12px 26px rgba(23,37,95,.16);padding:6px;z-index:20}
.zs-settings-menu button{display:block;width:100%;text-align:left;border:0;background:transparent;padding:8px 10px;font-size:12.5px;color:#33477c;border-radius:6px;cursor:pointer;white-space:nowrap}
.zs-settings-menu button:hover{background:#f0f5ff;color:#2563eb}
.zs-platform{display:block;text-align:center;margin-top:18px;padding-top:14px;border-top:1px dashed #e3ecfb;font-size:12.5px;color:#8b9ac0}
.zs-platform:hover{color:#2563eb}
.zs-error{background:#fef3f2;color:#b42318;border-radius:8px;padding:10px 14px;font-size:13px;margin-bottom:16px}
/* 验证码流程 */
.zs-flow-tip{font-size:13px;color:#667085;line-height:1.8;margin:0 0 16px}
.zs-flow-tip strong{color:#2563eb}
.zs-code{display:flex;gap:8px}
.zs-code input{flex:1;min-width:0;letter-spacing:.16em;padding-left:12px}
.zs-code-btn{border:1px solid #dbe4f3;background:#f4f8ff;color:#2563eb;border-radius:6px;padding:0 12px;font-size:12.5px;white-space:nowrap;cursor:pointer}
.zs-code-btn:disabled{opacity:.55;cursor:not-allowed}
.zs-count{font-size:12.5px;color:#2563eb;margin:-4px 0 10px;font-variant-numeric:tabular-nums}
.zs-demo-code{margin:0 0 10px;padding:8px 10px;border-radius:8px;background:#eff6ff;border:1px dashed #93b6f5;
  color:#41537e;font-size:12.5px;display:flex;align-items:center;gap:8px}
.zs-demo-code strong{color:#1d4ed8;letter-spacing:.14em}
.zs-demo-code button{border:0;background:transparent;color:#2563eb;font-size:12.5px;font-weight:600;cursor:pointer;padding:0}
.zs-flow-msg{font-size:12.5px;color:#d92d20;margin:0 0 10px}
.zs-flow-msg.ok{color:#039855}
/* 右侧其他登录方式 */
.zs-divider{width:1px;background:#e1e7f0;align-self:stretch;min-height:280px}
.zs-other-title{font-size:13.5px;color:#667085;margin:0 0 12px}
.zs-other-btns{display:flex;flex-direction:column;gap:10px}
.zs-other-btns button{display:flex;align-items:center;gap:10px;width:100%;padding:9px 12px;font-size:13.5px;color:#475467;
  border:1px solid #d5dce8;border-radius:6px;background:linear-gradient(180deg,#fff,#f4f6fa);cursor:pointer;transition:.16s}
.zs-other-btns button:hover{border-color:#f07a6f;color:#d92d20;box-shadow:0 4px 12px rgba(23,37,95,.10)}
.zs-other-btns svg{width:17px;height:17px;fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round;flex-shrink:0}
.zs-third{display:flex;gap:8px;margin-top:20px}
.zs-third button{width:30px;height:30px;border:1px solid #dfe5ee;border-radius:5px;background:#fff;color:#fff;font-size:13px;font-weight:700;cursor:pointer;transition:.16s}
.zs-third button:hover{transform:translateY(-2px);box-shadow:0 4px 10px rgba(23,37,95,.16)}
.zs-third-wx{background:#07c160!important}
.zs-third-qq{background:#12b7f5!important}
.zs-third-wb{background:#e6162d!important}
.zs-notice{margin:14px 0 0;font-size:12px;line-height:1.7;color:#d92d20}
.zs-portal-join{display:inline-block;margin-top:14px;font-size:12px;color:#2563eb;font-weight:600}
/* 扫码弹层 */
.zs-overlay{position:fixed;inset:0;background:rgba(10,20,50,.45);backdrop-filter:blur(4px);display:grid;place-items:center;z-index:100;padding:20px}
.zs-qrbox{position:relative;background:#fff;border-radius:18px;padding:32px 44px 28px;text-align:center;box-shadow:0 24px 60px rgba(10,20,50,.35)}
.zs-qrbox h3{margin:0 0 4px;font-size:19px;color:#17255f}
.zs-qr{margin:14px auto;width:260px;padding:8px;border:1px solid #eef2fa;border-radius:12px;background:#fff}
.zs-qr img{display:block;width:100%;height:auto;image-rendering:auto}
.zs-qr-loading{height:244px;display:grid;place-items:center;color:#8b9ac0;font-size:13px}
.zs-qrbox p{margin:0 0 4px;font-size:14px;color:#33477c}
.zs-qr-note{font-size:12px!important;color:#8b9ac0!important;line-height:1.7}
.zs-qr-warn{font-size:12.5px!important;color:#b54708!important;line-height:1.7}
.zs-qr-target{margin:8px 0 0!important;padding:7px 9px;border-radius:8px;background:#f6f9ff;border:1px solid #e3ecfb;
  font-size:11px!important;line-height:1.5!important;color:#667085!important;word-break:break-all}
.zs-qr-close{position:absolute;top:10px;right:10px;width:30px;height:30px;border:0;background:#f2f5fb;color:#667085;border-radius:50%;cursor:pointer;font-size:14px}
.zs-qr-close:hover{background:#e7edf7}
.zs-qr-mock{margin-top:14px;border:1px solid #dbe4f3;background:#f4f8ff;color:#2563eb;border-radius:8px;padding:8px 16px;font-size:13px;font-weight:600;cursor:pointer}
.zs-qr-mock:hover{border-color:#2563eb}
@media(max-width:1080px){
  .zs-inner{grid-template-columns:1fr;gap:30px}
  .zs-hero h1{font-size:40px}
  .zs-back{margin-bottom:16px}
  .zs-desc{margin-bottom:16px}
}
@media(max-width:640px){
  .zs-inner{padding:24px 14px}
  .zs-inner-card{padding:20px 16px}
  .zs-panel-grid{grid-template-columns:1fr;gap:18px}
  .zs-divider{width:100%;height:1px;min-height:0}
  .zs-other-btns{display:grid;grid-template-columns:1fr 1fr}
  .zs-third{margin-top:14px}
}
</style>
