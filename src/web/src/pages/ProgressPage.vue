<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api, statusText, formatDuration, formatTime } from '../api'
import PageState from '../components/PageState.vue'
import MonthPicker from '../components/MonthPicker.vue'
import FilterSelect from '../components/FilterSelect.vue'

const tab = ref('work-orders')
const loading = ref(false)
const error = ref('')
const items = ref<any[]>([])
const matrix = ref<{ rows: any[]; columns: string[] }>({ rows: [], columns: [] })
const period = ref(new Date().toISOString().slice(0, 7))
const tabs = [['work-orders', '工单进度'], ['processes', '工序进度'], ['employees', '人员进度'], ['bundle-processes', '工序明细'], ['output', '产量统计'], ['worktime', '工时统计'], ['wages', '工资统计']]
const orders = ref<any[]>([])
const bundleFilter = reactive({ work_order_id: '', bundle_no: '', process_code: '' })

/* 工单进度（树形）：一级=产品/项目，二级=工单 */
const progressFilter = reactive({ project_code: '', product_name: '', work_order_code: '' })
const expanded = reactive<Record<string, boolean>>({})
const projectOrders = reactive<Record<string, any[]>>({})
const ordersLoading = reactive<Record<string, boolean>>({})

function pct(value: any): number {
  const n = Number(value || 0)
  return Math.max(0, Math.min(100, n * 100))
}
function fmtPct(value: any): string {
  return pct(value).toFixed(1) + '%'
}

async function loadOrders() {
  try { orders.value = (await api<{ items: any[] }>('/work-orders')).items } catch { orders.value = [] }
}

async function loadProgressTree() {
  const query = new URLSearchParams()
  if (progressFilter.project_code) query.set('project_code', progressFilter.project_code)
  if (progressFilter.product_name) query.set('product_name', progressFilter.product_name)
  if (progressFilter.work_order_code) query.set('work_order_code', progressFilter.work_order_code)
  const rows = (await api<{ items: any[] }>(`/progress/styles?${query.toString()}`)).items
  items.value = rows
  /* 展开状态联动：有工单号筛选时自动展开命中的项目 */
  if (progressFilter.work_order_code) {
    for (const row of rows) expanded[row.project_id] = true
  }
  await Promise.all(rows.filter(r => expanded[r.project_id]).map(r => loadProjectOrders(r.project_id)))
}

async function loadProjectOrders(projectId: string) {
  if (ordersLoading[projectId]) return
  ordersLoading[projectId] = true
  try {
    const detail = await api<{ work_orders: any[] }>(`/progress/styles/${projectId}`)
    let list = detail.work_orders || []
    if (progressFilter.work_order_code) {
      const kw = progressFilter.work_order_code.toLowerCase()
      list = list.filter(o => String(o.work_order_code || '').toLowerCase().includes(kw))
    }
    projectOrders[projectId] = list
  } catch {
    projectOrders[projectId] = []
  } finally {
    ordersLoading[projectId] = false
  }
}

async function toggleExpand(projectId: string) {
  expanded[projectId] = !expanded[projectId]
  if (expanded[projectId] && !projectOrders[projectId]) await loadProjectOrders(projectId)
}

