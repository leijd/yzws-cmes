<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from './api'
import { confirmLogout } from './session'
import ToastHost from './components/ToastHost.vue'
import GuideAssistant from './components/GuideAssistant.vue'
import ModalDialog from './components/ModalDialog.vue'
import SensitiveRequestDetail from './components/SensitiveRequestDetail.vue'

const route = useRoute()
const collapsed = ref(false)
/* 路由尚未解析（matched 为空）时也按独立页处理，避免误判为业务页去请求 /me */
const isLogin = computed(() => route.matched.length === 0 || !!route.meta.standalone)
const permissions = ref<Record<string, boolean>>({})
const currentUserName = ref('')
/* 工作页面品牌：本厂配置的 logo 与系统名称（未配置时用平台默认） */
const branding = ref<{ system_name?: string | null; logo_url?: string | null }>({})
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
  { path: '/materials/receipts', label: '物料入库', icon: '▥', group: '生产管理', permission: 'work_order:read' },
  { path: '/materials/master', label: '物料管理', icon: '▦', group: '基础资料', permission: 'masterdata:read' },
  { path: '/rework/E03', label: '返工复检', icon: '⟳', group: '生产管理', permission: 'work_order:read' },
  { path: '/settings/workshops', label: '车间管理', icon: '⌂', group: '基础资料', permission: 'workshop:read' },
  { path: '/settings/lines', label: '产线管理', icon: '▤', group: '基础资料', permission: 'line:read' },
  { path: '/settings/users', label: '人员与组织', icon: '♟', group: '基础资料', permission: 'identity:read' },
  { path: '/masterdata/B06', label: '员工与账号', icon: '♙', group: '基础资料', permission: '' },
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
    branding.value = await api<{ system_name?: string | null; logo_url?: string | null }>('/tenant/branding', {}, { silent: true })
  } catch {
    branding.value = {}
  }
}

watch(isLogin, () => { void loadPermissions(); void loadBranding() }, { immediate: true })

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
    <aside class="sidebar">
      <div class="brand">
        <img v-if="branding.logo_url" class="brand-logo" :src="branding.logo_url" alt="本厂 logo">
        <span v-else class="brand-mark">智</span>
        <span v-if="!collapsed" class="brand-name">{{ branding.system_name || '一智算' }}</span>
      </div>
      <nav v-for="group in groups" :key="group">
        <div v-if="!collapsed" class="nav-group">{{ group }}</div>
        <RouterLink v-for="item in visibleNav.filter(n => n.group === group)" :key="item.path" :to="item.path" :title="item.label">
          <span class="nav-icon">{{ item.icon }}</span><span v-if="!collapsed">{{ item.label }}</span>
        </RouterLink>
      </nav>
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
.brand-name{max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.reminder-button{position:relative;border:1px solid #c7dcff;background:white;border-radius:8px;padding:6px 12px;color:#17255f;cursor:pointer;font-size:13px}
.reminder-badge{display:inline-block;margin-left:6px;min-width:18px;padding:0 5px;border-radius:9px;background:#e11d48;color:#fff;font-size:11px;line-height:18px;text-align:center}
</style>
