<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from './api'
import { confirmLogout } from './session'
import ToastHost from './components/ToastHost.vue'
import GuideAssistant from './components/GuideAssistant.vue'
import HelpBot from './components/HelpBot.vue'
import ModalDialog from './components/ModalDialog.vue'
import SensitiveRequestDetail from './components/SensitiveRequestDetail.vue'

const route = useRoute()
const collapsed = ref(false)
/* 导航中的权限问答 Bot：按当前登录角色给出对应知识库解答 */
const helpBotOpen = ref(false)
const isPlatformAdmin = computed(() => !!route.meta.platform || route.path.startsWith('/platform'))
/* 仅平台管理页（/platform/tenants 等）提供浮动 Bot 入口；平台门户、登录页均不展示 */
const showPlatformHelp = computed(() => !!route.meta.platform && !route.meta.public)
/* 路由尚未解析（matched 为空）时也按独立页处理，避免误判为业务页去请求 /me */
const isLogin = computed(() => route.matched.length === 0 || !!route.meta.standalone)
const permissions = ref<Record<string, boolean>>({})
const canManageIdentity = computed(() => !!permissions.value['identity:write'])
const currentUserName = ref('')
/* 工作页面品牌：本厂配置的 logo 与系统名称（未配置时显示本厂厂名） */
const branding = ref<{ system_name?: string | null; tenant_name?: string | null; logo_url?: string | null }>({})
const nav = [
  { path: '/workbench/A03', label: '工作台', icon: '⌂', group: '生产管理', permission: 'workbench:read' },
  { path: '/notifications/A04', label: '消息与会话', icon: '✉', group: '生产管理', permission: '' },
  { path: '/orders/C03', label: '项目与工单', icon: '▤', group: '生产管理', permission: 'work_order:read' },
  { path: '/tasks/D01', label: '任务与派工', icon: '✓', group: '生产管理', permission: 'task:read' },
  { path: '/mywage/H02', label: '我的工资', icon: '¥', group: '生产管理', permission: '' },

  { path: '/reports/D06', label: '报工与审批', icon: '◎', group: '生产管理', permission: 'report:read' },
  { path: '/quality/E01', label: '质检与库存', icon: '▣', group: '生产管理', permission: 'work_order:read' },
  { path: '/quality/process-inspections', label: '过程巡检', icon: '◉', group: '生产管理', permission: 'work_order:read' },
  { path: '/payroll/F09', label: '工资单管理', icon: '¥', group: '分析中心', permission: 'payroll:read' },
  { path: '/progress/F01', label: '进度与统计', icon: '◫', group: '分析中心', permission: 'progress:read' },
  { path: '/products/B01', label: '产品与工序', icon: '◇', group: '基础资料', permission: 'product:read' },
  { path: '/markers/C11', label: '唛架管理', icon: '▤', group: '基础资料', permission: 'marker:read' },
  { path: '/print/C10', label: '菲票打印', icon: '⎙', group: '生产管理', permission: 'print:read' },
  { path: '/labels/C12', label: '扎标签', icon: '▥', group: '生产管理', permission: 'work_order:read' },
  { path: '/materials/receipts', label: '物料入库', icon: '▥', group: '生产管理', permission: 'inventory:read' },
  { path: '/materials/master', label: '物料管理', icon: '▦', group: '基础资料', permission: 'warehouse:read' },
  { path: '/rework/E03', label: '返工复检', icon: '⟳', group: '生产管理', permission: 'work_order:read' },
  { path: '/outsource/orders', label: '外协管理', icon: '⇄', group: '生产管理', permission: 'outsource:read' },
  { path: '/evidence', label: '巡厂与证据', icon: '◉', group: '生产管理', permission: 'video:view' },
  { path: '/attendance/screen', label: '考勤屏', icon: '▣', group: '生产管理', permission: 'attendance:screen' },
  { path: '/attendance/records', label: '简单考勤', icon: '⏱', group: '生产管理', permission: 'attendance:manage' },
  { path: '/settings/workshops', label: '车间管理', icon: '⌂', group: '基础资料', permission: 'workshop:read' },
  { path: '/settings/lines', label: '产线管理', icon: '▤', group: '基础资料', permission: 'line:read' },
  { path: '/settings/users', label: '人员与组织', icon: '♙', group: '基础资料', permission: 'identity:read' },
  { path: '/settings/teams', label: '生产组管理', icon: '♟', group: '基础资料', permission: 'team:read' },
  { path: '/masterdata/customers', label: '客户资料', icon: '◈', group: '基础资料', permission: 'masterdata:read' },
  { path: '/masterdata/B06', label: '员工信息', icon: '♙', group: '基础资料', permission: '' },
  { path: '/settings/dictionaries', label: '字典维护', icon: '☰', group: '基础资料', permission: 'masterdata:read' },
  { path: '/tenant/profile', label: '本厂信息', icon: '⌂', group: '系统管理', permission: '' },
  { path: '/portal/settings', label: '门户内容', icon: '▤', group: '系统管理', permission: '' },
  { path: '/identity/G05', label: '用户角色权限', icon: '⚷', group: '系统管理', permission: 'identity:read' },
  { path: '/identity/sensitive-requests', label: '敏感信息授权审批', icon: '✉', group: '系统管理', permission: 'identity:write' },
  { path: '/settings/G01', label: '功能设置', icon: '⚙', group: '系统管理', permission: 'settings:read' },
  { path: '/import-export/G09', label: '导入导出', icon: '⇅', group: '系统管理', permission: 'settings:read' },
  { path: '/backup/G10', label: '备份与同步', icon: '⛁', group: '系统管理', permission: 'settings:read' },
  { path: '/tenant/backups', label: '明文备份导出', icon: '⚿', group: '系统管理', permission: 'backup:export' },
  { path: '/audit/G08', label: '审计与字段', icon: '⌘', group: '系统管理', permission: 'settings:read' },
]
const visibleNav = computed(() => nav.filter(item => !item.permission || permissions.value[item.permission]))
const groups = computed(() => [...new Set(visibleNav.value.map(item => item.group))])
/* 从工作页面退出：服务端注销会话（吊销访问令牌与刷新令牌）后再回登录页 */
function logout() {
  void confirmLogout()
}
async function loadPermissions() {
  if (isLogin.value || !localStorage.getItem('clothp_access_token')) {
    permissions.value = {}
    currentUserName.value = ''
    return
  }
  try {
    /* 外壳信息的请求失败不弹提示，由各业务页自行处理 */
    const [permissionData, me] = await Promise.all([
      api<{ actions: Record<string, boolean> }>('/me/permissions', {}, { silent: true }),
      api<{ user: { login_name: string; employee_name?: string | null } }>('/me', {}, { silent: true }),
    ])
    permissions.value = permissionData.actions
    currentUserName.value = me.user.employee_name || me.user.login_name
  } catch {
    permissions.value = {}
    currentUserName.value = ''
  }
}
async function loadBranding() {
  if (isLogin.value || !localStorage.getItem('clothp_access_token')) {
    branding.value = {}
    return
  }
  try {
    branding.value = await api<{ system_name?: string | null; tenant_name?: string | null; logo_url?: string | null }>('/tenant/branding', {}, { silent: true })
  } catch {
    branding.value = {}
  }
}

