<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('work_order:write'))
const inspections = ref<any[]>([])
const workOrders = ref<any[]>([])
const bundles = ref<any[]>([])
const employees = ref<any[]>([])
const tasks = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref(false)
const form = reactive({ inspection_no: '', work_order_id: '', process_snapshot_id: '', bundle_id: '', inspected_qty: 1, passed_qty: 1, rework_qty: 0, scrapped_qty: 0, inspector_employee_id: '', remark: '' })

const workOrderOptions = computed(() => workOrders.value.map(w => ({ value: w.id, label: `${w.work_order_code} · ${w.product_name}` })))
const processOptions = computed(() => tasks.value.map(t => ({ value: t.process_snapshot_id, label: `${t.process_code} · ${t.process_name}` })))
const bundleOptions = computed(() => bundles.value.filter(b => !form.work_order_id || b.work_order_id === form.work_order_id).map(b => ({ value: b.id, label: `扎 ${b.bundle_no} · ${b.color}/${b.size} · ${b.current_qty}件` })))
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: `${e.code} · ${e.name}` })))
const total = computed(() => Number(form.passed_qty || 0) + Number(form.rework_qty || 0) + Number(form.scrapped_qty || 0))
const statusTextMap: Record<string, string> = { pending: '待确认', confirmed: '已确认' }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [pi, wo, b, e] = await Promise.all([
      api<{ items: any[] }>('/process-inspections'),
      api<{ items: any[] }>('/work-orders'),
      api<{ items: any[] }>('/bundles'),
      api<{ items: any[] }>('/employees'),
    ])
    inspections.value = pi.items; workOrders.value = wo.items; bundles.value = b.items; employees.value = e.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

async function loadTasks() {
  tasks.value = []
  form.process_snapshot_id = ''
  if (!form.work_order_id) return
  try {
    const data = await api<{ items: any[] }>('/production-tasks?work_order_id=' + form.work_order_id)
    tasks.value = data.items.filter(t => !t.parent_task_id)
    if (tasks.value.length) form.process_snapshot_id = tasks.value[0].process_snapshot_id
    const bundle = bundles.value.find(b => b.work_order_id === form.work_order_id)
    if (bundle) { form.bundle_id = bundle.id; setBundleQty(bundle) }
  } catch (e) { error.value = (e as Error).message }
}

function setBundleQty(bundle: any) {
  const qty = Number(bundle.current_qty || 1)
  form.inspected_qty = qty; form.passed_qty = qty; form.rework_qty = 0; form.scrapped_qty = 0
}

function openCreate() {
  Object.assign(form, { inspection_no: '', work_order_id: '', process_snapshot_id: '', bundle_id: '', inspected_qty: 1, passed_qty: 1, rework_qty: 0, scrapped_qty: 0, inspector_employee_id: '', remark: '' })
  tasks.value = []; error.value = ''; modal.value = true
}

