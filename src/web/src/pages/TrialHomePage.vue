<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import NetworkBackground from '../components/NetworkBackground.vue'
import BgAnimToggle from '../components/BgAnimToggle.vue'
import { QR_APP_NAMES, type QrApp } from '../qrLogin'
import { api } from '../api'
import { toastError, toastSuccess } from '../toast'
import { syncNeedPhone } from '../phoneGate'
import { clearTrialSession, isValidPhone, maskPhone, myFactories, readTrialSession, saveTrialSession, switchTenant, trialEnter, trialExtend, trialExtendStatus, workspaceHome, type PortalFactory, type TrialExtensionRequest } from '../trial'

const router = useRouter()
const route = useRoute()
const session = readTrialSession()
const maskedPhone = computed(() => maskPhone(session?.phone || ''))
const notice = ref('')
const viaQr = computed(() => route.query.via === 'qr' || session?.via === 'qr')
/* 游客只是浏览，不算登录态 */
const viaGuest = computed(() => route.query.via === 'guest'
  || session?.via === 'guest' || session?.phone === '游客用户')
const viaAccount = computed(() => !viaQr.value && !viaGuest.value
  && (route.query.via === 'account' || session?.via === 'account' || !!session?.displayName))
const qrAppName = computed(() => QR_APP_NAMES[String(route.query.app || 'zhisuan') as QrApp] || '一智算')
const accountName = ref('')
/* 收费厂门户优先显示登录账号名；体验门户显示体验会话名称 */
const displayName = computed(() => portal.value?.edition === 'paid'
  ? (accountName.value || '本厂用户')
  : (session?.displayName || accountName.value || maskedPhone.value))
/* 厂编码：登录后进入门户时以 URL 上的 ?tenant= 为准（避免本机遗留的体验会话把收费厂带成体验门户） */
const tenantCode = computed(() => String(route.query.tenant || session?.tenantCode || ''))

/* ===== 制衣厂门户（体验用户与注册用户共用本页）=====
 * 有厂编码即加载门户信息：厂名/logo/版本类型/欢迎词/概览数据。
 * 体验用户（edition=trial）右侧按钮「开始体验」，注册用户（edition=paid）右侧按钮「进入工作」，点开同一工作台。 */
interface PortContenInfo { welcome_title: string; welcome_text: string; logo_url: string; customized: boolean }
interface PortalInfo {
  code: string; name: string; short_code: string; edition: 'trial' | 'paid'; trial_ends_at?: number | null
  content: PortContenInfo
  stats: { today_orders: number; wip_orders: number; employees: number; total_orders: number }
}
const portal = ref<PortalInfo | null>(null)
const isPaid = computed(() => portal.value?.edition === 'paid')
const portalName = computed(() => portal.value?.name || displayName.value || '一智算制衣')
const portalLogo = computed(() => portal.value?.content?.logo_url || '')
const heroTitle = computed(() => portal.value?.content?.welcome_title || content.value.welcome_title)
const heroText = computed(() => portal.value?.content?.welcome_text || welcomeDesc.value)
const entryLabel = computed(() => isPaid.value ? '进入工作' : '开始体验')
const editionLabel = computed(() => isPaid.value ? '正式版' : '免费体验版')
const portalStats = computed(() => {
  if (!isPaid.value || !portal.value) return stats.value
  const s = portal.value.stats
  return [
    { label: '今日工单', value: String(s.today_orders ?? 0), unit: '单' },
    { label: '在制订单', value: String(s.wip_orders ?? 0), unit: '单' },
    { label: '员工总数', value: String(s.employees ?? 0), unit: '人' },
    { label: '累计工单', value: String(s.total_orders ?? 0), unit: '单' },
  ]
})

async function loadPortal() {
  const code = tenantCode.value
  if (!code) return
  /* 登录用户（注册用户）：用 /me 补全显示名 */
  if (localStorage.getItem('clothp_access_token')) {
    try {
      const me = await api<{ user?: { employee_name?: string | null; login_name?: string } }>('/me', {}, { silent: true })
      accountName.value = me?.user?.employee_name || me?.user?.login_name || ''
    } catch { /* 令牌失效则按未登录处理 */ }
  }
  try {
    portal.value = await api<PortalInfo>(`/public/tenant-portal?code=${encodeURIComponent(code)}`, {}, { silent: true })
    if (portal.value?.trial_ends_at) trialEndsAt.value = portal.value.trial_ends_at
  } catch { /* 厂信息加载失败：退回体验主页默认内容 */ }
}

/* ===== 顶层菜单：当前手机号名下有多家制衣厂时可切换，进入工作台即进入当前制衣厂 ===== */
const factories = ref<PortalFactory[]>([])
const switchingFactory = ref(false)

async function loadFactories() {
  if (!localStorage.getItem('clothp_access_token')) { factories.value = []; return }
  try {
    factories.value = await myFactories()
  } catch { /* 请求失败时保留上次列表，避免顶层切换菜单因网络抖动消失 */ }
}

function onFactoryChange(event: Event) {
  const select = event.target as HTMLSelectElement
  void switchFactory(select.value)
}

