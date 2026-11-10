<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, command, formatTime, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'

const { has } = usePermissions()
const canApprove = computed(() => has('review:approve'))
const canCorrect = computed(() => has('review:correct'))
const canReverse = computed(() => has('report:reverse'))

const tab = ref<'pending' | 'all'>('pending')
const reports = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')
const current = ref<any>(null)
const saving = ref(false)
const correction = reactive({ employee_id: '', good_qty: 0, defect_qty: 0, unit_price: '', actual_work_seconds: null as number | null, reason: '' })
async function load() {
  loading.value = true; error.value = ''
  try { reports.value = (await api<{ items: any[] }>(tab.value === 'pending' ? '/report-reviews' : '/work-reports')).items } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function changeTab(value: typeof tab.value) { tab.value = value; load() }
async function approve(item: any) {
  error.value = ''
  try { await api(`/report-reviews/${item.id}/approve`, { method: 'POST', body: JSON.stringify({ version: 0, reason: '' }) }); notice.value = '报工已审批，有效产量和计件工资已更新'; await load() } catch (e) { error.value = (e as Error).message }
}
async function reject(item: any) {
  const reason = window.prompt('请输入驳回原因（驳回将释放任务占用）')
  if (!reason) return
  error.value = ''
  try { await api(`/report-reviews/${item.id}/reject`, { method: 'POST', body: JSON.stringify({ version: 0, reason }) }); notice.value = '报工已驳回，不形成有效产量或工资'; await load() } catch (e) { error.value = (e as Error).message }
}
function openCorrect(item: any) {
  current.value = item; correction.employee_id = ''; correction.good_qty = item.good_qty; correction.defect_qty = item.defect_qty; correction.unit_price = item.unit_price; correction.actual_work_seconds = null; correction.reason = ''
}
async function saveCorrection() {
  if (!current.value) return
  saving.value = true; error.value = ''
  try { await api(`/report-reviews/${current.value.id}/correct-approve`, { method: 'POST', body: JSON.stringify({ version: 0, good_qty: Number(correction.good_qty), defect_qty: Number(correction.defect_qty), unit_price: correction.unit_price, actual_work_seconds: correction.actual_work_seconds, reason: correction.reason }) }); current.value = null; notice.value = '纠错并审批成功，原值、新值和原因已写入审计'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function reverse(item: any) {
  const reason = window.prompt('已审批报工只能冲销。请输入冲销原因：')
  if (!reason) return
  try { await command(`/work-reports/${item.id}/reverse`, { version: 0, reason }); notice.value = '冲销成功，已追加反向数量与计件流水'; await load() } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
</script>
<template>
  <div class="page-header"><div><h1>报工与审批</h1><p>待审批不计有效工资，纠错保留原始快照和完整原因</p></div><button class="button secondary" @click="load">刷新</button></div>
  <div v-if="error && !current" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><div class="tabs"><button :class="{ active: tab === 'pending' }" @click="changeTab('pending')">待审批</button><button :class="{ active: tab === 'all' }" @click="changeTab('all')">全部报工</button></div><PageState :loading="loading" /><div v-if="!loading" class="table-wrap"><table><thead><tr><th>报工时间</th><th>人员/工序</th><th>良品</th><th>不良</th><th>工价</th><th>计件金额</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in reports" :key="item.id"><td>{{ formatTime(item.occurred_at) }}</td><td><strong>{{ item.employee_name }}</strong><small class="text-muted" style="display:block">{{ item.process_name }}</small></td><td>{{ item.good_qty }}</td><td :class="{ 'text-danger': item.defect_qty > 0 }">{{ item.defect_qty }}</td><td>{{ item.unit_price }}</td><td class="money">{{ item.piecework_amount || '—' }}<small v-if="item.review_status === 'pending'" class="text-muted" style="display:block">待审批不计工资</small></td><td><span class="status" :class="item.review_status">{{ statusText[item.review_status] }}</span></td><td><template v-if="item.review_status === 'pending'"><button v-if="canApprove" class="button link" @click="approve(item)">通过</button><button v-if="canCorrect" class="button link" @click="openCorrect(item)">纠错</button><button v-if="canApprove" class="button link text-danger" @click="reject(item)">驳回</button></template><button v-else-if="item.review_status === 'approved' && item.report_kind !== 'reversal' && canReverse" class="button link text-danger" @click="reverse(item)">冲销</button></td></tr></tbody></table><PageState :empty="!reports.length" /></div></section>
  <ModalDialog v-if="current" wide title="审批纠错 · 原值 / 新值对照" @close="current = null"><div class="quantity-summary"><span>原良品<strong>{{ current.good_qty }}</strong></span><span>原不良<strong>{{ current.defect_qty }}</strong></span><span>原工价<strong>{{ current.unit_price }}</strong></span><span>新计件预览<strong>{{ (Number(correction.good_qty || 0) * Number(correction.unit_price || 0)).toFixed(6) }}</strong></span></div><div v-if="error" class="notice error">{{ error }}</div><div class="form-grid"><div class="field"><label>新良品数量</label><input v-model.number="correction.good_qty" type="number" min="0"></div><div class="field"><label>新不良数量</label><input v-model.number="correction.defect_qty" type="number" min="0"></div><div class="field"><label>新生效工价</label><input v-model="correction.unit_price" inputmode="decimal"></div><div class="field"><label>实际总工时（秒）</label><input v-model.number="correction.actual_work_seconds" type="number" min="0"></div><div class="field full"><label class="required">纠错原因</label><textarea v-model="correction.reason" rows="3" placeholder="改人、改价、增量必须填写具体原因"></textarea></div></div><div class="notice">仅当前报工的生效快照会改变，不回写产品工序或其他员工报工。</div><template #footer><button class="button secondary" @click="current = null">取消</button><button v-if="canCorrect" class="button" :disabled="saving || !correction.reason" @click="saveCorrection">保存并审批</button></template></ModalDialog>
</template>
