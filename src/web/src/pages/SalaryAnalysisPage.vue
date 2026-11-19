<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import PageState from '../components/PageState.vue'
import SceneBadge from '../components/SceneBadge.vue'

/* 分析中心 · 员工薪水分析（设计 6.12.2 / 6.12.9）：
 * 薪水情况（应发/实发/已付/未付）+ 工作量情况（合格件数/次品/报工次数/标准工时/合格率）。
 * 按场景分 Tab/筛选，数据范围由后端 salary:read 权限与 5.7 约束。 */
const rows = ref<any[]>([])
const scenes = ref<any[]>([])
const summary = ref<any[]>([])
const period = ref('')
const sceneType = ref('')
const keyword = ref('')
const loading = ref(false)
const error = ref('')

const filtered = computed(() => rows.value.filter(r =>
  !keyword.value || r.name.includes(keyword.value) || r.employee_no.includes(keyword.value)))
const totals = computed(() => filtered.value.reduce((acc, r) => ({
  gross: acc.gross + Number(r.gross_amount || 0), net: acc.net + Number(r.net_amount || 0),
  paid: acc.paid + Number(r.paid_amount || 0), good: acc.good + Number(r.good_qty || 0),
  defect: acc.defect + Number(r.defect_qty || 0), hours: acc.hours + Number(r.standard_work_hours || 0),
}), { gross: 0, net: 0, paid: 0, good: 0, defect: 0, hours: 0 }))

async function load() {
  loading.value = true; error.value = ''
  try {
    const [res, sc, sm] = await Promise.all([
      api<any>(`/analysis/salary?period=${encodeURIComponent(period.value)}&scene_type=${encodeURIComponent(sceneType.value)}`),
      api<{ items: any[] }>('/scenes', {}, { silent: true }),
      api<{ items: any[] }>('/analysis/scene-summary', {}, { silent: true }),
    ])
    rows.value = res.items || []; scenes.value = sc.items; summary.value = sm.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div><h2>员工薪水分析</h2><p class="sub">薪水情况 + 工作量情况 · 按业务场景筛选与统计</p></div>
      <div class="head-actions">
        <input v-model="keyword" placeholder="搜索工号/姓名" class="search">
        <input v-model="period" placeholder="工资周期 yyyy-MM" class="search">
        <select v-model="sceneType" class="filter"><option value="">全部场景</option>
          <option v-for="s in scenes" :key="s.scene_type" :value="s.scene_type">{{ s.label }}</option></select>
        <button type="button" class="btn-primary" @click="load">查询</button>
      </div>
    </header>

    <div class="kpi-grid">
      <div class="kpi"><span>应发合计</span><b>{{ totals.gross.toFixed(2) }}</b></div>
      <div class="kpi"><span>实发合计</span><b>{{ totals.net.toFixed(2) }}</b></div>
      <div class="kpi"><span>已付合计</span><b>{{ totals.paid.toFixed(2) }}</b></div>
      <div class="kpi"><span>合格件数</span><b>{{ totals.good }}</b></div>
      <div class="kpi"><span>标准工时</span><b>{{ totals.hours.toFixed(1) }} h</b></div>
    </div>

    <div v-if="summary.length" class="scene-summary">
      <h3>场景维度统计</h3>
      <div class="scene-chips">
        <span v-for="s in summary" :key="s.scene_type + s.scene_sub" class="scene-chip">
          <SceneBadge :scene-type="s.scene_type" :scene-sub="s.scene_sub" :outsource="s.outsource" compact />
          工单 {{ s.work_order_count }} · 计划 {{ s.planned_qty }} · 进行 {{ s.running_count }} · 完成 {{ s.completed_count }}
        </span>
      </div>
    </div>

    <p v-if="error" class="alert-error">{{ error }}</p>
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap">
      <table>
        <thead><tr>
          <th rowspan="2">工号</th><th rowspan="2">姓名</th><th colspan="4">薪水情况</th><th colspan="5">工作量情况</th>
        </tr><tr>
          <th>应发</th><th>实发</th><th>已付</th><th>工资单数</th>
          <th>合格件数</th><th>次品数</th><th>报工次数</th><th>标准工时</th><th>合格率</th>
        </tr></thead>
        <tbody>
          <tr v-for="r in filtered" :key="r.employee_id">
            <td>{{ r.employee_no }}</td><td><b>{{ r.name }}</b></td>
            <td>{{ Number(r.gross_amount).toFixed(2) }}</td><td>{{ Number(r.net_amount).toFixed(2) }}</td>
            <td>{{ Number(r.paid_amount).toFixed(2) }}</td><td>{{ r.sheet_count }}</td>
            <td>{{ r.good_qty }}</td><td>{{ r.defect_qty }}</td><td>{{ r.report_count }}</td>
            <td>{{ Number(r.standard_work_hours).toFixed(1) }} h</td>
            <td><span class="rate" :class="{ low: r.pass_rate < 0.95 }">{{ (r.pass_rate * 100).toFixed(1) }}%</span></td>
          </tr>
          <tr v-if="!filtered.length"><td colspan="11" class="empty">暂无数据</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.page-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
h2 { margin: 0; font-size: 18px; font-weight: 600; }
h3 { font-size: 14px; margin: 0 0 10px; }
.sub { margin: 4px 0 0; color: #8A9099; font-size: 12px; }
.head-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.search, .filter { padding: 6px 9px; border: 1px solid #DDE3EE; border-radius: 7px; font-size: 12px; }
.btn-primary { padding: 7px 14px; border: 0; border-radius: 7px; background: linear-gradient(135deg,#2F6BFF,#5B8BFF); color: #fff; font-size: 13px; cursor: pointer; }
.kpi-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; margin-bottom: 16px; }
.kpi { padding: 14px 16px; border: 1px solid #E5E8EF; border-radius: 11px; background: linear-gradient(135deg,#fff,#F7F9FF); }
.kpi span { display: block; color: #8A9099; font-size: 11px; margin-bottom: 6px; }
.kpi b { font-size: 20px; color: #1F2329; }
.scene-summary { margin-bottom: 16px; }
.scene-chips { display: flex; gap: 8px; flex-wrap: wrap; }
.scene-chip { display: inline-flex; align-items: center; gap: 8px; padding: 7px 11px; border: 1px solid #E5E8EF; border-radius: 9px; background: #fff; font-size: 11px; color: #4E5562; }
.alert-error { color: #DC2626; background: #FEF2F2; border: 1px solid #FECACA; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.rate { color: #16A34A; font-weight: 600; }
.rate.low { color: #DC2626; }
.empty { text-align: center; color: #8A9099; padding: 20px; }
th { background: #F5F7FA; }
</style>