async function switchFactory(code: string) {
  if (!code || code === tenantCode.value || switchingFactory.value) return
  const target = factories.value.find(f => f.code === code)
  switchingFactory.value = true
  try {
    const result = await switchTenant(code)
    /* 会话切到目标厂：门户内容与右侧“进入工作”都跟随该厂 */
    localStorage.removeItem('clothp_platform_access_token')
    localStorage.setItem('clothp_access_token', result.access_token)
    if (result.refresh_token) localStorage.setItem('clothp_refresh_token', result.refresh_token)
    else localStorage.removeItem('clothp_refresh_token')
    await router.replace({ path: route.path, query: { ...route.query, tenant: code } })
    /* 目标厂注册信息里没有手机号：先补录再继续 */
    syncNeedPhone(result.need_contact_phone, code)
    if (result.need_contact_phone) {
      await router.push('/complete-phone')
      return
    }
    await loadPortal()
    await loadFactories()
    toastSuccess(`已切换到「${target?.name || result.tenant?.name || code}」门户`)
  } catch (e) {
    toastError((e as Error).message || '切换制衣厂失败，请稍后重试')
    await loadFactories()
  } finally {
    switchingFactory.value = false
  }
}

/* 右侧悬浮按钮：体验用户=开始体验（换管理员会话进工作台）；注册用户=进入工作（直接进当前制衣厂工作台） */
async function onPrimaryEntry() {
  if (isPaid.value) {
    if (!localStorage.getItem('clothp_access_token')) { void router.push('/login'); return }
    void router.push('/workbench/A03')
    return
  }
  await startExperience()
}

/* ===== 体验页动态内容：平台控制面「体验页内容」编辑，这里只读展示 ===== */
interface PainPoint { title: string; desc: string }
interface TrialContent {
  welcome_title: string
  welcome_text: string
  intro_title: string
  pain_points: PainPoint[]
  solution_title: string
  solution_text: string
}
const content = ref<TrialContent>({
  welcome_title: '欢迎使用一智算制衣 MES',
  welcome_text: '免费体验已开通：这是一个真实的制衣厂数字化车间系统，订单、扫菲、计件工资一气呵成。',
  intro_title: '这些场景，您是不是也遇到过？',
  pain_points: [],
  solution_title: '一智算的解决方案',
  solution_text: '',
})

/* ===== 体验功能列表与点亮进度（体验到=1，未到=0） ===== */
interface TrialFeature { code: string; name: string; path: string; lit: number }
const features = ref<TrialFeature[]>([])
const trialEndsAt = ref(0)
const remainingDays = computed(() => {
  if (!trialEndsAt.value) return 30
  return Math.max(0, Math.ceil((trialEndsAt.value / 1000 - Date.now()) / 86400000))
})
const litCount = computed(() => features.value.filter(f => f.lit === 1).length)
/* 体验进度：点亮比例驱动主页内容（进度条与文案随体验深入而更新） */
const progressPct = computed(() => features.value.length ? Math.round(litCount.value / features.value.length * 100) : 0)
const progressStage = computed(() => {
  if (!features.value.length) return ''
  if (progressPct.value >= 80) return '体验已接近完整，可以联系平台转正式订阅了'
  if (progressPct.value >= 40) return '核心功能已上手，试试把真实订单录进来感受完整流程'
  return '刚起步：从「功能体验」区挑一项开始，每用一项就点亮一项'
})
const welcomeDesc = computed(() => {
  if (viaQr.value) return `已通过扫码确认登录「${qrAppName.value}」，已为您开启免费体验。`
  if (viaGuest.value) return '当前以游客身份浏览演示主页，游客不计入登录状态。开通体验厂后可保留体验进度。'
  return content.value.welcome_text
})

onMounted(async () => {
  /* 未通过验证码验证 / 扫码确认的访问直接回首页 */
  /* 登录后的制衣厂门户（?tenant=厂编码）无需体验会话；纯体验访问必须有验证过的会话 */
  const portalCode = String(route.query.tenant || '')
  if (!session && !portalCode) { void router.replace('/portal'); return }
  try {
    const r = await api<{ content: TrialContent }>('/public/trial-content', {}, { silent: true })
    if (r?.content) content.value = { ...content.value, ...r.content, pain_points: r.content.pain_points || [] }
  } catch { /* 使用内置默认内容 */ }
  await loadPortal()
  await loadFactories()
  if (tenantCode.value) {
    try {
      const r = await api<{ items: TrialFeature[]; status: string; trial_ends_at: number | null }>(
        `/public/trial-features?tenant=${encodeURIComponent(tenantCode.value)}`, {}, { silent: true })
      features.value = r.items || []
      trialEndsAt.value = r.trial_ends_at || trialEndsAt.value
    } catch { /* 功能列表加载失败时展示占位提示 */ }
  }
  void loadExtension()
  /* 从短信登录页重新验证后回来（/trial/home?enter=1）：自动点击一次“开始体验” */
  if (route.query.enter === '1' && session?.entryToken && tenantCode.value) {
    void startExperience()
  }
})

