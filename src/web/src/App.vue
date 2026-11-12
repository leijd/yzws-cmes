<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from './api'
import ToastHost from './components/ToastHost.vue'
import GuideAssistant from './components/GuideAssistant.vue'

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
  { path: '/settings/G01', label: '功能设置', icon: '⚙', group: '系统管理', permission: 'settings:read' },
  { path: '/import-export/G09', label: '导入导出', icon: '⇅', group: '系统管理', permission: 'settings:read' },
  { path: '/backup/G10', label: '备份与同步', icon: '⛁', group: '系统管理', permission: 'settings:read' },
  { path: '/tenant/backups', label: '明文备份导出', icon: '⚿', group: '系统管理', permission: 'backup:export' },
  { path: '/audit/G08', label: '审计与字段', icon: '⌘', group: '系统管理', permission: 'settings:read' },
]
const visibleNav = computed(() => nav.filter(item => !item.permission || permissions.value[item.permission]))
const groups = computed(() => [...new Set(visibleNav.value.map(item => item.group))])
function logout() {
  localStorage.removeItem('clothp_access_token')
  localStorage.removeItem('clothp_refresh_token')
  window.location.href = '/login'
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
onMounted(() => window.addEventListener('clothp-branding-updated', onBrandingUpdated))
onBeforeUnmount(() => window.removeEventListener('clothp-branding-updated', onBrandingUpdated))
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
      <div class="topbar-right"><span class="sync-dot"></span><span class="hide-mobile">中心节点 · 已连接</span><button class="user-button" @click="logout">{{ currentUserName || '当前用户' }} · 退出</button></div>
    </header>
    <main class="content"><router-view /></main>
    <nav class="mobile-nav">
      <RouterLink v-for="item in visibleNav.slice(0, 5)" :key="item.path" :to="item.path"><span>{{ item.icon }}</span>{{ item.label.replace('与审批', '').replace('与统计', '') }}</RouterLink>
    </nav>
    <!-- 体验助手：悬浮开关 + 分步操作引导（动画箭头），按当前用户权限过滤步骤 -->
    <GuideAssistant :permissions="permissions" />
  </div>
  <ToastHost />
</template>

<style scoped>
.brand-logo{width:34px;height:34px;border-radius:9px;object-fit:contain;flex:none;background:rgba(255,255,255,.14)}
.brand-name{max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
</style>