watch(isLogin, () => { void loadPermissions(); void loadBranding(); void loadFactories() }, { immediate: true })

/* ===== 多制衣厂切换：名下多家厂时品牌区显示下拉，选择后换新会话并重载页面 ===== */
const factories = ref<{ code: string; name: string; current?: boolean; kind?: string }[]>([])
const currentFactoryCode = ref('')
const switchingFactory = ref(false)
async function loadFactories() {
  if (isLogin.value || !localStorage.getItem('clothp_access_token')) { factories.value = []; return }
  try {
    const r = await api<{ items: { code: string; name: string; current?: boolean; kind?: string }[] }>('/auth/my-factories', {}, { silent: true })
    factories.value = r.items || []
    currentFactoryCode.value = factories.value.find(f => f.current)?.code || ''
  } catch { factories.value = [] }
}
const brandMenuOpen = ref(false)
async function onFactoryChange(code: string) {
  brandMenuOpen.value = false
  if (!code || code === currentFactoryCode.value || switchingFactory.value) return
  switchingFactory.value = true
  try {
    const r = await api<{ access_token: string }>('/auth/switch-tenant', { method: 'POST', body: JSON.stringify({ tenant_code: code, node_id: 'web-local' }) }, { silent: true })
    localStorage.setItem('clothp_access_token', r.access_token)
    localStorage.removeItem('clothp_platform_access_token')
  } catch { /* 失败也重载，恢复原状 */ }
  location.reload()
}

/* 门户设置页保存 logo/系统名称后即时刷新左侧品牌区 */
function onBrandingUpdated() { void loadBranding() }