async function lightFeature(f: TrialFeature) {
  if (!tenantCode.value) {
    notice.value = '当前为演示浏览：开通体验厂后，每使用一个功能就会点亮一项。'
    return
  }
  if (f.lit === 1) { notice.value = `「${f.name}」已点亮`; return }
  try {
    await api('/public/trial-features/light', {
      method: 'POST',
      body: JSON.stringify({ tenant: tenantCode.value, code: f.code }),
    }, { silent: true })
    f.lit = 1
    notice.value = `「${f.name}」已点亮（${litCount.value}/${features.value.length}）`
  } catch (e) {
    notice.value = (e as Error).message || '点亮失败，请稍后重试'
  }
}

/* ===== 开始体验：进入该体验厂的 MES 工作台（以命中账号的身份与权限进入） ===== */
const entering = ref(false)
/* 普通员工（如厂长）首次进入：需输入厂管理员提供的初始密码 */
const trialPassword = ref('')
const needTrialPassword = ref(false)
const entryStatus = computed(() => session?.status || '')
const registerLink = computed(() => {
  const phone = session?.phone && isValidPhone(session.phone) ? `&phone=${session.phone}` : ''
  return `/register?mode=trial${phone}`
})

async function startExperience() {
  if (entering.value) return
  notice.value = ''
  /* 还没有体验厂：与正式注册用户一样，先提交体验类型的制衣厂注册申请 */
  if (!tenantCode.value) {
    await router.push(registerLink.value)
    return
  }
  entering.value = true
  try {
    const result = await trialEnter({
      entry_token: session?.entryToken,
      ticket: session?.ticket,
      phone: session?.phone,
      password: needTrialPassword.value ? trialPassword.value : undefined,
      tenant_code: tenantCode.value,
    })
    needTrialPassword.value = false
    trialPassword.value = ''
    localStorage.removeItem('clothp_platform_access_token')
    localStorage.setItem('clothp_access_token', result.access_token)
    if (result.refresh_token) localStorage.setItem('clothp_refresh_token', result.refresh_token)
    if (result.entry_token && session) {
      saveTrialSession({ ...session, entryToken: result.entry_token, status: 'approved' })
    }
    /* 旧数据体验厂注册信息里没有手机号：先补录再进工作台 */
    syncNeedPhone(result.need_contact_phone, tenantCode.value || result.tenant?.code)
    if (result.need_contact_phone) {
      await router.push('/complete-phone')
      return
    }
    await router.push(workspaceHome(result.permissions || []))
  } catch (e) {
    const error = e as Error & { code?: string }
    /* 失败必须立即可见：全局 Toast（右下角）+ 体验主页内的进度提示 */
    const show = (message: string) => { notice.value = message; toastError(message) }
    switch (error.code) {
      case 'TRIAL_PENDING':
        if (session) saveTrialSession({ ...session, status: 'pending' })
        show('体验制衣厂正在等待平台管理员审批…')
        setTimeout(() => { void router.push('/trial/pending') }, 600)
        break
      case 'NO_TRIAL_TENANT':
        show('该手机号还没有体验制衣厂，正在为你打开体验注册页…')
        setTimeout(() => { void router.push(registerLink.value) }, 900)
        break
      case 'TRIAL_EXPIRED':
        show('体验期已结束，请联系平台转为正式订阅后再进入工作台。')
        break
      case 'PASSWORD_REQUIRED':
        /* 普通员工首次进入：在页面底部密码条补录厂管理员提供的初始密码后重试 */
        needTrialPassword.value = true
        show(error.message || '首次进入需输入厂管理员提供的初始密码')
        break
      case 'INVALID_PASSWORD':
        needTrialPassword.value = true
        show(error.message || '首次登录密码不正确，请向厂管理员确认后重试')
        break
      case 'INVALID_ENTRY_TOKEN':
      case 'INVALID_TICKET':
      case 'MISSING_CREDENTIAL':
        show('体验凭证已失效，请重新获取手机验证码确认后自动进入工作台…')
        setTimeout(() => {
          const back = session?.phone && isValidPhone(session.phone) ? `&phone=${session.phone}` : ''
          void router.push(`/login?mode=sms${back}&enter=1`)
        }, 900)
        break
      default:
        show(error.message || '进入工作台失败，请稍后重试')
    }
  } finally {
    entering.value = false
  }
}
/* ===== 申请延长体验：体验厂管理员提交，平台管理员审批并指定新截止时间 ===== */
const extension = ref<TrialExtensionRequest | null>(null)
const extendReason = ref('')
const extendOpen = ref(false)
const extendBusy = ref(false)
const formatDate = (micros?: number | null) => micros ? new Date(micros / 1000).toLocaleDateString('zh-CN') : ''
const extensionText = computed(() => {
  const r = extension.value
  if (!r) return ''
  if (r.status === 'pending') return '延期申请审核中，请等待平台管理员处理'
  if (r.status === 'approved') return `延期已批准：体验期延长至 ${formatDate(r.new_trial_ends_at)}`
  if (r.status === 'rejected') return `延期申请未通过${r.review_reason ? '：' + r.review_reason : ''}`
  return ''
})

async function loadExtension() {
  if (!session?.entryToken || !tenantCode.value) return
  try {
    const r = await trialExtendStatus(session.entryToken, tenantCode.value)
    extension.value = r.request || null
    if (r.trial_ends_at) trialEndsAt.value = r.trial_ends_at
  } catch { /* 凭证过期等：静默，进度条仍用本地数据 */ }
}

