<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, formatTime } from '../api'
import PageState from '../components/PageState.vue'
import MonthPicker from '../components/MonthPicker.vue'

const period = ref(new Date().toISOString().slice(0, 7))
const data = ref<any>(null)
const loading = ref(false)
const error = ref('')
const payrollStatusText: Record<string, string> = { draft: '草稿', confirmed: '待支付', partially_paid: '部分支付', paid: '全部支付' }

async function load() {
  loading.value = true; error.value = ''
  try { data.value = await api(`/my/wages?period=${period.value}`) } catch (e) { error.value = (e as Error).message; data.value = null } finally { loading.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>我的工资</h1><p>仅展示本人计件与工资发放情况，数据与工资财务口径一致</p></div><div class="header-actions"><MonthPicker v-model="period" @update:modelValue="load"/><button class="button secondary" @click="load">刷新</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <PageState :loading="loading"/>
  <template v-if="!loading && data">
    <div class="metrics" style="grid-template-columns:repeat(3,minmax(0,1fr))">
      <article class="metric"><div class="metric-label">计件合计（{{ data.piecework.length }} 笔）</div><div class="metric-value money">¥ {{ data.piecework_total }}</div><div class="metric-foot">{{ data.employee_name }} · {{ data.period }}</div></article>
      <article class="metric"><div class="metric-label">应发 / 实发</div><div class="metric-value money">¥ {{ data.sheet ? data.sheet.net_amount : '0.000000' }}</div><div class="metric-foot">{{ data.sheet ? ('应发 ' + data.sheet.gross_amount) : '工资单尚未生成' }}</div></article>
      <article class="metric"><div class="metric-label">已发 / 未发</div><div class="metric-value money">{{ data.sheet ? data.sheet.paid_amount : '0.000000' }}</div><div class="metric-foot">{{ data.sheet ? ('未发 ' + data.sheet.outstanding_amount + ' · ' + (payrollStatusText[data.sheet.status] || data.sheet.status)) : '—' }}</div></article>
    </div>
    <div class="dashboard-grid">
      <section class="panel"><div class="panel-header"><h2>计件明细</h2></div><PageState :empty="!data.piecework.length"/><div v-if="data.piecework.length" class="table-wrap"><table><thead><tr><th>时间</th><th>工序</th><th>数量</th><th>工价</th><th>金额</th><th>类型</th></tr></thead><tbody><tr v-for="item in data.piecework"><td>{{ formatTime(item.created_at) }}</td><td>{{ item.process_name }}</td><td>{{ item.qty }}</td><td>{{ item.unit_price }}</td><td class="money">¥ {{ item.amount }}</td><td>{{ item.entry_kind === 'confirmed' ? '确认计件' : item.entry_kind === 'reversal' ? '冲销' : item.entry_kind }}</td></tr></tbody></table></div></section>
      <section class="panel"><div class="panel-header"><h2>付款流水</h2></div><div class="panel-body"><PageState :empty="!data.sheet || !data.sheet.payments.length"/><div v-if="data.sheet && data.sheet.payments.length" class="table-wrap"><table><thead><tr><th>付款时间</th><th>金额</th><th>方式</th></tr></thead><tbody><tr v-for="p in data.sheet.payments"><td>{{ formatTime(p.paid_at) }}</td><td class="money">¥ {{ p.amount }}</td><td>{{ p.payment_method === 'bank_transfer' ? '银行转账' : p.payment_method }}</td></tr></tbody></table></div><div v-if="data.sheet" class="notice" style="margin-top:16px">工资单状态：{{ payrollStatusText[data.sheet.status] || data.sheet.status }}；应发 {{ data.sheet.gross_amount }}，实发 {{ data.sheet.net_amount }}，已发 {{ data.sheet.paid_amount }}，未发 {{ data.sheet.outstanding_amount }}。</div></div></section>
    </div>
  </template>
</template>
