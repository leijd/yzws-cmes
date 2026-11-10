<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import NetworkBackground from '../components/NetworkBackground.vue'
import BgAnimToggle from '../components/BgAnimToggle.vue'
import LoginQrBadge from '../components/LoginQrBadge.vue'
import TrialAccountDialog from '../components/TrialAccountDialog.vue'
import { syncNeedPhone } from '../phoneGate'
import type { QrApp } from '../qrLogin'
import {
  TRIAL_RESEND_MS, formatCountdown, isValidPhone, maskPhone,
  clearTrialSession, readTrialChallenge, readTrialSession, saveTrialChallenge,
  sendRegisterCode, startTrial,
} from '../trial'

const router = useRouter()
const year = new Date().getFullYear()
/* 三个模块入口共用的用户名密码登录弹层 */
const accountApp = ref<QrApp | null>(null)
function openAccount(app: QrApp) { accountApp.value = app }

interface AppCard {
  key: QrApp
  name: string
  en: string
  slogan: string
  theme: string
  icon: string
  to: string
  points: string[]
}

const apps: AppCard[] = [
  {
    key: 'zhisuan', name: '一智算', en: 'AI Accounting & Settlement', slogan: '算账结算省心',
    theme: 'blue', icon: '智', to: '/login',
    points: ['订单工单全流程', '扫菲计件算工资', '一厂一密钥加密'],
  },
  {
    key: 'couhe', name: '二撮合', en: 'Order Matching Platform', slogan: '订单客户与制衣厂双向撮合',
    theme: 'green', icon: '合', to: '/login/matcher',
    points: ['产能评价模型', 'AI 漫剧营销', '供需双向撮合'],
  },
  {
    key: 'sansheng', name: '三生万', en: 'Business Model Intelligence', slogan: '经营分析模型 → 数据资产',
    theme: 'purple', icon: '生', to: '/login/sansheng',
    points: ['经营分析模型', '能力提升服务', '数据资产入伙'],
  },
]

/* 卡片入口：注册用户进业务主页，体验用户进体验主页，游客/未登录进对应模块登录页 */
const WORKSPACE_HOME = '/workbench/A03'
const enteringKey = ref<QrApp | ''>('')

/* 本地 token 可能已过期：进入业务主页前先向后端确认，避免未登录直接打开工作台 */
async function hasValidAccessToken(): Promise<boolean> {
  const token = localStorage.getItem('clothp_access_token')
  if (!token) return false
  try {
    const response = await fetch('/api/v1/me', { headers: { Authorization: `Bearer ${token}` } })
    if (response.ok) return true
    localStorage.removeItem('clothp_access_token')
    localStorage.removeItem('clothp_refresh_token')
    return false
  } catch {
    return false
  }
}

function isGuestSession(session: ReturnType<typeof readTrialSession>) {
  /* 兼容历史数据：旧版游客会话没有 via 字段，用展示名兜底 */
  return !!session && (session.via === 'guest' || session.phone === '游客用户')
}

async function enterApp(a: AppCard) {
  if (enteringKey.value) return
  enteringKey.value = a.key
  try {
    if (await hasValidAccessToken()) { await router.push(WORKSPACE_HOME); return }
    const session = readTrialSession()
    if (session && !isGuestSession(session)) { await router.push(`/trial/home?app=${a.key}`); return }
    /* 游客浏览不算登录态，顺手清掉旧游客会话，避免下次被当成已登录 */
    if (session) clearTrialSession()
    await router.push(a.to)
  } finally {
    enteringKey.value = ''
  }
}

/* ===== 免费体验 / 客户登录 Tab ===== */
const joinTab = ref<'trial' | 'login'>('trial')

/* ===== 免费体验：手机号 → 短信验证码（3 分钟内有效）→ 进入一智算演示主页 ===== */
const trialStep = ref<'phone' | 'code'>('phone')
const trialPhone = ref('')
const trialCode = ref('')
const trialMsg = ref('')
const trialOk = ref(false)
const trialLoading = ref(false)
const trialDemoCode = ref('')
const trialSmsSent = ref(false)
const trialExpiresAt = ref(0)
const trialResendAt = ref(0)
const nowTs = ref(Date.now())
let trialTimer: number | undefined