async function submitExtension() {
  if (!session?.entryToken || !tenantCode.value) { toastError('体验凭证已失效，请重新用手机验证码登录'); return }
  if (extendBusy.value) return
  extendBusy.value = true
  try {
    const r = await trialExtend(session.entryToken, tenantCode.value, extendReason.value)
    toastSuccess(r.message || '延期申请已提交')
    extendOpen.value = false
    extendReason.value = ''
    await loadExtension()
  } catch (e) {
    toastError((e as Error).message || '延期申请失败，请稍后重试')
  } finally {
    extendBusy.value = false
  }
}

/* ===== 解决方案区的三种选择 ===== */
function continueTrial() {
  document.getElementById('modules')?.scrollIntoView({ behavior: 'smooth' })
}
/* 暂停：体验进度（含点亮进度与本厂账号）已保存在本机，下次打开同一浏览器继续 */
function pauseTrial() {
  notice.value = '体验进度已保存，下次打开同一浏览器即可继续。'
  setTimeout(() => { void router.push('/portal') }, 800)
}
/* 退出体验：清除本机体验会话 */
function exitTrial() {
  clearTrialSession()
  void router.push('/portal')
}
function logout() {
  /* 注册用户“退出登录”同时清除 MES 登录态；体验用户仅清体验会话 */
  if (isPaid.value) {
    localStorage.removeItem('clothp_access_token')
    localStorage.removeItem('clothp_refresh_token')
  }
  exitTrial()
}

const loginSource = computed(() => {
  if (viaQr.value) return `扫码登录 · ${qrAppName.value}`
  if (viaGuest.value) return '游客浏览 · 不保存登录态'
  if (viaAccount.value) return `账号登录 · ${displayName.value}`
  return tenantCode.value ? `体验厂 ${tenantCode.value}` : '演示环境'
})
const heroName = computed(() => viaQr.value ? qrAppName.value : '一智算')

const stats = computed(() => [
  { label: '体验剩余', value: String(remainingDays.value), unit: '天' },
  { label: '已点亮功能', value: String(litCount.value || 0), unit: `/${features.value.length || '-'} 项` },
  { label: '今日工单', value: '0', unit: '单' },
  { label: '在制订单', value: '0', unit: '单' },
])

function openModule(name: string) {
  notice.value = `「${name}」为体验功能入口，正式业务数据将在开通后接入。`
}
</script>

