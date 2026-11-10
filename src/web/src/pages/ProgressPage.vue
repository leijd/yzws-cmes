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
async function loadOrders() {
  try { orders.value = (await api<{ items: any[] }>('/work-orders')).items } catch { orders.value = [] }
}
async function load() {
  loading.value = true; error.value = ''
  try {
    if (['output', 'worktime', 'wages'].includes(tab.value)) matrix.value = await api(`/statistics/${tab.value}?period=${period.value}`)
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
    <div v-if="!loading && tab === 'work-orders'" class="panel-body"><PageState :empty="!items.length" /><div class="progress-list"><div v-for="item in items" class="progress-row"><span>{{ item.work_order_code }}<small class="text-muted" style="display:block">{{ item.product_name }} · {{ statusText[item.status] }}</small></span><div class="progress-track"><div class="progress-bar" :style="{ width: `${Math.min(item.progress_ratio * 100, 100)}%` }"></div></div><strong>{{ Math.round(item.progress_ratio * 100) }}%</strong></div></div></div>
    <div v-if="!loading && ['processes','employees'].includes(tab)" class="table-wrap"><table><thead><tr><th>{{ tab === 'processes' ? '工序' : '人员' }}</th><th>任务数</th><th>计划量</th><th>审批良品</th><th>未审批</th><th>完成率</th></tr></thead><tbody><tr v-for="item in items"><td>{{ item.name }}</td><td>{{ item.task_count }}</td><td>{{ item.planned_qty }}</td><td>{{ item.approved_good_qty }}</td><td>{{ item.pending_qty }}</td><td>{{ Math.round(item.progress_ratio * 100) }}%</td></tr></tbody></table><PageState :empty="!items.length" /></div>
    <div v-if="!loading && ['output','worktime','wages'].includes(tab)" class="table-wrap"><table><thead><tr><th style="position:sticky;left:0">人员</th><th v-for="column in [...matrix.columns].sort()">{{ column.slice(5) }}</th></tr></thead><tbody><tr v-for="row in matrix.rows"><td style="position:sticky;left:0;background:white">{{ row.name }}</td><td v-for="column in [...matrix.columns].sort()">{{ tab === 'worktime' ? formatDuration(Number(row.values[column] || 0)) : (row.values[column] || '0') }}</td></tr></tbody></table><PageState :empty="!matrix.rows.length" /></div>
    <div v-if="!loading && tab === 'bundle-processes'" class="panel-body"><div class="toolbar" style="padding:0 0 14px;border:0"><FilterSelect v-model="bundleFilter.work_order_id" :options="[{value:'',label:'全部工单'}].concat(orders.map(o => ({ value: o.id, label: o.work_order_code })))" @change="load"/><input v-model.trim="bundleFilter.bundle_no" class="search-input" placeholder="扎号" @keyup.enter="load"><input v-model.trim="bundleFilter.process_code" class="search-input" placeholder="工序编码" @keyup.enter="load"><button class="button secondary" @click="load">查询</button></div><div class="table-wrap"><table><thead><tr><th>扎号</th><th>颜色/尺码</th><th>工序</th><th>作业人</th><th>良品</th><th>不良</th><th>状态</th><th>报工时间</th></tr></thead><tbody><tr v-for="(item, index) in items" :key="index"><td><strong>{{ item.bundle_no }}</strong></td><td>{{ item.color }}/{{ item.size }}</td><td>{{ item.process_code }} · {{ item.process_name }}</td><td>{{ item.employee_name }}</td><td>{{ item.good_qty }}</td><td>{{ item.defect_qty }}</td><td>{{ item.review_status }}</td><td>{{ item.reported_at ? formatTime(item.reported_at) : '—' }}</td></tr></tbody></table><PageState :empty="!items.length"/></div></div>
  </section>
</template>