async function load() {
  loading.value = true; error.value = ''
  try {
    if (tab.value === 'work-orders') await loadProgressTree()
    else if (['output', 'worktime', 'wages'].includes(tab.value)) matrix.value = await api(`/statistics/${tab.value}?period=${period.value}`)
    else if (tab.value === 'bundle-processes') {
      const query = new URLSearchParams()
      if (bundleFilter.work_order_id) query.set('work_order_id', bundleFilter.work_order_id)
      if (bundleFilter.bundle_no) query.set('bundle_no', bundleFilter.bundle_no)
      if (bundleFilter.process_code) query.set('process_code', bundleFilter.process_code)
      items.value = (await api<{ items: any[] }>(`/progress/bundle-processes?${query.toString()}`)).items
    } else items.value = (await api<{ items: any[] }>(`/progress/${tab.value}`)).items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function changeTab(value: string) { tab.value = value; load() }
onMounted(() => { loadOrders(); load() })
</script>
<template>
  <div class="page-header"><div><h1>进度与统计</h1><p>工单、工序、人员及日月矩阵使用同一审批有效口径</p></div><div class="header-actions"><MonthPicker v-if="['output','worktime','wages'].includes(tab)" v-model="period" @update:modelValue="load"/><button class="button secondary" @click="load">刷新</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <section class="panel"><div class="tabs"><button v-for="item in tabs" :class="{ active: tab === item[0] }" @click="changeTab(item[0])">{{ item[1] }}</button></div><PageState :loading="loading" />
    <!-- 工单进度：树形表格（一级产品/项目，二级工单） -->
    <div v-if="!loading && tab === 'work-orders'" class="panel-body">
      <div class="toolbar" style="padding:0 0 14px;border:0">
        <input v-model.trim="progressFilter.project_code" class="search-input" placeholder="项目号 / 产品号" @keyup.enter="load">
        <input v-model.trim="progressFilter.product_name" class="search-input" placeholder="产品名称" @keyup.enter="load">
        <input v-model.trim="progressFilter.work_order_code" class="search-input" placeholder="工单号" @keyup.enter="load">
        <button class="button secondary" @click="load">查询</button>
        <button class="button secondary" @click="progressFilter.project_code='';progressFilter.product_name='';progressFilter.work_order_code='';load()">重置</button>
      </div>
      <PageState :empty="!items.length" />
      <div v-if="items.length" class="tree-table">
        <div class="tree-head">
          <span>产品号 / 产品名称 / 客户名称</span><span class="tree-num">订单数</span><span class="tree-num">入库数</span><span class="tree-pct">项目进度</span>
        </div>
        <div v-for="row in items" :key="row.project_id" class="tree-group">
          <div class="tree-row parent" @click="toggleExpand(row.project_id)">
            <span class="tree-name">
              <span class="tree-name-main">
                <span class="tree-toggle">{{ expanded[row.project_id] ? '▾' : '▸' }}</span>
                <span class="tree-title"><strong>{{ row.project_code }}</strong> · {{ row.product_name }}</span>
              </span>
              <small class="text-muted tree-sub">{{ row.product_code }} · {{ row.customer_name || '—' }}</small>
            </span>
            <span class="tree-num">{{ row.order_qty }}</span>
            <span class="tree-num">{{ row.received_qty }}</span>
            <span class="tree-pct">{{ fmtPct(row.progress_ratio) }}</span>
          </div>
          <div class="tree-bar"><div class="progress-bar" :style="{ width: pct(row.progress_ratio) + '%' }"></div></div>
          <div v-if="expanded[row.project_id]" class="tree-children">
            <div v-if="ordersLoading[row.project_id]" class="text-muted" style="padding:8px 18px">加载工单…</div>
            <div v-else-if="!(projectOrders[row.project_id] || []).length" class="text-muted" style="padding:8px 18px">暂无工单</div>
            <div v-for="wo in (projectOrders[row.project_id] || [])" :key="wo.id" class="tree-row child">
              <span class="tree-name" style="padding-left:34px">工单 {{ wo.work_order_code }} <small class="text-muted">· {{ statusText[wo.status] || wo.status }}</small></span>
              <span class="tree-num">{{ wo.planned_product_qty }}</span>
              <span class="tree-num">{{ wo.approved_good_qty }}</span>
              <span class="tree-pct">{{ fmtPct(wo.progress_ratio) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
    <div v-if="!loading && ['processes','employees'].includes(tab)" class="table-wrap"><table><thead><tr><th>{{ tab === 'processes' ? '工序' : '人员' }}</th><th>任务数</th><th>计划量</th><th>审批良品</th><th>未审批</th><th>完成率</th></tr></thead><tbody><tr v-for="item in items"><td>{{ item.name }}</td><td>{{ item.task_count }}</td><td>{{ item.planned_qty }}</td><td>{{ item.approved_good_qty }}</td><td>{{ item.pending_qty }}</td><td class="tree-pct">{{ fmtPct(item.progress_ratio) }}</td></tr></tbody></table><PageState :empty="!items.length" /></div>
    <div v-if="!loading && ['output','worktime','wages'].includes(tab)" class="table-wrap"><table><thead><tr><th style="position:sticky;left:0">人员</th><th v-for="column in [...matrix.columns].sort()">{{ column.slice(5) }}</th></tr></thead><tbody><tr v-for="row in matrix.rows"><td style="position:sticky;left:0;background:white">{{ row.name }}</td><td v-for="column in [...matrix.columns].sort()">{{ tab === 'worktime' ? formatDuration(Number(row.values[column] || 0)) : (row.values[column] || '0') }}</td></tr></tbody></table><PageState :empty="!matrix.rows.length" /></div>
    <div v-if="!loading && tab === 'bundle-processes'" class="panel-body"><div class="toolbar" style="padding:0 0 14px;border:0"><FilterSelect v-model="bundleFilter.work_order_id" :options="[{value:'',label:'全部工单'}].concat(orders.map(o => ({ value: o.id, label: o.work_order_code })))" @change="load"/><input v-model.trim="bundleFilter.bundle_no" class="search-input" placeholder="扎号" @keyup.enter="load"><input v-model.trim="bundleFilter.process_code" class="search-input" placeholder="工序编码" @keyup.enter="load"><button class="button secondary" @click="load">查询</button></div><div class="table-wrap"><table><thead><tr><th>扎号</th><th>颜色/尺码</th><th>工序</th><th>作业人</th><th>良品</th><th>不良</th><th>状态</th><th>报工时间</th></tr></thead><tbody><tr v-for="(item, index) in items" :key="index"><td><strong>{{ item.bundle_no }}</strong></td><td>{{ item.color }}/{{ item.size }}</td><td>{{ item.process_code }} · {{ item.process_name }}</td><td>{{ item.employee_name }}</td><td>{{ item.good_qty }}</td><td>{{ item.defect_qty }}</td><td>{{ item.review_status }}</td><td>{{ item.reported_at ? formatTime(item.reported_at) : '—' }}</td></tr></tbody></table><PageState :empty="!items.length"/></div></div>
  </section>
</template>
<style scoped>
.tree-table { width: 100%; }
.tree-head, .tree-row { display: grid; grid-template-columns: minmax(220px, 1fr) 80px 80px 100px; gap: 12px; align-items: center; }
.tree-head { padding: 0 16px 10px; color: var(--muted); font-size: 13px; border-bottom: 1px solid var(--border); }
.tree-group { border-bottom: 1px solid var(--border); }
.tree-row { padding: 10px 16px; font-size: 15px; }
.tree-row.parent { cursor: pointer; }
.tree-row.parent:hover { background: #f5f9ff; }
.tree-row.child { background: #fafdf0; font-size: 14px; font-style: italic; color: #64748b; }
.tree-row.child .tree-name, .tree-row.child .tree-name small { color: #64748b; }
.tree-row.child .tree-pct { color: #64748b; font-weight: 400; font-size: 15px; }
.tree-name { min-width: 0; }
.tree-name-main { display: flex; align-items: center; }
.tree-toggle { display: inline-flex; align-items: center; justify-content: center; flex: 0 0 32px; width: 32px; font-size: 30px; line-height: 1; color: var(--primary); }
.tree-title { flex: 1; min-width: 0; }
.tree-sub { display: block; padding-left: 32px; }
.tree-num, .tree-pct { text-align: center; }
.tree-pct { font-size: 18px; font-weight: 700; color: var(--primary); }
.tree-bar { height: 8px; margin: 0 16px 8px; border-radius: 5px; background: #e6effc; overflow: hidden; }
.tree-bar .progress-bar { height: 100%; background: linear-gradient(90deg, #2563eb, #06b6d4); border-radius: 5px; }
.tree-children { padding-bottom: 8px; }
</style>