<template>
  <!-- 体验用户有体验会话；登记用户（收费厂）用 ?tenant=厂编码 进入，无体验会话 -->
  <div v-if="session || tenantCode" class="trial-home">
    <header class="th-nav">
      <div class="th-nav-inner">
        <!-- 顶部品牌：各家制衣厂用自己的 logo 与厂名；没有 logo 时用平台默认 logo -->
        <div class="th-brand">
          <img v-if="portalLogo" :src="portalLogo" alt="logo" class="th-brand-logo">
          <span v-else class="th-brand-mark">智</span>
          <span>{{ portalName }}<small>{{ editionLabel }}</small></span>
        </div>
        <nav class="th-links">
          <a href="#welcome">欢迎</a>
          <a href="#intro">功能介绍</a>
          <a href="#solution">解决方案</a>
          <a href="#modules">功能体验</a>
        </nav>
        <!-- 顶层菜单：一个手机号登记了多家制衣厂时，可直接切换当前门户 -->
        <div v-if="isPaid && factories.length > 1" class="th-factory">
          <span class="th-factory-label">当前制衣厂</span>
          <select :value="tenantCode" :disabled="switchingFactory" aria-label="切换制衣厂" @change="onFactoryChange">
            <option v-for="f in factories" :key="f.code" :value="f.code">{{ f.name }}（{{ f.code }}）</option>
          </select>
        </div>
        <div class="th-user">
          <span class="th-phone">{{ displayName }}</span>
          <span v-if="tenantCode" class="th-tenant-badge">{{ tenantCode }}</span>
          <button type="button" @click="logout">{{ isPaid ? '退出登录' : '退出体验' }}</button>
        </div>
      </div>
    </header>

    <!-- 第一段：欢迎词（厂管理员可改；未改时用平台默认内容） -->
    <section id="welcome" class="th-hero">
      <NetworkBackground theme="aurora" />
      <div class="th-hero-inner">
        <div class="th-welcome">
          <span class="th-badge">{{ isPaid ? '账号登录 · ' + portalName : loginSource }}</span>
          <h1>{{ heroTitle }}</h1>
          <p>{{ heroText }}</p>
          <div class="th-hero-actions">
            <a href="#intro" class="th-btn solid">看看能解决什么 ↓</a>
            <a href="#modules" class="th-btn soft">直接体验功能</a>
            <RouterLink v-if="!isPaid && tenantCode" to="/login" class="th-btn soft">以正式用户身份登录完整系统 →</RouterLink>
          </div>
          <div v-if="!isPaid && tenantCode && features.length" class="th-progress">
            <div class="th-progress-bar"><span :style="{ width: progressPct + '%' }"></span></div>
            <small>体验进度 {{ progressPct }}%（{{ litCount }}/{{ features.length }}）· {{ progressStage }}</small>
          </div>
        </div>
        <div class="th-stats">
          <div v-for="s in portalStats" :key="s.label" class="th-stat">
            <p class="th-stat-label">{{ s.label }}</p>
            <p class="th-stat-value">{{ s.value }}<small>{{ s.unit }}</small></p>
          </div>
        </div>
      </div>
    </section>

    <!-- 第二段：功能介绍（点出痛点、强化痛处；平台后台可编辑） -->
    <section id="intro" class="th-section">
      <div class="th-section-head">
        <h2>{{ content.intro_title }}</h2>
      </div>
      <div class="th-grid">
        <div v-for="(p, i) in content.pain_points" :key="i" class="th-pain">
          <span class="th-pain-no">{{ i + 1 }}</span>
          <h3>{{ p.title }}</h3>
          <p>{{ p.desc }}</p>
        </div>
      </div>
    </section>

    <!-- 第三段：解决方案（动态）+ 三种选择 -->
    <section id="solution" class="th-solution">
      <div class="th-section-head">
        <h2>{{ content.solution_title }}</h2>
        <p class="th-solution-text">{{ content.solution_text }}</p>
      </div>
      <div v-if="!isPaid" class="th-choices">
        <button type="button" class="th-choice primary" @click="continueTrial">
          <strong>① 继续体验</strong>
          <small>进入功能体验区，每使用一项功能即点亮一项</small>
        </button>
        <button type="button" class="th-choice" @click="pauseTrial">
          <strong>② 暂停，下次继续</strong>
          <small>体验进度已保存在本机，随时回来接着用</small>
        </button>
        <button type="button" class="th-choice danger" @click="exitTrial">
          <strong>③ 退出体验</strong>
          <small>清除本机体验会话并返回首页</small>
        </button>
      </div>
      <p v-if="tenantCode && !isPaid" class="th-cred-tip">
        <template v-if="entryStatus === 'pending'">体验制衣厂「{{ session?.trialName || tenantCode }}」已提交，等待平台管理员审核；通过后将以手机号 <strong>{{ session?.adminLogin || maskPhone(session?.phone || '') }}</strong> 作为该厂管理员账户。</template>
        <template v-else>体验厂管理员账户 <strong>{{ session?.adminLogin || maskPhone(session?.phone || '') }}</strong><template v-if="session?.adminPassword">，初始密码 <strong>{{ session.adminPassword }}</strong>（供本厂其他员工首次短信/微信登录使用）</template>。管理员本人使用手机号/微信登录，点击右侧“开始体验”即可进入工作台。</template>
      </p>
      <!-- 申请延长体验：体验厂管理员提交，平台管理员审批并确定新的体验截止时间 -->
      <div v-if="tenantCode && !isPaid && entryStatus !== 'pending'" class="th-extend">
        <div class="th-extend-info">
          <strong>需要更多体验时间？</strong>
          <span v-if="extensionText">{{ extensionText }}</span>
          <span v-else>体验期 {{ formatDate(trialEndsAt) }} 结束，可向平台管理员申请延长（由平台审批并确定新的截止时间）。</span>
        </div>
        <button v-if="extension?.status !== 'pending' && extension?.status !== 'approved'" type="button"
          class="th-btn soft" @click="extendOpen = !extendOpen">申请延长体验</button>
      </div>
      <div v-if="extendOpen" class="th-extend-form">
        <textarea v-model.trim="extendReason" rows="2" maxlength="200"
          placeholder="延期原因（选填，如：正在录入真实订单做完整测试）"></textarea>
        <button type="button" class="th-btn solid" :disabled="extendBusy" @click="submitExtension">
          {{ extendBusy ? '正在提交…' : '提交延期申请' }}
        </button>
      </div>
    </section>

    <!-- 功能体验区：体验功能列表 + 点亮进度 -->
    <section id="modules" class="th-modules">
      <div class="th-section-head">
        <h2>功能体验</h2>
        <p v-if="tenantCode">点击即可点亮功能：已点亮 {{ litCount }}/{{ features.length || 0 }} 项，点亮进度保留至体验结束。</p>
        <p v-else>当前为演示浏览，<RouterLink to="/register?mode=trial" class="th-link">开通体验厂</RouterLink>后可点亮功能并保留进度。</p>
      </div>
      <div class="th-grid">
        <button v-for="f in features" :key="f.code" type="button" class="th-module" :class="{ lit: f.lit === 1 }" @click="lightFeature(f)">
          <span class="th-module-name">{{ f.name }}</span>
          <span class="th-module-desc">{{ f.lit === 1 ? '已点亮 ✓' : '点击点亮' }}</span>
          <span class="th-module-tag" :class="{ on: f.lit === 1 }">{{ f.lit === 1 ? '1' : '0' }}</span>
        </button>
      </div>
      <p v-if="notice" class="th-notice">{{ notice }}</p>
      <div class="th-tip">
        <div>
          <h3>免费体验说明</h3>
          <p>体验厂为真实制衣厂记录（类型为体验用户，30 天有效）：手机号验证即自动开通，含管理员账号与本厂独立数据密钥；功能与页面与正式制衣厂相同，到期后可联系平台转正式订阅。</p>
        </div>
        <button type="button" class="th-btn solid" @click="continueTrial">继续体验 ↓</button>
      </div>
    </section>

    <footer class="th-footer">© 南宁一智万商科技有限责任公司 · 一智算{{ isPaid ? '制衣 MES' : '免费体验' }}</footer>
    <!-- 页面右侧中部悬浮：体验用户=开始体验 / 注册用户=进入工作，打开同一个工作台 -->
    <button type="button" class="th-start-fab" :disabled="entering" @click="onPrimaryEntry">
      <span class="th-start-fab-icon" aria-hidden="true">▶</span>
      <span class="th-start-fab-text">
        <strong>{{ entering ? '正在进入…' : entryLabel }}</strong>
        <small>{{ isPaid ? '进入一智算工作台' : '进入一智算工作台（体验）' }}</small>
      </span>
    </button>
    <!-- 普通员工（如厂长）首次进入体验厂：补录厂管理员提供的初始密码后进入本人工作页面 -->
    <div v-if="needTrialPassword" class="th-password-bar" role="dialog" aria-label="首次登录密码">
      <p class="th-password-tip">该手机号是本厂员工账号（如厂长），首次进入需输入厂管理员提供的初始密码，之后只需手机验证码。</p>
      <div class="th-password-row">
        <input v-model="trialPassword" type="password" autocomplete="off" placeholder="初始密码（厂管理员提供）"
          @keyup.enter="startExperience" />
        <button type="button" class="th-btn solid" :disabled="entering" @click="startExperience">
          {{ entering ? '正在进入…' : '进入工作台' }}
        </button>
        <button type="button" class="th-btn soft" @click="needTrialPassword = false; trialPassword = ''">取消</button>
      </div>
    </div>
    <BgAnimToggle />
  </div>