async function createInspection() {
  if (!form.work_order_id || !form.process_snapshot_id || !form.bundle_id || Number(form.inspected_qty) !== total.value) { error.value = '请选择工单、工序、扎，且巡检数=合格+返工+报废'; return }
  saving.value = true; error.value = ''
  try {
    await api('/process-inspections', { method: 'POST', body: JSON.stringify({ ...form, inspected_qty: Number(form.inspected_qty), passed_qty: Number(form.passed_qty), rework_qty: Number(form.rework_qty), scrapped_qty: Number(form.scrapped_qty) }) })
    modal.value = false; notice.value = '过程巡检已保存，请确认后生效'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

async function confirmInspection(item: any) {
  error.value = ''
  try { await api(`/process-inspections/${item.id}/confirm`, { method: 'POST', body: JSON.stringify({ version: item.version }) }); notice.value = '过程巡检已确认'; await load() } catch (e) { error.value = (e as Error).message }
}

async function createRework(item: any) {
  const qtyText = window.prompt(`返工数量（可返工 ${item.rework_qty}）`, String(item.rework_qty || 0))
  if (!qtyText) return
  const qty = Number(qtyText)
  if (!qty || qty <= 0) { error.value = '返工数量必须大于零'; return }
  const reason = window.prompt('返工原因', '巡检发现尺寸偏差')
  if (!reason) return
  error.value = ''
  try {
    const result = await api<{ rework_no: string }>(`/process-inspections/${item.id}/rework`, { method: 'POST', body: JSON.stringify({ qty, reason }) })
    notice.value = `返工单已创建：${result.rework_no}，请到“返工复检”分派和报工`; await load()
  } catch (e) { error.value = (e as Error).message }
}

onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>过程巡检</h1><p>按工单工序和扎记录巡检结果，不合格可生成返工单</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button><button v-if="canWrite" class="button" @click="openCreate">+ 巡检单</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap"><table><thead><tr><th>巡检单号</th><th>工单</th><th>工序</th><th>扎</th><th>巡检数</th><th>合格</th><th>返工</th><th>报废</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in inspections" :key="item.id"><td><strong>{{ item.inspection_no }}</strong></td><td>{{ item.work_order_code }}</td><td>{{ item.process_code }} · {{ item.process_name }}</td><td>{{ item.bundle_no }} · {{ item.color }}/{{ item.size }}</td><td>{{ item.inspected_qty }}</td><td>{{ item.passed_qty }}</td><td :class="{ 'text-danger': item.rework_qty > 0 }">{{ item.rework_qty }}</td><td>{{ item.scrapped_qty }}</td><td><span class="status" :class="item.status">{{ statusTextMap[item.status] || statusText[item.status] || item.status }}</span></td><td><button v-if="canWrite && item.status === 'pending'" class="button link" @click="confirmInspection(item)">确认</button><button v-if="canWrite && item.status === 'confirmed' && item.rework_qty > 0" class="button link text-danger" @click="createRework(item)">生成返工单</button></td></tr></tbody></table><PageState :empty="!inspections.length" /></div>
  </section>

  <ModalDialog v-if="modal" title="新增过程巡检" @close="modal = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label>巡检单号</label><input v-model.trim="form.inspection_no" placeholder="留空自动生成"></div>
      <div class="field"><label class="required">生产工单</label><FilterSelect v-model="form.work_order_id" :options="workOrderOptions" placeholder="选择工单" @change="loadTasks" /></div>
      <div class="field"><label class="required">工序</label><FilterSelect v-model="form.process_snapshot_id" :options="processOptions" placeholder="选择工序" /></div>
      <div class="field"><label class="required">扎</label><FilterSelect v-model="form.bundle_id" :options="bundleOptions" placeholder="选择扎" @change="(id:any) => { const b = bundles.find(x => x.id === id); if (b) setBundleQty(b) }" /></div>
      <div class="field"><label class="required">巡检数</label><input v-model.number="form.inspected_qty" type="number" min="1"></div>
      <div class="field"><label>合格</label><input v-model.number="form.passed_qty" type="number" min="0"></div>
      <div class="field"><label>返工</label><input v-model.number="form.rework_qty" type="number" min="0"></div>
      <div class="field"><label>报废</label><input v-model.number="form.scrapped_qty" type="number" min="0"></div>
      <div class="field"><label>巡检人</label><FilterSelect v-model="form.inspector_employee_id" :options="employeeOptions" placeholder="留空为当前用户" /></div>
      <div class="field full"><label>备注</label><input v-model.trim="form.remark"></div>
    </div>
    <div class="notice">巡检合计：{{ total }} / 巡检数 {{ form.inspected_qty }}；返工数大于 0 时，确认后可生成返工单。</div>
    <template #footer><button class="button secondary" @click="modal = false">取消</button><button class="button" :disabled="saving || Number(form.inspected_qty) !== total" @click="createInspection">保存</button></template>
  </ModalDialog>
</template>