const trialSecondsLeft = computed(() => Math.max(0, trialExpiresAt.value - nowTs.value))
const trialResendSeconds = computed(() => Math.max(0, Math.ceil((trialResendAt.value - nowTs.value) / 1000)))
const trialCountdown = computed(() => formatCountdown(trialSecondsLeft.value))
const trialMaskedPhone = computed(() => maskPhone(trialPhone.value))

function persistTrial() {
  if (trialStep.value === 'phone' && !trialResendAt.value) { saveTrialChallenge(null); return }
  saveTrialChallenge({
    phone: trialPhone.value,
    step: trialStep.value,
    expiresAt: trialExpiresAt.value,
    resendAt: trialResendAt.value,
    demoCode: trialDemoCode.value,
    smsSent: trialSmsSent.value,
  })
}

function resetTrialChallenge() {
  trialStep.value = 'phone'
  trialCode.value = ''
  trialDemoCode.value = ''
  trialExpiresAt.value = 0
  trialResendAt.value = 0
  trialSmsSent.value = false
  saveTrialChallenge(null)
}

async function handleSendCode() {
  trialMsg.value = ''
  trialOk.value = false
  if (!isValidPhone(trialPhone.value)) { trialMsg.value = '请输入正确的 11 位手机号'; return }
  if (trialResendSeconds.value > 0) { trialMsg.value = `请 ${trialResendSeconds.value} 秒后再获取验证码`; return }
  trialLoading.value = true
  try {
    const result = await sendRegisterCode(trialPhone.value)
    trialStep.value = 'code'
    trialCode.value = ''
    trialExpiresAt.value = result.expiresAt
    trialResendAt.value = Date.now() + TRIAL_RESEND_MS
    trialDemoCode.value = result.demoCode
    trialSmsSent.value = result.smsSent
    nowTs.value = Date.now()
    trialOk.value = true
    trialMsg.value = result.smsSent
      ? `验证码已发送至 ${trialMaskedPhone.value}，3 分钟内有效。`
      : '演示模式：未配置短信服务商，请查看下方演示验证码，3 分钟内有效。'
    persistTrial()
  } catch (e) {
    trialMsg.value = (e as Error).message || '验证码发送失败，请稍后重试'
  } finally { trialLoading.value = false }
}

async function handleVerifyCode() {
  trialMsg.value = ''
  trialOk.value = false
  if (!/^\d{6}$/.test(trialCode.value)) { trialMsg.value = '请输入 6 位数字验证码'; return }
  if (trialSecondsLeft.value <= 0) {
    resetTrialChallenge()
    trialMsg.value = '验证码已超过 3 分钟有效期，请重新获取'
    return
  }
  trialLoading.value = true
  try {
    /* 一键免费体验：验证码通过即自动开通真实体验厂（手机号/账号/密码/密钥/类型/起止时间落库），随后进入体验页 */
    await startTrial(trialPhone.value, trialCode.value)
    saveTrialChallenge(null)
    await router.push('/trial/home')
  } catch (e) {
    trialMsg.value = (e as Error).message || '体验开通失败，请稍后重试'
  } finally { trialLoading.value = false }
}

function backToPhone() {
  trialStep.value = 'phone'
  trialCode.value = ''
  trialExpiresAt.value = 0
  trialDemoCode.value = ''
  trialMsg.value = ''
  trialOk.value = false
  persistTrial()
}

onMounted(() => {
  const saved = readTrialChallenge()
  if (saved) {
    trialPhone.value = saved.phone
    trialStep.value = saved.step
    trialExpiresAt.value = saved.expiresAt
    trialResendAt.value = saved.resendAt
    trialDemoCode.value = saved.demoCode
    trialSmsSent.value = saved.smsSent
    if (saved.step === 'code' && saved.expiresAt <= Date.now()) {
      resetTrialChallenge()
      trialMsg.value = '验证码已过期，请重新获取'
    }
  }
  trialTimer = window.setInterval(() => {
    nowTs.value = Date.now()
    if (trialStep.value === 'code' && trialExpiresAt.value > 0 && trialExpiresAt.value <= nowTs.value) {
      resetTrialChallenge()
      trialMsg.value = '验证码已超过 3 分钟有效期，请重新获取'
    }
  }, 1000)
})
onBeforeUnmount(() => { if (trialTimer) window.clearInterval(trialTimer) })

