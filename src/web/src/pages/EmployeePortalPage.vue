<script setup lang="ts">
/**
 * 员工门户（设计 6.5）：服务端固定模板，固定风格与文字，不提供编辑入口。
 * 按角色显示工作入口；工人进入手机岗位首页，管理人员进入 PC/H5 工作台。
 */
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import NetworkBackground from '../components/NetworkBackground.vue'

const loading = ref(true)
const error = ref('')
const userName = ref('')
const entries = ref<{ key: string; label: string; route: string; icon: string }[]>([])

/* 固定入口（设计 6.6 表）：由后端权限集合驱动，隐藏无权限入口仅用于展示 */
const ALL_ENTRIES: { key: string; label: string; route: string; icon: string; permission: string }[] = [
  { key: 'scan', label: '扫码报工', route: '/mobile/home', icon: '⛶', permission: '' },
  { key: 'myreports', label: '我的报工', route: '/mobile/home', icon: '◎', permission: '' },
  { key: 'mywage', label: '个人工资明细', route: '/mywage/H02', icon: '¥', permission: '' },
  { key: 'process', label: '工序查询', route: '/mobile/home', icon: '◇', permission: 'product:read' },
  { key: 'dispatch', label: '派工', route: '/mobile/home', icon: '✓', permission: 'task:read' },
  { key: 'approve', label: '待审批', route: '/reports/D06', icon: '☑', permission: 'report:read' },
  { key: 'members', label: '本组人员', route: '/settings/users', icon: '♙', permission: 'identity:read' },
  { key: 'progress', label: '本线工序进度', route: '/progress/F01', icon: '◫', permission: 'progress:read' },
  { key: 'dashboard', label: '进度看板', route: '/workbench/A03', icon: '⌂', permission: 'workbench:read' },
  { key: 'inspection', label: '总检记录', route: '/quality/E01', icon: '▣', permission: 'work_order:read' },
  { key: 'receipt', label: '物料入库', route: '/materials/receipts', icon: '▥', permission: 'work_order:read' },
]

async function load() {
  loading.value = true; error.value = ''
  try {
    const [me, perms] = await Promise.all([
      api<{ user: { employee_name?: string | null; login_name: string } }>('/me', {}, { silent: true }),
      api<{ actions: Record<string, boolean> }>('/me/permissions', {}, { silent: true }),
    ])
    userName.value = me.user.employee_name || me.user.login_name
    /* 固定模板：全部入口按权限展示（工人无 PC 权限时仅保留手机入口） */
    entries.value = ALL_ENTRIES.filter(e => !e.permission || perms.actions[e.permission])
  } catch (e) {
    error.value = (e as Error).message
  } finally { loading.value = false }
}

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 9) return '早上好'
  if (h < 12) return '上午好'
  if (h < 14) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
})

onMounted(load)
</script>

<template>
  <div class="ep-page">
    <NetworkBackground theme="aurora" />
    <div class="ep-card">
      <div class="ep-brand"><span class="ep-mark">智</span><strong>一智算 · 员工门户</strong></div>
      <h1>{{ greeting }}，{{ userName || '同事' }}</h1>
      <p class="ep-desc">以下为您的固定工作入口，按岗位权限显示；点击即可进入对应功能。</p>
      <div v-if="loading" class="ep-tip">正在加载…</div>
      <div v-else-if="error" class="ep-tip">{{ error }}</div>
      <div v-else class="ep-grid">
        <RouterLink v-for="e in entries" :key="e.key" :to="e.route" class="ep-entry">
          <span class="ep-icon">{{ e.icon }}</span>
          <span>{{ e.label }}</span>
        </RouterLink>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ep-page{min-height:100vh;display:grid;place-items:center;padding:32px 20px;position:relative;
  font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;color:#17255f;
  background:linear-gradient(160deg,#eaf3ff 0%,#f5f9ff 45%,#eef8fc 100%)}
.ep-card{position:relative;z-index:1;width:min(640px,100%);background:#fff;border:1px solid #e3ecfb;border-radius:20px;
  padding:32px 36px;box-shadow:0 18px 48px rgba(23,37,95,.10)}
.ep-brand{display:flex;align-items:center;gap:8px;font-size:14px;color:#41537e;margin-bottom:16px}
.ep-mark{display:grid;place-items:center;width:28px;height:28px;border-radius:9px;color:#fff;font-size:12px;
  background:linear-gradient(135deg,#2563eb,#06b6d4)}
h1{margin:0 0 8px;font-size:22px}
.ep-desc{margin:0 0 18px;font-size:13.5px;color:#475569;line-height:1.8}
.ep-tip{font-size:13px;color:#94a3b8;text-align:center;padding:12px}
.ep-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:12px}
.ep-entry{display:flex;flex-direction:column;align-items:center;gap:8px;padding:18px 10px;border:1px solid #e3ecfb;
  border-radius:14px;background:#f8fbff;color:#17255f;font-size:13.5px;font-weight:600;text-decoration:none;
  transition:transform .15s ease,box-shadow .15s ease}
.ep-entry:hover{transform:translateY(-2px);box-shadow:0 10px 24px rgba(37,99,235,.15);border-color:#bfdbfe}
.ep-icon{font-size:22px}
</style>