/* ===== 顶栏提醒：网页版点击后弹窗显示未读提醒；敏感信息授权申请可继续查看请求详情并审批 ===== */
const reminderOpen = ref(false)
const reminders = ref<any[]>([])
const reminderUnread = ref(0)
const reminderLoading = ref(false)
const reminderError = ref('')
const reminderDetailId = ref('')
const reminderGeneric = ref<any>(null)
const canReviewRequests = computed(() => !!permissions.value['identity:write'])
let reminderTimer: ReturnType<typeof setInterval> | undefined
const reminderTypeLabels: Record<string, string> = {
  sensitive_view_request: '敏感信息授权申请', sensitive_view_request_result: '授权申请结果',
}
async function loadReminders() {
  if (isLogin.value || !localStorage.getItem('clothp_access_token')) return
  reminderLoading.value = true; reminderError.value = ''
  try {
    const res = await api<{ items: any[]; unread_count: number }>('/notifications?read=false&limit=20', {}, { silent: true })
    reminders.value = res.items || []
    reminderUnread.value = res.unread_count || 0
  } catch { /* 外壳提醒加载失败不弹提示 */ } finally { reminderLoading.value = false }
}
function openReminders() { reminderOpen.value = true; void loadReminders() }
async function openReminderDetail(item: any) {
  reminderOpen.value = false
  try { await api('/notifications/read', { method: 'POST', body: JSON.stringify({ ids: [item.id] }) }, { silent: true }) } catch { /* 标记失败不阻塞查看 */ }
  if (item.type === 'sensitive_view_request' && item.owner_id) reminderDetailId.value = item.owner_id
  else reminderGeneric.value = item
  void loadReminders()
}
function onReminderReviewed() { reminderDetailId.value = ''; void loadReminders() }
/* 登录后从独立页切回工作台时立即刷新提醒（不用 immediate，避免 setup 阶段引用未初始化的函数） */
watch(isLogin, login => { if (!login) void loadReminders() })

onMounted(() => {
  window.addEventListener('clothp-branding-updated', onBrandingUpdated)
  void loadReminders()
  reminderTimer = setInterval(() => { void loadReminders() }, 30000)
})
onBeforeUnmount(() => {
  window.removeEventListener('clothp-branding-updated', onBrandingUpdated)
  if (reminderTimer) clearInterval(reminderTimer)
})
</script>

<template>
  <router-view v-if="isLogin" />
  <div v-else class="app-shell" :class="{ collapsed }">
    <aside class="sidebar" @click="brandMenuOpen = false">
      <div class="brand" :class="{ clickable: factories.length > 1 }"
        @click.stop="factories.length > 1 && (brandMenuOpen = !brandMenuOpen)">
        <img v-if="branding.logo_url" class="brand-logo" :src="branding.logo_url" alt="本厂 logo">
        <span v-else class="brand-mark">{{ (branding.tenant_name || '厂').slice(0, 1) }}</span>
        <template v-if="!collapsed">
          <span class="brand-name">{{ branding.system_name || branding.tenant_name || '生产管理' }}</span>
          <!-- 名下多家制衣厂：厂名右侧下拉箭头，点击弹出列表切换；单厂保持纯展示 -->
          <span v-if="factories.length > 1" class="brand-caret" :class="{ open: brandMenuOpen }">▾</span>
          <div v-if="brandMenuOpen && factories.length > 1" class="brand-menu" @click.stop>
            <button v-for="f in factories" :key="f.code" type="button" class="brand-menu-item"
              :class="{ current: f.code === currentFactoryCode }" :disabled="switchingFactory" @click="onFactoryChange(f.code)">
              {{ f.name }}<span v-if="f.code === currentFactoryCode"> · 当前</span>
            </button>
          </div>
        </template>
      </div>
      <nav v-for="group in groups" :key="group">
        <div v-if="!collapsed" class="nav-group">{{ group }}</div>
        <RouterLink v-for="item in visibleNav.filter(n => n.group === group)" :key="item.path" :to="item.path" :title="item.label">
          <span class="nav-icon">{{ item.icon }}</span><span v-if="!collapsed">{{ item.label }}</span>
        </RouterLink>
      </nav>
      <!-- 导航中的问题助手入口：平台管理员与制衣厂管理员排查「没有权限」 -->
      <button class="help-bot-entry" title="问题助手" @click="helpBotOpen = !helpBotOpen">
        <span class="hb-entry-icon">🤖</span><span v-if="!collapsed">问题助手</span>
      </button>
    </aside>
    <header class="topbar">
      <button class="icon-button" @click="collapsed = !collapsed" aria-label="折叠菜单">☰</button>
      <div class="topbar-title">服装生产管理平台</div>
      <div class="topbar-right"><span class="sync-dot"></span><span class="hide-mobile">中心节点 · 已连接</span><button class="reminder-button" title="查看提醒" @click="openReminders">✉ 提醒<span v-if="reminderUnread > 0" class="reminder-badge">{{ reminderUnread }}</span></button><button class="user-button" @click="logout" title="注销当前账号并返回登录页">{{ currentUserName || '当前用户' }} · 退出登录</button></div>
    </header>
    <main class="content"><router-view /></main>
    <nav class="mobile-nav">
      <RouterLink v-for="item in visibleNav.slice(0, 5)" :key="item.path" :to="item.path"><span>{{ item.icon }}</span>{{ item.label.replace('与审批', '').replace('与统计', '') }}</RouterLink>
    </nav>
    <!-- 体验助手：悬浮开关 + 分步操作引导（动画箭头），按当前用户权限过滤步骤 -->
    <GuideAssistant :permissions="permissions" />
  </div>
  <!-- 平台管理页为独立布局，单独提供右下角 Bot 入口；门户页/登录页不显示 -->
  <button v-if="showPlatformHelp && isLogin" class="help-bot-fab" title="问题助手" @click="helpBotOpen = !helpBotOpen">🤖 问题助手</button>
  <HelpBot :open="helpBotOpen && (showPlatformHelp || !isLogin)" :platform="isPlatformAdmin" :can-manage="canManageIdentity" @close="helpBotOpen = false" />
  <!-- 网页版提醒弹窗：点击顶栏「提醒」显示未读提醒列表，点详细查看请求信息 -->
  <ModalDialog v-if="reminderOpen" title="提醒消息" @close="reminderOpen = false">
    <div v-if="reminderError" class="notice error">{{ reminderError }}</div>
    <div v-if="reminderLoading" class="text-muted">加载中…</div>
    <div v-else-if="!reminders.length" class="notice">暂无未读提醒。</div>
    <div v-for="item in reminders" :key="item.id" class="notice" style="display:flex;align-items:flex-start;justify-content:space-between;gap:12px">
      <div><strong>{{ item.title }}</strong><div class="text-muted" style="font-size:12px;margin-top:4px">{{ reminderTypeLabels[item.type] || item.type }}</div><div style="font-size:13px;margin-top:6px">{{ item.body }}</div></div>
      <button class="button link" style="flex:none" @click="openReminderDetail(item)">详细</button>
    </div>
    <template #footer>
      <RouterLink to="/notifications/A04" class="button secondary" @click="reminderOpen = false">查看全部消息</RouterLink>
      <button class="button" @click="reminderOpen = false">关闭</button>
    </template>
  </ModalDialog>

  <ModalDialog v-if="reminderGeneric" :title="reminderGeneric.title" @close="reminderGeneric = null">
    <p style="font-size:14px;line-height:1.8;white-space:pre-wrap">{{ reminderGeneric.body }}</p>
    <template #footer><button class="button" @click="reminderGeneric = null">知道了</button></template>
  </ModalDialog>
  <SensitiveRequestDetail v-if="reminderDetailId" :request-id="reminderDetailId" :can-review="canReviewRequests" @close="reminderDetailId = ''" @reviewed="onReminderReviewed" />

  <ToastHost />
