<script setup lang="ts">
/**
 * 手机岗位首页（设计 6.6）：固定岗位入口，不显示切换账号入口。
 * 入口由后端权限集合驱动；直接请求 API 仍须被后端拒绝（前端隐藏只是展示）。
 */
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { confirmLogout } from '../session'

const userName = ref('')
const tenantName = ref('')
const personType = ref('')
const roleCodes = ref<string[]>([])
const entries = ref<{ key: string; label: string; route: string; icon: string }[]>([])

const ROLE_ENTRIES: Record<string, { key: string; label: string; route: string; icon: string; permission: string }[]> = {
  worker: [
    { key: 'scan', label: '扫码报工', route: '/s', icon: '⛶', permission: '' },
    { key: 'myreports', label: '我的报工', route: '/mywage/H02', icon: '◎', permission: '' },
    { key: 'mywage', label: '个人工资明细', route: '/mywage/H02', icon: '¥', permission: '' },
    { key: 'process', label: '工序查询', route: '/products/B01', icon: '◇', permission: 'product:read' },
  ],
  group_leader: [
    { key: 'dispatch', label: '派工', route: '/tasks/D01', icon: '✓', permission: 'task:read' },
    { key: 'approve', label: '待审批', route: '/reports/D06', icon: '☑', permission: 'report:read' },
    { key: 'members', label: '本组人员', route: '/settings/users', icon: '♙', permission: 'identity:read' },
    { key: 'progress', label: '本线工序进度', route: '/progress/F01', icon: '◫', permission: 'progress:read' },
  ],
  workshop_director: [
    { key: 'dashboard', label: '进度看板', route: '/workbench/A03', icon: '⌂', permission: 'workbench:read' },
    { key: 'approve', label: '报工审批', route: '/reports/D06', icon: '☑', permission: 'report:read' },
    { key: 'users', label: '人员与产线', route: '/settings/users', icon: '♙', permission: 'identity:read' },
    { key: 'lines', label: '产线管理', route: '/settings/lines', icon: '▤', permission: 'line:read' },
  ],
  warehouse_keeper: [
    { key: 'receipt', label: '物料入库', route: '/materials/receipts', icon: '▥', permission: 'work_order:read' },
    { key: 'issue', label: '生产领料', route: '/materials/receipts', icon: '▦', permission: 'work_order:read' },
  ],
  final_inspector: [
    { key: 'inspection', label: '总检记录', route: '/quality/E01', icon: '▣', permission: 'work_order:read' },
  ],
  factory_head: [
    { key: 'dashboard', label: '进度看板', route: '/workbench/A03', icon: '⌂', permission: 'workbench:read' },
    { key: 'progress', label: '进度与统计', route: '/progress/F01', icon: '◫', permission: 'progress:read' },
  ],
}

/* 新旧角色模板编码映射：平台新模板与旧租户 role.code 并存，统一归到岗位模板 */
const ROLE_ALIASES: Record<string, string> = {
  tenant_admin: 'factory_head',
  factory_manager: 'factory_head',
  production_supervisor: 'factory_head',
  process_engineer: 'factory_head',
  process_technician: 'factory_head',
  payroll_finance: 'factory_head',
  statistician: 'factory_head',
  manager: 'factory_head',
  FChief: 'factory_head',
  FM: 'factory_head',
  warehouse: 'warehouse_keeper',
  cutting_lead: 'group_leader',
}

async function load() {
  try {
    const [me, perms] = await Promise.all([
      api<{ user: { employee_name?: string | null; login_name: string; person_type?: string | null; role_codes?: string[] } }>('/me', {}, { silent: true }),
      api<{ actions: Record<string, boolean> }>('/me/permissions', {}, { silent: true }),
    ])
    userName.value = me.user.employee_name || me.user.login_name
    /* 顶部品牌显示本厂厂名，不出现产品名 */
    try {
      const brand = await api<{ tenant_name?: string }>('/tenant/branding', {}, { silent: true })
      tenantName.value = brand.tenant_name || ''
    } catch { /* 品牌加载失败只用中性标题 */ }
    /* 身份类型由 /me 返回当前登录人的 person_type，不再拉全厂人员列表取首条 */
    personType.value = me.user.person_type || ''
    roleCodes.value = me.user.role_codes || []
    /* 固定岗位入口：优先按有效角色编码匹配岗位模板（person_type 只有 manager/worker，
     * 无法区分组长、仓管、总检等岗位），无匹配时再按人员类型兜底；最后按权限过滤。 */
    const matched = roleCodes.value
      .map(code => ROLE_ALIASES[code] || code)
      .find(code => ROLE_ENTRIES[code])
    const template = (matched ? ROLE_ENTRIES[matched] : undefined)
      || (personType.value === 'worker' ? ROLE_ENTRIES.worker : ROLE_ENTRIES.factory_head)
      || ROLE_ENTRIES.worker
    entries.value = template.filter(e => !e.permission || perms.actions[e.permission])
  } catch {
    entries.value = []
  }
}

/* 注销当前账号：服务端吊销会话后回登录页 */
function logout() {
  void confirmLogout('退出后将注销当前账号，需要重新登录，确认退出？')
}

onMounted(load)
</script>

<template>
  <div class="mj-page">
    <header class="mj-header">
      <div class="mj-brand"><span class="mj-mark">{{ (tenantName || '厂').slice(0, 1) }}</span><strong>{{ tenantName || '本厂工作台' }}</strong></div>
      <div class="mj-user">{{ userName }} <button class="mj-logout" @click="logout">退出</button></div>
    </header>
    <main class="mj-main">
      <h2>岗位首页</h2>
      <div v-if="!entries.length" class="mj-empty">暂无可用的岗位入口，请联系厂管理员分配角色</div>
      <div v-else class="mj-grid">
        <RouterLink v-for="e in entries" :key="e.key" :to="e.route" class="mj-entry">
          <span class="mj-icon">{{ e.icon }}</span>
          <span>{{ e.label }}</span>
        </RouterLink>
      </div>
    </main>
  </div>
</template>

<style scoped>
.mj-page{min-height:100vh;background:linear-gradient(170deg,#eaf3ff,#f5f9ff);font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;color:#17255f}
.mj-header{display:flex;justify-content:space-between;align-items:center;padding:14px 18px;background:#fff;border-bottom:1px solid #e3ecfb}
.mj-brand{display:flex;align-items:center;gap:8px;font-size:15px}
.mj-mark{display:grid;place-items:center;width:26px;height:26px;border-radius:8px;color:#fff;font-size:12px;
  background:linear-gradient(135deg,#2563eb,#06b6d4)}
.mj-user{font-size:13px;color:#41537e;display:flex;align-items:center;gap:8px}
.mj-logout{border:1px solid #cbd5e1;background:#fff;border-radius:8px;padding:4px 10px;font-size:12px;color:#475569}
.mj-main{max-width:560px;margin:0 auto;padding:24px 18px}
.mj-main h2{font-size:18px;margin:0 0 16px}
.mj-empty{font-size:13px;color:#94a3b8;background:#fff;border:1px dashed #cbd5e1;border-radius:12px;padding:24px;text-align:center}
.mj-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:14px}
.mj-entry{display:flex;flex-direction:column;align-items:center;gap:10px;padding:22px 12px;background:#fff;border:1px solid #e3ecfb;
  border-radius:16px;font-size:14px;font-weight:600;color:#17255f;text-decoration:none;box-shadow:0 6px 16px rgba(23,37,95,.06)}
.mj-icon{font-size:26px}
</style>