/* ===== 注册客户登录 ===== */
const loginForm = reactive({ login_name: '', password: '', node_id: 'web-local' })
const loginLoading = ref(false)
const loginError = ref('')
const showPassword = ref(false)
async function login() {
  loginError.value = ''
  if (!loginForm.login_name || !loginForm.password) { loginError.value = '请填写账号和密码'; return }
  loginLoading.value = true
  try {
    const result = await api<{ account_type: 'tenant' | 'platform'; access_token: string; refresh_token?: string; password_change_required?: boolean; need_contact_phone?: boolean; tenant?: { code?: string } }>(
      '/auth/unified-login', { method: 'POST', body: JSON.stringify(loginForm) })
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
  } catch (e) { loginError.value = (e as Error).message } finally { loginLoading.value = false }
}
</script>

<template>
  <div class="portal">
    <!-- 顶部导航 -->
    <header class="p-nav">
      <div class="p-nav-inner">
        <div class="p-brand">
          <span class="p-brand-mark">智</span>
          <span class="p-brand-name">一智万商<small>南宁一智万商科技有限责任公司</small></span>
        </div>
        <nav class="p-links">
          <a href="#top">首页</a>
          <a href="#apps">产品体系</a>
          <a href="#join">帮助中心</a>
        </nav>
        <div class="p-nav-actions">
          <a href="#join" class="p-btn ghost">客户登录</a>
          <a href="#join" class="p-btn solid">免费体验</a>
        </div>
      </div>
    </header>

    <!-- 主视觉 -->
    <section id="top" class="p-hero">
      <NetworkBackground theme="vivid" />
      <div class="p-hero-inner">
        <div class="p-hero-left">
          <h1><span class="p-title-dark">一智</span><span class="p-title-grad">万商</span><br /><span class="p-title-grad">企业 AI 经营平台</span></h1>
          <p class="p-hero-sub">1步2步3步，我们一起走进AI</p>
          <div class="p-hero-actions">
            <a href="#join" class="p-btn big solid">免费体验一个月</a>
            <a href="#apps" class="p-btn big soft">了解三大应用 ↓</a>
          </div>
        </div>
        <div id="apps" class="p-apps">
          <div v-for="a in apps" :key="a.key" class="p-app-card" :class="a.theme">
            <div class="p-app-inner">
              <LoginQrBadge :app="a.key" />
              <div class="p-app-head">
                <span class="p-app-icon">{{ a.icon }}</span>
                <div>
                  <h3>{{ a.name }}</h3>
                  <p class="p-app-en">{{ a.en }}</p>
                </div>
              </div>
              <p class="p-app-slogan">{{ a.slogan }}</p>
              <ul class="p-app-points">
                <li v-for="pt in a.points" :key="pt">{{ pt }}</li>
              </ul>
              <button type="button" class="p-app-btn" :disabled="enteringKey === a.key" @click="enterApp(a)">
                {{ enteringKey === a.key ? '正在进入…' : '进入' + a.name }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 登录区：Tab 切换 -->
    <section id="join" class="p-join">
      <div class="p-section-head">
        <h2>统一入口，各走各的路</h2>
        <p>新客户手机号即可免费体验一个月；已签约客户登录进入工作台。</p>
      </div>
      <div class="p-tabbox">
        <div class="p-tabs" role="tablist">
          <button class="p-tab" :class="{ active: joinTab === 'trial' }" role="tab" @click="joinTab = 'trial'">
            <span class="p-tab-badge">新客户</span>免费体验
          </button>
          <button class="p-tab" :class="{ active: joinTab === 'login' }" role="tab" @click="joinTab = 'login'">
            <span class="p-tab-badge deep">注册客户</span>客户登录
          </button>
        </div>
        <!-- 面板一：免费体验（手机号 → 短信验证码） -->
        <div v-show="joinTab === 'trial'" class="p-tabpane">
          <p class="p-tab-desc">手机号验证通过即<strong>自动开通体验制衣厂（30 天）</strong>——手机号、账号、密码、密钥、类型与起止时间自动生成，功能与页面和正式厂完全相同。到期再决定，不花冤枉钱。</p>

          <template v-if="trialStep === 'phone'">
            <div class="p-field">
              <label>手机号</label>
              <input v-model.trim="trialPhone" type="tel" maxlength="11" placeholder="请输入 11 位手机号" inputmode="numeric" @keyup.enter="handleSendCode" />
            </div>
            <p v-if="trialMsg" class="p-msg" :class="{ ok: trialOk }">{{ trialMsg }}</p>
            <button class="p-btn big solid full" :disabled="trialLoading" @click="handleSendCode">
              {{ trialLoading ? '正在发送…' : trialResendSeconds > 0 ? trialResendSeconds + 's 后重新获取' : '获取验证码' }}
            </button>
          </template>

          <template v-else>
            <div class="p-code-head">
              <span>验证码已发送至 <strong>{{ trialMaskedPhone }}</strong></span>
              <button type="button" class="p-text-btn" @click="backToPhone">修改手机号</button>
            </div>
            <div class="p-field">
              <label>短信验证码 <em>剩余 {{ trialCountdown }}</em></label>
              <div class="p-code-row">
                <input v-model.trim="trialCode" type="text" maxlength="6" inputmode="numeric" placeholder="请输入 6 位验证码" @keyup.enter="handleVerifyCode" />
                <button type="button" class="p-code-btn" :disabled="trialResendSeconds > 0 || trialLoading" @click="handleSendCode">
                  {{ trialResendSeconds > 0 ? trialResendSeconds + 's 重发' : '重新发送' }}
                </button>
              </div>
            </div>
            <p v-if="trialDemoCode" class="p-demo-code">
              演示验证码：<strong>{{ trialDemoCode }}</strong>
              <button type="button" class="p-text-btn" @click="trialCode = trialDemoCode">自动填入</button>
            </p>
            <p v-if="trialMsg" class="p-msg" :class="{ ok: trialOk }">{{ trialMsg }}</p>
            <button class="p-btn big solid full" :disabled="trialLoading" @click="handleVerifyCode">
              {{ trialLoading ? '正在验证…' : '验证并进入一智算' }}
            </button>
          </template>
          <p class="p-panel-note">✓ 全功能开放 &nbsp; ✓ 无需绑卡 &nbsp; ✓ 专员一对一带教</p>
          <p class="p-panel-note"><button type="button" class="p-text-btn" @click="openAccount('zhisuan')">已有体验账号？用户名密码登录 →</button></p>
        </div>
        <!-- 面板二：注册客户登录 -->
        <div v-show="joinTab === 'login'" class="p-tabpane">
          <p class="p-tab-desc">适用于<strong>已签约的付费客户</strong>，登录后进入您厂的工作台。</p>
          <div class="p-field">
            <label>账号</label>
            <input v-model.trim="loginForm.login_name" autocomplete="username" placeholder="请输入账号" />
          </div>
          <div class="p-field">
            <label>密码</label>
            <div class="p-pwd">
              <input v-model="loginForm.password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="请输入密码" @keyup.enter="login" />
              <button type="button" @click="showPassword = !showPassword">{{ showPassword ? '隐藏' : '显示' }}</button>
            </div>
          </div>
          <p v-if="loginError" class="p-msg">{{ loginError }}</p>
          <button class="p-btn big deep full" :disabled="loginLoading" @click="login">
            {{ loginLoading ? '正在登录…' : '登 录' }}
          </button>
          <p class="p-panel-note"><RouterLink to="/login" class="p-link">更多登录方式：极速注册 / 短信 / 扫码 / 游客 →</RouterLink></p>
        </div>
      </div>
    </section>

    <!-- 页脚 -->
    <footer class="p-footer">
      <div class="p-footer-grid">
        <div class="p-footer-col brand-col">
          <div class="p-brand small"><span class="p-brand-mark">智</span><span>一智万商 · 企业 AI 经营平台</span></div>
          <p class="p-footer-slogan">引商入局 · 开拓新土 · 共创未来</p>
          <p class="p-footer-desc">专注小微企业数字化：一智算打地基，二撮合接订单，三生万共成长。</p>
        </div>
        <div class="p-footer-col">
          <h4>联系我们</h4>
          <p>商务合作：0771-XXXXXXX</p>
          <p>微信咨询：yzs-service（一智算客服）</p>
          <p>邮箱：business@yizhisuan.cn</p>
        </div>
        <div class="p-footer-col">
          <h4>公司地址</h4>
          <p>广西南宁市青秀区<br />XX路 XX 号 XX 大厦 XX 层</p>
          <p class="p-footer-note">（正式地址以营业执照登记为准）</p>
        </div>
        <div class="p-footer-col qr-col">
          <h4>免费体验</h4>
          <div class="p-qr">二维码<br />占位</div>
          <p class="p-footer-note">扫码体验 / 添加客服</p>
        </div>
      </div>
      <div class="p-footer-bar">
        <p>© {{ year }} 南宁一智万商科技有限责任公司 版权所有</p>
        <p><a href="https://beian.miit.gov.cn" target="_blank" rel="noopener">桂ICP备XXXXXXXX号-X</a><span class="p-footer-sep">|</span><span>桂公网安备 XXXXXXXXXXXXX号</span></p>
      </div>
    </footer>

    <!-- 三个模块共用：用户名密码登录 / 注册 -->
    <TrialAccountDialog v-if="accountApp" :app="accountApp" @close="accountApp = null" />

    <!-- 背景动效开关（默认关闭） -->
    <BgAnimToggle />
  </div>
</template>

<style scoped>
.portal{min-height:100vh;color:#17255f;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;
  background:linear-gradient(150deg,#dcebff 0%,#e9f4ff 34%,#dff2fb 66%,#d6e9fd 100%)}
/* 按钮 */
.p-btn{display:inline-flex;align-items:center;justify-content:center;border-radius:10px;padding:9px 20px;font-size:14px;font-weight:600;border:1px solid transparent;transition:.18s;white-space:nowrap}
.p-btn.solid{background:linear-gradient(90deg,#2563eb,#06b6d4);color:#fff;box-shadow:0 8px 20px rgba(37,99,235,.25)}
.p-btn.solid:hover{filter:brightness(1.06)}
.p-btn.deep{background:linear-gradient(90deg,#1d4ed8,#0891b2);color:#fff;box-shadow:0 8px 20px rgba(29,78,216,.22)}
.p-btn.deep:hover{filter:brightness(1.06)}
.p-btn.ghost{border-color:#c3d5f2;color:#1d4ed8;background:rgba(255,255,255,.7)}
.p-btn.ghost:hover{border-color:#2563eb}
.p-btn.soft{background:rgba(255,255,255,.75);color:#1d4ed8;border-color:#dbe7fb}
.p-btn.big{padding:14px 32px;font-size:16px;border-radius:12px}
.p-btn.full{width:100%}
.p-btn:disabled{opacity:.55;cursor:not-allowed}
/* 导航 */
.p-nav{position:sticky;top:0;z-index:50;background:rgba(255,255,255,.75);backdrop-filter:blur(12px);border-bottom:1px solid rgba(255,255,255,.9)}
.p-nav-inner{max-width:1200px;margin:0 auto;height:66px;display:flex;align-items:center;gap:28px;padding:0 24px}
.p-brand{display:flex;align-items:center;gap:10px;font-weight:800;font-size:19px;color:#17255f}
.p-brand small{display:block;font-size:11px;font-weight:500;color:#7b8ab0}
.p-brand-mark{display:grid;place-items:center;width:36px;height:36px;border-radius:10px;color:#fff;font-size:15px;flex-shrink:0;
  background:linear-gradient(135deg,#2563eb,#06b6d4);box-shadow:0 4px 12px rgba(37,99,235,.3)}
.p-links{display:flex;gap:24px;margin-left:auto}
.p-links a{font-size:14px;color:#41537e}.p-links a:hover{color:#2563eb}
.p-nav-actions{display:flex;gap:10px}
/* 主视觉 */
.p-hero{position:relative;overflow:hidden}
.p-hero::after{content:"";position:absolute;left:0;right:0;bottom:0;height:110px;z-index:0;pointer-events:none;
  background:linear-gradient(180deg,rgba(220,235,255,0),rgba(224,238,255,.82) 62%,#dcebff 100%)}
.p-hero-inner{position:relative;z-index:1;max-width:1200px;margin:0 auto;padding:72px 24px 80px;display:grid;grid-template-columns:1fr 1.15fr;gap:48px;align-items:center}
.p-hero h1{font-size:50px;line-height:1.3;margin:0 0 18px;letter-spacing:-.01em}
.p-title-dark{color:#17255f}
.p-title-grad{background:linear-gradient(90deg,#2563eb,#06b6d4);-webkit-background-clip:text;background-clip:text;color:transparent}
.p-hero-sub{font-size:17px;color:#33477c;margin:0 0 10px}
.p-hero-desc{color:#7b8ab0;font-size:14px;margin:0 0 34px}
.p-hero-actions{display:flex;gap:14px;flex-wrap:wrap}
/* 应用卡片：外框 90% 白（四周突出 15px）+ 内容区 80% 白 */
.p-apps{position:relative;top:-30px;display:flex;flex-direction:column;gap:16px}
.p-app-card{background:rgba(255,255,255,.26);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);
  border:1px solid rgba(255,255,255,.62);border-radius:20px;padding:15px;
  box-shadow:0 0 26px rgba(255,255,255,.35),0 14px 34px rgba(15,42,110,.18);transition:box-shadow .25s}
.p-app-card:hover{box-shadow:0 0 34px rgba(255,255,255,.55),0 18px 44px rgba(15,42,110,.26)}
.p-app-inner{background:rgba(255,255,255,.82);border-radius:14px;padding:18px 20px;position:relative}
.p-app-head{display:flex;align-items:center;gap:12px}
.p-app-head h3{margin:0;font-size:22px}
.p-app-en{font-size:12pt;color:#5b6f9c;margin:2px 0 0;letter-spacing:.02em}
.p-app-icon{display:grid;place-items:center;width:46px;height:46px;border-radius:12px;color:#fff;font-size:19px;font-weight:700;flex-shrink:0}
.p-app-card.blue .p-app-icon{background:linear-gradient(135deg,#2563eb,#06b6d4)}
.p-app-card.green .p-app-icon{background:linear-gradient(135deg,#2563eb,#22c55e)}
.p-app-card.purple .p-app-icon{background:linear-gradient(135deg,#7c3aed,#c084fc)}
.p-app-slogan{margin:14px 0 12px;font-weight:600;font-size:17px;color:#33477c}
.p-app-points{list-style:none;margin:0 0 16px;padding:0;display:flex;flex-wrap:wrap;gap:8px 16px}
.p-app-points li{padding:0 0 0 18px;position:relative;color:#3f5182;font-size:12pt;white-space:nowrap;flex-shrink:0}
.p-app-points li::before{content:"✓";position:absolute;left:0;color:#22c55e;font-weight:700}
.p-app-desc{margin:0 0 14px;font-size:13px;color:#7b8ab0;line-height:1.7}
.p-app-btn{display:block;width:100%;border:0;text-align:center;border-radius:10px;padding:11px;font-size:12pt;font-weight:600;color:#fff;cursor:pointer;font-family:inherit}
.p-app-btn:disabled{opacity:.62;cursor:wait}
.p-app-card.blue .p-app-btn{background:linear-gradient(90deg,#2563eb,#06b6d4)}
.p-app-card.green .p-app-btn{background:linear-gradient(90deg,#2563eb,#22c55e)}
.p-app-card.purple .p-app-btn{background:linear-gradient(90deg,#7c3aed,#c084fc)}
.p-app-btn:hover{filter:brightness(1.07)}
/* 区块头 */
.p-section-head{text-align:center;max-width:760px;margin:0 auto 44px;padding:0 24px}
.p-section-head h2{font-size:32px;margin:0 0 12px;letter-spacing:-.01em;color:#17255f}
.p-section-head p{color:#7b8ab0;font-size:15px;line-height:1.8;margin:0}
/* 登录区 Tab */
.p-join{max-width:1200px;margin:0 auto;padding:74px 24px 90px}
.p-tabbox{max-width:520px;margin:0 auto;background:rgba(255,255,255,.88);backdrop-filter:blur(10px);border:1px solid rgba(255,255,255,.95);
  border-radius:20px;padding:12px 32px 34px;box-shadow:0 14px 40px rgba(23,37,95,.1)}
.p-tabs{display:flex;border-bottom:1px solid #e3ecfb;margin:0 -32px 26px}
.p-tab{flex:1;border:0;background:transparent;padding:18px 8px 15px;font-size:16px;font-weight:600;color:#7b8ab0;cursor:pointer;
  border-bottom:2.5px solid transparent;display:flex;align-items:center;justify-content:center;gap:8px;transition:.15s}
.p-tab:hover{color:#2563eb}
.p-tab.active{color:#17255f;border-bottom-color:#2563eb}
.p-tab-badge{font-size:11px;font-weight:600;border-radius:999px;padding:2px 9px;color:#fff;background:linear-gradient(90deg,#22c55e,#06b6d4)}
.p-tab-badge.deep{background:linear-gradient(90deg,#1d4ed8,#0891b2)}
.p-tab-desc{font-size:14px;color:#7b8ab0;line-height:1.8;margin:0 0 22px}
.p-tab-desc strong{color:#2563eb}
.p-field{margin-bottom:16px}
.p-field label{display:block;font-size:13px;font-weight:600;color:#41537e;margin-bottom:7px}
.p-field input{width:100%;border:1px solid #d5e1f5;border-radius:10px;padding:12px 14px;font-size:14.5px;background:#fff;outline:none;transition:.15s}
.p-field input:focus{border-color:#2563eb;box-shadow:0 0 0 3px rgba(37,99,235,.12)}
.p-pwd{display:flex;gap:8px}
.p-pwd input{flex:1}
.p-pwd button{border:1px solid #d5e1f5;background:#f4f8ff;color:#2563eb;border-radius:10px;padding:0 14px;font-size:13px;white-space:nowrap}
.p-msg{font-size:13px;color:#d92d20;margin:-4px 0 12px}
.p-msg.ok{color:#039855}
/* 免费体验验证码 */
.p-code-head{display:flex;align-items:center;justify-content:space-between;gap:10px;font-size:13.5px;color:#41537e;margin:0 0 16px}
.p-code-head strong{color:#17255f}
.p-text-btn{border:0;background:transparent;color:#2563eb;font-size:13px;font-weight:600;cursor:pointer;padding:0;white-space:nowrap}
.p-text-btn:hover{text-decoration:underline}
.p-field label em{font-style:normal;float:right;font-weight:600;color:#2563eb;font-variant-numeric:tabular-nums}
.p-code-row{display:flex;gap:8px}
.p-code-row input{flex:1;min-width:0;letter-spacing:.18em;font-size:16px}
.p-code-btn{border:1px solid #d5e1f5;background:#f4f8ff;color:#2563eb;border-radius:10px;padding:0 14px;font-size:13px;font-weight:600;white-space:nowrap;cursor:pointer}
.p-code-btn:disabled{opacity:.55;cursor:not-allowed}
.p-demo-code{margin:0 0 12px;padding:9px 12px;border-radius:10px;background:#eff6ff;border:1px dashed #93b6f5;
  color:#41537e;font-size:13px;display:flex;align-items:center;gap:8px}
.p-demo-code strong{color:#1d4ed8;letter-spacing:.16em;font-size:15px}
.p-panel-note{margin:16px 0 0;font-size:12.5px;color:#8b9ac0;text-align:center}
.p-link{color:#2563eb;font-weight:600}
/* 页脚 */
.p-footer{background:#0d1b3e;color:#8fa3c8;padding:56px 24px 0;font-size:13px}
.p-footer-grid{max-width:1180px;margin:0 auto;display:grid;grid-template-columns:1.6fr 1fr 1fr .8fr;gap:36px;padding-bottom:40px}
.p-footer-col h4{color:#fff;font-size:14px;margin:0 0 14px}
.p-footer-col p{margin:0 0 8px;line-height:1.7}
.p-footer .p-brand{color:#fff;font-size:15px}
.p-footer-slogan{margin:14px 0 6px;color:#c9d8f5;font-size:14px;letter-spacing:.1em}
.p-footer-desc{line-height:1.8}
.p-footer-note{color:#5f7398;font-size:12px}
.p-qr{width:92px;height:92px;border:1px dashed #415980;border-radius:10px;display:grid;place-items:center;text-align:center;color:#5f7398;font-size:12px;line-height:1.6;margin-bottom:10px}
.p-footer-bar{max-width:1180px;margin:0 auto;border-top:1px solid #1c2c48;padding:18px 0 22px;display:flex;flex-wrap:wrap;gap:8px 24px;justify-content:space-between}
.p-footer-bar p{margin:0}
.p-footer-bar a{color:#8fa3c8}.p-footer-bar a:hover{color:#fff}
.p-footer-sep{margin:0 12px;color:#31456a}
@media(max-width:1020px){
  .p-hero-inner{grid-template-columns:1fr;gap:40px;padding:56px 20px 64px}
  .p-apps{top:0}
  .p-hero h1{font-size:38px}
  .p-links{display:none}
  .p-footer-grid{grid-template-columns:1fr 1fr;gap:26px}
}
@media(max-width:600px){
  .p-hero h1{font-size:31px}
  .p-nav-actions .ghost{display:none}
  .p-footer-grid{grid-template-columns:1fr;gap:22px}
  .p-tabbox{padding:12px 20px 26px}
  .p-tabs{margin:0 -20px 22px}
  .p-tab{font-size:14.5px}
  .p-code-row{flex-direction:column}
  .p-code-btn{padding:11px}
  .p-app-points{flex-wrap:wrap;row-gap:6px}
  .p-app-points li{flex-shrink:1}
}
</style>
