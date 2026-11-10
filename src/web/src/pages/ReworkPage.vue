<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import DatePicker from '../components/DatePicker.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('work_order:write'))
const orders = ref<any[]>([])
const inspections = ref<any[]>([])
const employees = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const current = ref<any>(null)
const form = reactive({ rework_no: '', inspection_id: '', qty: 1, due_date: '', reason: '' })
const assignForm = reactive({ employee_id: '', reason: '' })
const reinspectForm = reactive({ inspected_qty: 0, passed_qty: 0, rework_qty: 0, scrapped_qty: 0, remark: '' })
const reportForm = reactive({ qty: 0, remark: '' })

const statusText: Record<string, string> = { pending: '待分派', assigned: '返工中', completed: '已完成', cancelled: '已取消' }
const inspectionOptions = computed(() => inspections.value.filter(i => i.status === 'confirmed' && Number(i.rework_qty) > 0).map(i => ({ value: i.id, label: `${i.inspection_no} · 返工 ${i.rework_qty}` })))
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: `${e.code} · ${e.name}` })))
const currentInspection = computed(() => inspections.value.find(i => i.id === form.inspection_id))
const reinspectTotal = computed(() => Number(reinspectForm.passed_qty || 0) + Number(reinspectForm.rework_qty || 0) + Number(reinspectForm.scrapped_qty || 0))

