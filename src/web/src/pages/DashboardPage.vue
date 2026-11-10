<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import PageState from '../components/PageState.vue'

type Metric = string | number
const loading = ref(true)
const error = ref('')
const metrics = ref<Record<string, Metric>>({})
const workOrders = ref<any[]>([])
const tenantProfile = ref<any>(null)
const editionText: Record<string, string> = { basic: '基础版', standard: '标准版', pro: '专业版' }
const cards = [
  ['pending_release', '待下发工单', '需要裁床组处理'],
  ['in_production', '生产中工单', '当前有效生产任务'],
  ['unassigned_tasks', '待派工任务', '尚未分配生产人员'],
  ['pending_reviews', '待审批报工', '未计入有效工资'],
  ['today_wages', '今日有效工资', '仅已审批计件金额'],
]
async function load() {
  loading.value = true; error.value = ''
  try {
    const [board, progress] = await Promise.all([
      api<{ metrics: Record<string, Metric> }>('/workbench'),
      api<{ items: any[] }>('/progress/work-orders?limit=6'),
    ])
    metrics.value = board.metrics; workOrders.value = progress.items
    try { tenantProfile.value = await api<any>('/tenant/profile', {}, { silent: true }) } catch { tenantProfile.value = null }
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
onMounted(load)
</script>
<template>
  <div class="page-header"><div><h1>生产工作台</h1><p>实时掌握生产进度、待办事项与有效计件</p></div><div class="header-actions"><RouterLink to="/tasks/D01" class="button">进入报工</RouterLink></div></div>
  <div v-if="tenantProfile?.subscription" class="notice">
    {{ tenantProfile.name }}（{{ tenantProfile.code }}） · 订阅版本：<strong>{{ editionText[tenantProfile.subscription.edition] || tenantProfile.subscription.edition }}</strong>
    <template v-if="tenantProfile.user_quota.limit"> · 启用用户 {{ tenantProfile.user_quota.used }}/{{ tenantProfile.user_quota.limit }}</template>
    <RouterLink to="/tenant/profile" style="float:right">本厂信息 →</RouterLink>
  </div>
  <PageState :loading="loading" :error="error" @retry="load" />
  <template v-if="!loading && !error">
    <section class="metrics"><article v-for="card in cards" :key="card[0]" class="metric"><div class="metric-label">{{ card[1] }}<span>↗</span></div><div class="metric-value">{{ metrics[card[0]] ?? 0 }}</div><div class="metric-foot">{{ card[2] }}</div></article></section>
    <div class="dashboard-grid">
      <section class="panel"><div class="panel-header"><h2>工单生产进度</h2><RouterLink to="/progress/F01" class="button link">查看全部</RouterLink></div><div class="panel-body"><PageState :empty="!workOrders.length" /><div class="progress-list"><div v-for="order in workOrders" :key="order.id" class="progress-row"><span>{{ order.work_order_code }}<small class="text-muted" style="display:block">{{ order.product_name }}</small></span><div class="progress-track"><div class="progress-bar" :style="{ width: `${Math.min(order.progress_ratio * 100, 100)}%` }"></div></div><strong>{{ Math.round(order.progress_ratio * 100) }}%</strong></div></div></div></section>
      <section class="panel"><div class="panel-header"><h2>快捷操作</h2></div><div class="panel-body progress-list"><RouterLink to="/orders/C03" class="notice">工单下发 <span style="float:right">→</span></RouterLink><RouterLink to="/tasks/D01" class="notice">多人派工与扫码报工 <span style="float:right">→</span></RouterLink><RouterLink to="/reports/D06" class="notice">报工审批与纠错 <span style="float:right">→</span></RouterLink><div class="notice success">中心节点连接正常，业务数据已持久化到 PostgreSQL。</div></div></section>
    </div>
  </template>
</template>