</template>

<style scoped>
.brand-logo{width:34px;height:34px;border-radius:9px;object-fit:contain;flex:none;background:rgba(255,255,255,.14)}
.brand{position:relative}
.brand.clickable{cursor:pointer}
.brand-name{max-width:130px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.brand-caret{margin-left:.3rem;color:#9db4e8;font-size:12px;transition:transform .15s}
.brand-caret.open{transform:rotate(180deg)}
.brand-menu{position:absolute;top:calc(100% + 8px);left:0;min-width:180px;background:#fff;border:1px solid #e3e9f4;border-radius:12px;box-shadow:0 12px 32px rgba(2,12,40,.35);padding:6px;z-index:80}
.brand-menu-item{display:block;width:100%;text-align:left;padding:9px 12px;border:0;background:transparent;border-radius:8px;font-size:13px;color:#24365e;cursor:pointer}
.brand-menu-item:hover{background:#f2f6ff}
.brand-menu-item.current{color:#2563eb;font-weight:600}
.reminder-button{position:relative;border:1px solid #c7dcff;background:white;border-radius:8px;padding:6px 12px;color:#17255f;cursor:pointer;font-size:13px}
.reminder-badge{display:inline-block;margin-left:6px;min-width:18px;padding:0 5px;border-radius:9px;background:#e11d48;color:#fff;font-size:11px;line-height:18px;text-align:center}
.help-bot-entry{position:sticky;bottom:0;width:100%;margin-top:18px;display:flex;align-items:center;gap:10px;border:1px solid rgba(255,255,255,.18);border-radius:10px;background:rgba(37,99,235,.28);color:#e8f0ff;padding:10px 12px;font-size:13px;text-align:left}
.help-bot-entry:hover{background:rgba(37,99,235,.55)}
.hb-entry-icon{font-size:16px;flex:none}
.help-bot-fab{position:fixed;right:24px;bottom:24px;z-index:95;border:0;border-radius:999px;padding:11px 18px;background:linear-gradient(120deg,#102a56,#2563eb);color:#fff;font-size:13px;font-weight:600;box-shadow:0 10px 26px rgba(23,37,95,.28)}
.help-bot-fab:hover{filter:brightness(1.1)}
</style>