async function load() {
  loading.value = true; error.value = ''
  try {
    const [r, i, e] = await Promise.all([api<{ items: any[] }>('/rework-orders'), api<{ items: any[] }>('/final-inspections'), api<{ items: any[] }>('/employees')])
    orders.value = r.items; inspections.value = i.items; employees.value = e.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, { rework_no: '', inspection_id: '', qty: 1, due_date: '', reason: '' })
  error.value = ''; modal.value = 'create'
}
async function createOrder() {
  if (!form.inspection_id || Number(form.qty) <= 0 || !form.reason.trim()) { error.value = '请选择总检单、填写返工数量和原因'; return }
  saving.value = true; error.value = ''
  try {
    await api('/rework-orders', { method: 'POST', body: JSON.stringify({ rework_no: form.rework_no.trim() || null, inspection_id: form.inspection_id, qty: Number(form.qty), due_at: form.due_date ? new Date(form.due_date).getTime() * 1000 : null, reason: form.reason.trim() }) })
    modal.value = ''; notice.value = '返工单已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function openAssign(item: any) {
  current.value = item; assignForm.employee_id = item.employee_id || ''; assignForm.reason = ''
  error.value = ''; modal.value = 'assign'
}
async function assign() {
  if (!current.value || !assignForm.employee_id) { error.value = '请选择返工人员'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/rework-orders/${current.value.id}/assign`, { method: 'POST', body: JSON.stringify({ employee_id: assignForm.employee_id, reason: assignForm.reason }) })
    modal.value = ''; notice.value = '返工单已分派'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function openReport(item: any) {
  current.value = item
  reportForm.qty = Math.max(0, Number(item.qty || 0) - Number(item.reported_qty || 0))
  reportForm.remark = ''
  error.value = ''; modal.value = 'report'
}
async function report() {
  if (!current.value || Number(reportForm.qty) <= 0) { error.value = '返工报工数量必须大于零'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/rework-orders/${current.value.id}/report`, { method: 'POST', body: JSON.stringify({ qty: Number(reportForm.qty), remark: reportForm.remark }) })
    modal.value = ''; notice.value = '返工报工已提交，请进行返工复检'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function openReinspect(item: any) {
  current.value = item
  Object.assign(reinspectForm, { inspected_qty: item.qty, passed_qty: item.qty, rework_qty: 0, scrapped_qty: 0, remark: '' })
  error.value = ''; modal.value = 'reinspect'
}
async function reinspect() {
  if (!current.value) return
  if (Number(reinspectForm.inspected_qty) !== reinspectTotal.value) { error.value = '复检数量必须等于合格+返工+报废'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/rework-orders/${current.value.id}/reinspections`, { method: 'POST', body: JSON.stringify({ inspected_qty: Number(reinspectForm.inspected_qty), passed_qty: Number(reinspectForm.passed_qty), rework_qty: Number(reinspectForm.rework_qty), scrapped_qty: Number(reinspectForm.scrapped_qty), remark: reinspectForm.remark }) })
    modal.value = ''; notice.value = '复检已记录，合格数已生成新的总检记录'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>返工与复检</h1><p>总检返工单分派、复检与合格回写</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button><button v-if="canWrite" class="button" @click="openCreate">+ 返工单</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><PageState :loading="loading"/>
    <div v-if="!loading && orders.length" class="table-wrap"><table><thead><tr><th>返工单号</th><th>来源单</th><th>工单</th><th>工序</th><th>款号</th><th>扎号</th><th>颜色/尺码</th><th>返工数</th><th>已报工</th><th>返工人</th><th>状态</th><th>原因</th><th>操作</th></tr></thead><tbody><tr v-for="item in orders" :key="item.id"><td><strong>{{ item.rework_no }}</strong></td><td>{{ item.inspection_no }}</td><td>{{ item.work_order_code }}</td><td>{{ item.process_code ? item.process_code + ' · ' + item.process_name : '—' }}</td><td>{{ item.product_code }}</td><td>{{ item.bundle_no }}</td><td>{{ item.color }}/{{ item.size }}</td><td>{{ item.qty }}</td><td>{{ item.reported_qty || 0 }}</td><td>{{ item.employee_name || '—' }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td>{{ item.reason }}</td><td><button v-if="canWrite && item.status !== 'completed' && item.status !== 'cancelled'" class="button link" @click="openAssign(item)">分派</button><button v-if="canWrite && item.status !== 'completed' && item.status !== 'cancelled' && Number(item.reported_qty || 0) < Number(item.qty || 0)" class="button link" @click="openReport(item)">报工</button><button v-if="canWrite && item.status !== 'completed' && item.status !== 'cancelled' && Number(item.reported_qty || 0) > 0" class="button link" @click="openReinspect(item)">复检</button></td></tr></tbody></table></div>
    <PageState v-if="!loading" :empty="!orders.length"/>
  </section>

  <ModalDialog v-if="modal === 'create'" title="新建返工单" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field full"><label class="required">总检记录（含返工数）</label><FilterSelect v-model="form.inspection_id" :options="inspectionOptions" placeholder="选择总检单"/></div>
      <div class="field"><label>返工单号</label><input v-model.trim="form.rework_no" placeholder="留空自动生成"></div>
      <div class="field"><label class="required">返工数量</label><input v-model.number="form.qty" type="number" min="1" :max="currentInspection?.rework_qty"></div>
      <div class="field"><label>要求完成日期</label><DatePicker v-model="form.due_date"/></div>
      <div class="field full"><label class="required">返工原因</label><input v-model.trim="form.reason" placeholder="如 袖口尺寸偏差"></div>
    </div>
    <div class="notice" v-if="currentInspection">该总检返工数：{{ currentInspection.rework_qty }}，合格 {{ currentInspection.passed_qty }}，报废 {{ currentInspection.scrapped_qty }}</div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="createOrder">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'assign'" title="分派返工" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid"><div class="field full"><label class="required">返工人员</label><FilterSelect v-model="assignForm.employee_id" :options="employeeOptions" placeholder="选择员工"/></div><div class="field full"><label>备注</label><input v-model.trim="assignForm.reason"></div></div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !assignForm.employee_id" @click="assign">确认分派</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'report'" title="返工报工" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="quantity-summary"><span>返工数<strong>{{ current?.qty }}</strong></span><span>已报工<strong>{{ current?.reported_qty || 0 }}</strong></span><span>剩余可报<strong>{{ Number(current?.qty || 0) - Number(current?.reported_qty || 0) }}</strong></span></div>
    <div class="form-grid"><div class="field"><label class="required">本次报工数量</label><input v-model.number="reportForm.qty" type="number" min="1" :max="Number(current?.qty || 0) - Number(current?.reported_qty || 0)"></div><div class="field full"><label>备注</label><input v-model.trim="reportForm.remark"></div></div>
    <div class="notice">报工后由质检进行返工复检；复检合格数会生成新的总检/巡检结果。</div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || Number(reportForm.qty) <= 0" @click="report">提交报工</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'reinspect'" title="返工复检" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="quantity-summary"><span>返工数<strong>{{ current?.qty }}</strong></span><span>复检合计<strong>{{ reinspectTotal }}</strong></span></div>
    <div class="form-grid"><div class="field"><label class="required">复检数量</label><input v-model.number="reinspectForm.inspected_qty" type="number" min="1"></div><div class="field"><label>合格</label><input v-model.number="reinspectForm.passed_qty" type="number" min="0"></div><div class="field"><label>继续返工</label><input v-model.number="reinspectForm.rework_qty" type="number" min="0"></div><div class="field"><label>报废</label><input v-model.number="reinspectForm.scrapped_qty" type="number" min="0"></div><div class="field full"><label>备注</label><textarea v-model="reinspectForm.remark" rows="2"></textarea></div></div>
    <div class="notice">合格数量会生成新的总检记录（原总检不覆盖），并按可入余额参与成品入库。</div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || Number(reinspectForm.inspected_qty) !== reinspectTotal" @click="reinspect">保存复检</button></template>
  </ModalDialog>
</template>