</template>

<style scoped>
.trial-home{min-height:100vh;color:#17255f;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;
  background:linear-gradient(160deg,#eaf3ff 0%,#f5f9ff 45%,#eef8fc 100%)}
/* 导航 */
.th-nav{position:sticky;top:0;z-index:50;background:rgba(255,255,255,.82);backdrop-filter:blur(12px);border-bottom:1px solid rgba(255,255,255,.9)}
.th-nav-inner{max-width:1200px;margin:0 auto;height:66px;padding:0 24px;display:flex;align-items:center;gap:28px}
.th-brand{display:flex;align-items:center;gap:10px;font-weight:800;font-size:19px}
.th-brand small{display:block;font-size:11px;font-weight:500;color:#7b8ab0}
.th-brand-mark{display:grid;place-items:center;width:36px;height:36px;border-radius:10px;color:#fff;font-size:15px;
  background:linear-gradient(135deg,#2563eb,#06b6d4);box-shadow:0 4px 12px rgba(37,99,235,.3)}
.th-brand-logo{width:36px;height:36px;border-radius:9px;object-fit:contain;background:#fff;border:1px solid #e3ecfb;padding:2px}
.th-links{display:flex;gap:24px;margin-left:auto}
.th-links a{font-size:14px;color:#41537e}.th-links a:hover{color:#2563eb}
.th-user{display:flex;align-items:center;gap:14px}
.th-phone{font-size:14px;color:#41537e;font-weight:600}
.th-tenant-badge{font-size:12px;font-weight:700;color:#1d4ed8;background:#dbeafe;border-radius:999px;padding:3px 10px}
.th-user button{border:1px solid #c3d5f2;background:rgba(255,255,255,.7);color:#1d4ed8;border-radius:10px;padding:8px 14px;font-size:13px;font-weight:600;cursor:pointer}
.th-user button:hover{border-color:#2563eb}
/* 顶层制衣厂切换 */
.th-factory{display:flex;align-items:center;gap:8px}
.th-factory-label{font-size:12.5px;color:#7b8ab0;white-space:nowrap}
.th-factory select{max-width:220px;border:1px solid #c3d5f2;background:rgba(255,255,255,.85);color:#1d4ed8;border-radius:10px;
  padding:7px 10px;font-size:13px;font-weight:600;font-family:inherit;cursor:pointer;outline:none}
.th-factory select:focus{border-color:#2563eb;box-shadow:0 0 0 3px rgba(37,99,235,.12)}
.th-factory select:disabled{opacity:.6;cursor:wait}
/* 按钮 */
.th-btn{display:inline-flex;align-items:center;justify-content:center;border-radius:10px;padding:11px 22px;font-size:14.5px;font-weight:600;border:1px solid transparent;white-space:nowrap;cursor:pointer}
.th-btn.solid{background:linear-gradient(90deg,#2563eb,#06b6d4);color:#fff;box-shadow:0 8px 20px rgba(37,99,235,.22)}
.th-btn.soft{background:rgba(255,255,255,.8);color:#1d4ed8;border-color:#dbe7fb}
.th-btn:hover{filter:brightness(1.05)}
/* 主视觉 */
.th-hero{position:relative;overflow:hidden}
.th-hero::after{content:"";position:absolute;left:0;right:0;bottom:0;height:80px;z-index:0;pointer-events:none;
  background:linear-gradient(180deg,rgba(234,243,255,0),#eaf3ff 100%)}
.th-hero-inner{position:relative;z-index:1;max-width:1200px;margin:0 auto;padding:64px 24px 76px;
  display:grid;grid-template-columns:1fr 1fr;gap:48px;align-items:center}
.th-badge{display:inline-block;border-radius:999px;padding:4px 12px;font-size:12px;font-weight:700;color:#1d4ed8;background:#dbeafe;margin-bottom:16px}
.th-welcome h1{font-size:38px;margin:0 0 14px;letter-spacing:-.01em}
.th-welcome h1 span{background:linear-gradient(90deg,#2563eb,#06b6d4);-webkit-background-clip:text;background-clip:text;color:transparent}
.th-welcome p{font-size:15.5px;line-height:1.9;color:#41537e;margin:0 0 26px;max-width:460px}
.th-hero-actions{display:flex;gap:12px;flex-wrap:wrap}
.th-progress{margin-top:14px;max-width:460px}
/* 申请延长体验 */
.th-extend{margin-top:14px;display:flex;align-items:center;gap:14px;flex-wrap:wrap;max-width:760px;
  background:rgba(255,255,255,.82);border:1px solid #e3ecfb;border-radius:12px;padding:12px 16px}
.th-extend-info{display:flex;flex-direction:column;gap:2px;font-size:12.5px;color:#475569}
.th-extend-info strong{font-size:13.5px;color:#17255f}
.th-extend-form{margin-top:10px;max-width:760px;display:flex;gap:10px;align-items:flex-start}
.th-extend-form textarea{flex:1;border:1px solid #cbd5e1;border-radius:10px;padding:10px 12px;font-size:13px;font-family:inherit;resize:vertical}
.th-extend-form textarea:focus{outline:2px solid #bfdbfe;border-color:#2563eb}
.th-progress-bar{height:8px;border-radius:999px;background:rgba(15,23,42,.12);overflow:hidden}
.th-progress-bar span{display:block;height:100%;border-radius:999px;background:linear-gradient(90deg,#2563eb,#06b6d4);transition:width .4s ease}
.th-progress small{display:block;margin-top:6px;font-size:12px;color:#475569}
.th-stats{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.th-stat{background:rgba(255,255,255,.82);backdrop-filter:blur(10px);border:1px solid rgba(255,255,255,.95);
  border-radius:16px;padding:22px 24px;box-shadow:0 12px 32px rgba(23,37,95,.10)}
.th-stat-label{font-size:13px;color:#7b8ab0;margin:0 0 8px}
.th-stat-value{font-size:30px;font-weight:800;margin:0;color:#17255f;font-variant-numeric:tabular-nums}
.th-stat-value small{font-size:13px;font-weight:600;color:#7b8ab0;margin-left:6px}
/* 痛点介绍 */
.th-section{max-width:1200px;margin:0 auto;padding:56px 24px 24px}
.th-section-head{text-align:center;margin:0 0 36px}
.th-section-head h2{font-size:30px;margin:0 0 10px}
.th-section-head p{font-size:15px;color:#7b8ab0;margin:0}
.th-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:18px}
.th-pain{position:relative;border:1px solid #f3d9d9;background:#fff;border-radius:18px;padding:26px 24px 22px;
  box-shadow:0 10px 26px rgba(23,37,95,.06)}
.th-pain-no{display:grid;place-items:center;width:30px;height:30px;border-radius:999px;color:#fff;font-weight:800;font-size:14px;
  background:linear-gradient(135deg,#e11d48,#f97316);margin-bottom:14px}
.th-pain h3{font-size:18px;margin:0 0 8px;color:#17255f}
.th-pain p{font-size:13.5px;line-height:1.8;color:#6b5a5a;margin:0}
/* 解决方案与三种选择 */
.th-solution{max-width:1200px;margin:0 auto;padding:48px 24px 16px}
.th-solution-text{max-width:760px;margin:0 auto;font-size:15px;line-height:1.9;color:#41537e}
.th-choices{display:grid;grid-template-columns:repeat(3,1fr);gap:18px;margin-top:30px}
.th-choice{display:flex;flex-direction:column;gap:8px;align-items:flex-start;text-align:left;border:1px solid #dbe7fb;background:#fff;
  border-radius:16px;padding:22px 24px;cursor:pointer;font-family:inherit;transition:transform .18s,box-shadow .18s;
  box-shadow:0 10px 26px rgba(23,37,95,.06)}
.th-choice:hover{transform:translateY(-3px);box-shadow:0 16px 34px rgba(23,37,95,.13)}
.th-choice.primary{border-color:#2563eb;background:linear-gradient(180deg,#eff6ff,#fff)}
.th-choice.primary strong{color:#1d4ed8}
.th-choice.danger strong{color:#b91c1c}
.th-choice strong{font-size:17px;color:#17255f}
.th-choice small{font-size:12.5px;line-height:1.7;color:#7b8ab0}
.th-cred-tip{text-align:center;font-size:12.5px;color:#7b8ab0;margin:18px 0 0}
/* 功能体验区 */
.th-modules{max-width:1200px;margin:0 auto;padding:48px 24px 80px}
.th-module{position:relative;text-align:left;border:1px solid #e3ecfb;background:#fff;border-radius:18px;padding:22px;
  box-shadow:0 10px 26px rgba(23,37,95,.06);cursor:pointer;transition:transform .18s,box-shadow .18s;font-family:inherit}
.th-module:hover{transform:translateY(-3px);box-shadow:0 16px 34px rgba(23,37,95,.13)}
.th-module.lit{border-color:#86efac;background:linear-gradient(180deg,#f0fdf4,#fff)}
.th-module-name{display:block;font-size:18px;font-weight:700;margin-bottom:6px;color:#17255f}
.th-module-desc{display:block;font-size:13.5px;line-height:1.7;color:#7b8ab0}
.th-module.lit .th-module-desc{color:#16a34a}
.th-module-tag{position:absolute;top:18px;right:18px;font-size:12px;font-weight:800;color:#94a3b8;background:#f1f5f9;border-radius:999px;padding:3px 10px}
.th-module-tag.on{color:#16a34a;background:#dcfce7}
.th-link{color:#1d4ed8;font-weight:600}
.th-notice{margin:20px 0 0;padding:12px 16px;border-radius:12px;background:#eff6ff;border:1px solid #c7dcff;font-size:14px;color:#1d4ed8;text-align:center}
.th-tip{margin-top:40px;display:flex;align-items:center;justify-content:space-between;gap:24px;
  background:#fff;border:1px solid #e3ecfb;border-radius:18px;padding:26px 30px;box-shadow:0 10px 26px rgba(23,37,95,.06)}
.th-tip h3{font-size:18px;margin:0 0 8px}
.th-tip p{font-size:13.5px;line-height:1.8;color:#7b8ab0;margin:0;max-width:760px}
.th-footer{background:#0d1b3e;color:#8fa3c8;text-align:center;padding:22px 24px;font-size:12.5px}
/* 右侧中部悬浮“开始体验” */
.th-start-fab{position:fixed;right:0;top:50%;transform:translateY(-50%);z-index:90;display:flex;align-items:center;gap:10px;
  border:1px solid rgba(255,255,255,.6);border-right:0;border-radius:16px 0 0 16px;padding:16px 18px 16px 16px;cursor:pointer;
  color:#fff;font-family:inherit;text-align:left;
  background:linear-gradient(135deg,#2563eb,#06b6d4);box-shadow:0 14px 34px rgba(37,99,235,.38);
  transition:transform .18s,box-shadow .18s,filter .18s}
.th-start-fab::before{content:"";position:absolute;inset:-8px;border-radius:22px 0 0 22px;z-index:-1;
  background:radial-gradient(circle at 30% 50%,rgba(37,99,235,.35),rgba(6,182,212,0) 70%);animation:thPulse 2.6s ease-in-out infinite}
.th-start-fab:hover{transform:translateY(-50%) translateX(-3px);box-shadow:0 18px 40px rgba(37,99,235,.48);filter:brightness(1.05)}
.th-start-fab:disabled{opacity:.72;cursor:wait}
.th-start-fab-icon{display:grid;place-items:center;width:34px;height:34px;border-radius:50%;flex-shrink:0;
  background:rgba(255,255,255,.2);font-size:13px;padding-left:2px}
.th-start-fab-text{display:flex;flex-direction:column;gap:2px}
.th-start-fab-text strong{font-size:17px;letter-spacing:.04em;white-space:nowrap}
.th-start-fab-text small{font-size:11px;color:rgba(255,255,255,.82);white-space:nowrap}
@keyframes thPulse{0%,100%{opacity:.55}50%{opacity:1}}
/* 首次进入密码条：固定在页面底部中部 */
.th-password-bar{position:fixed;left:50%;bottom:22px;transform:translateX(-50%);z-index:95;width:min(560px,calc(100% - 32px));
  background:rgba(255,255,255,.96);border:1px solid #c7dcff;border-radius:14px;padding:14px 16px;
  box-shadow:0 18px 44px rgba(23,37,95,.20)}
.th-password-tip{margin:0 0 10px;font-size:12.5px;line-height:1.7;color:#475569}
.th-password-row{display:flex;gap:10px;align-items:center}
.th-password-row input{flex:1;min-width:0;border:1px solid #cbd5e1;border-radius:10px;padding:10px 12px;font-size:14px;font-family:inherit;outline:none}
.th-password-row input:focus{border-color:#2563eb;box-shadow:0 0 0 3px rgba(37,99,235,.12)}
@media(max-width:600px){
  .th-start-fab{top:auto;bottom:18px;right:14px;transform:none;border-radius:14px;padding:12px 14px}
  .th-start-fab:hover{transform:none}
  .th-start-fab-text small{display:none}
}
@media(max-width:1020px){
  .th-hero-inner{grid-template-columns:1fr;gap:32px;padding:48px 20px 60px}
  .th-links{display:none}
  .th-grid{grid-template-columns:1fr 1fr}
  .th-choices{grid-template-columns:1fr}
}
@media(max-width:600px){
  .th-welcome h1{font-size:28px}
  .th-grid{grid-template-columns:1fr}
  .th-tip{flex-direction:column;align-items:flex-start;padding:20px}
  .th-nav-inner{padding:0 16px;gap:12px}
  .th-phone{font-size:12px}
  .th-factory-label{display:none}
  .th-factory select{max-width:140px}
}
</style>
