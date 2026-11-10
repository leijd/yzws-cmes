<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, statusText, uuid } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canAssign = computed(() => has('task:assign'))
const canReport = computed(() => has('report:create'))

const tasks = ref<any[]>([])
const employees = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const filter = ref('all')
const modal = ref<'assign' | 'bundle-assign' | 'report' | ''>('')
const current = ref<any>(null)
const selected = reactive<Record<string, boolean>>({})
const quantities = reactive<Record<string, number>>({})
const report = reactive({ bundle_id: '', good_qty: 0, defect_qty: 0, actual_work_seconds: null as number | null, remark: '', device_code: '' })
const reportKey = ref('')
const bundles = ref<any[]>([])
const meEmployeeId = ref('')
const currentBundle = computed(() => bundles.value.find(b => b.id === report.bundle_id))
const isModeB = computed(() => current.value?.report_mode === 'B')
const maxQty = computed(() => isModeB.value ? Number(current.value?.remaining_qty || 0) : Number(currentBundle.value?.current_qty ?? current.value?.remaining_qty ?? 0))
const bundleOptions = computed(() => bundles.value.map(b => ({ value: b.id, label: `${b.bundle_no} · ${b.color}/${b.size} · ${b.current_qty} 件` })))
function onBundleChange() { if (currentBundle.value) report.good_qty = currentBundle.value.current_qty }

const bundleRows = ref<any[]>([])
const bundleSelected = reactive<Record<string, boolean>>({})
const bundleQty = reactive<Record<string, number>>({})
const bundleEmployee = ref('')
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: `${e.code} · ${e.name}` })))
async function openBundleAssign(task: any) {
  current.value = task; error.value = ''
  Object.keys(bundleSelected).forEach(k => delete bundleSelected[k])
  Object.keys(bundleQty).forEach(k => delete bundleQty[k])
  bundleEmployee.value = ''; bundleRows.value = []
  modal.value = 'bundle-assign'
  try {
    if (!employees.value.length) employees.value = (await api<{ items: any[] }>('/employees')).items
    const items = (await api<{ items: any[] }>(`/work-orders/${task.work_order_id}/assignable-bundles`)).items
    bundleRows.value = items.filter((r: any) => r.task_id === task.id && r.assignable_qty > 0)
    for (const row of bundleRows.value) bundleQty[row.bundle_id] = row.assignable_qty
  } catch (e) { error.value = (e as Error).message }
}
async function submitBundleAssign() {
  if (!current.value) return
  const items = bundleRows.value.filter(r => bundleSelected[r.bundle_id]).map(r => ({ bundle_id: r.bundle_id, employee_id: bundleEmployee.value, planned_qty: Number(bundleQty[r.bundle_id] || r.assignable_qty) }))
  if (!items.length) { error.value = '请选择至少一个扎'; return }
  if (!bundleEmployee.value) { error.value = '请选择派工员工'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/production-tasks/${current.value.id}/bundle-assignments`, { method: 'POST', body: JSON.stringify({ version: current.value.version, items }) })
    modal.value = ''; notice.value = `扎级派工成功，共 ${items.length} 个扎`; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
const filtered = computed(() => tasks.value.filter(task => filter.value === 'all' || (filter.value === 'unassigned' ? !task.parent_task_id && task.status === 'unassigned' : !!task.parent_task_id)))
const assignUsed = computed(() => current.value ? tasks.value.filter(t => t.parent_task_id === current.value.id).reduce((sum, t) => sum + t.planned_qty, 0) : 0)
const assignRemaining = computed(() => current.value ? current.value.planned_qty - assignUsed.value : 0)
const assignedTotal = computed(() => Object.entries(selected).filter(([, yes]) => yes).reduce((sum, [id]) => sum + Number(quantities[id] || 0), 0))

async function load() {
  loading.value = true; error.value = ''
  try { const taskData = await api<{ items: any[] }>('/production-tasks'); tasks.value = taskData.items } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function ensureEmployees() {
  if (employees.value.length) return true
  try { employees.value = (await api<{ items: any[] }>('/employees')).items; return true } catch (e) { error.value = '员工列表加载失败：' + (e as Error).message; return false }
}
async function openAssign(task: any) {
  if (!await ensureEmployees()) return
  current.value = task; Object.keys(selected).forEach(k => delete selected[k]); Object.keys(quantities).forEach(k => delete quantities[k]); modal.value = 'assign'; error.value = ''
}
function distribute() {
  const ids = employees.value.filter(e => selected[e.id]).map(e => e.id)
  if (!ids.length || !current.value) return
  const base = Math.floor(assignRemaining.value / ids.length); let remainder = assignRemaining.value % ids.length
  ids.forEach(id => { quantities[id] = base + (remainder-- > 0 ? 1 : 0) })
}
async function assign() {
  if (!current.value) return
  const assignments = employees.value.filter(e => selected[e.id]).map(e => ({ employee_id: e.id, planned_qty: Number(quantities[e.id] || 0) }))
  if (!assignments.length) { error.value = '请至少选择一名员工'; return }
  saving.value = true; error.value = ''
  try { await api(`/production-tasks/${current.value.id}/assign`, { method: 'POST', body: JSON.stringify({ version: current.value.version, assignments }) }); modal.value = ''; notice.value = `派工成功，已创建 ${assignments.length} 个人员任务`; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function openReport(task: any) {
  current.value = task
  report.bundle_id = ''; report.good_qty = 0; report.defect_qty = 0; report.remark = ''; report.actual_work_seconds = null; report.device_code = ''
  reportKey.value = uuid(); modal.value = 'report'; error.value = ''
  bundles.value = []
  if (task.report_mode === 'B') return
  try {
    const all = (await api<{ items: any[] }>('/bundles')).items.filter((b: any) => b.work_order_id === task.work_order_id)
    let list = all
    try {
      const assignments = await api<{ items: any[] }>(`/production-tasks/${task.id}/bundle-assignments`)
      if (assignments.items?.length) {
        const mine = assignments.items.filter((a: any) => !meEmployeeId.value || a.employee_id === meEmployeeId.value)
        const ids = new Set(mine.map((a: any) => a.bundle_id))
        list = all.filter((b: any) => ids.has(b.id))
      }
    } catch { /* 模式 B 无派工记录时使用工单全部扎 */ }
    bundles.value = list
    if (list.length) { report.bundle_id = list[0].id; report.good_qty = list[0].current_qty }
  } catch (e) { error.value = '扎列表加载失败：' + (e as Error).message }
}
async function submitReport() {
  if (!current.value) return
  if (!isModeB.value && !report.bundle_id) { error.value = '请先选择扎'; return }
  saving.value = true; error.value = ''
  try {
    const result = await api<{ review_status: string }>('/work-reports', { method: 'POST', headers: { 'Idempotency-Key': reportKey.value }, body: JSON.stringify({ task_id: current.value.id, bundle_id: report.bundle_id, good_qty: Number(report.good_qty), defect_qty: Number(report.defect_qty), actual_work_seconds: report.actual_work_seconds, device_code: report.device_code || null, attachment_ids: [], report_kind: 'normal', occurred_at: Date.now() * 1000, remark: report.remark || null }) })
    modal.value = ''; notice.value = result.review_status === 'approved' ? '报工已提交并自动审批，已形成有效计件。' : '报工已提交，等待审批；当前不计入有效工资。'; await load()
  } catch (e) { error.value = `${(e as Error).message}。可使用相同命令安全重试。` } finally { saving.value = false }
}
onMounted(async () => {
  load()
  try { const me = await api<{ user: { employee_id?: string } }>('/me'); meEmployeeId.value = me.user.employee_id || '' } catch { /* ignore */ }
})
</script>
<template>
  <div class="page-header"><div><h1>任务与派工</h1><p>按工序任务多人分配计划量，支持分批报工和余额控制</p></div><button class="button secondary" @click="load">刷新任务</button></div>
  <div v-if="error && !modal" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><div class="tabs"><button :class="{ active: filter === 'all' }" @click="filter = 'all'">全部任务</button><button :class="{ active: filter === 'unassigned' }" @click="filter = 'unassigned'">待派工</button><button :class="{ active: filter === 'assigned' }" @click="filter = 'assigned'">人员任务</button></div><PageState :loading="loading" /><div v-if="!loading" class="table-wrap"><table><thead><tr><th>工序</th><th>生产人员</th><th>计划数量</th><th>已报/良品</th><th>未审批</th><th>剩余可报</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="task in filtered" :key="task.id"><td><strong>{{ task.process_code }} · {{ task.process_name }}</strong><small class="text-muted" style="display:block">{{ task.parent_task_id ? '人员任务' : '工序父任务' }}</small></td><td>{{ task.employee_name || '—' }}</td><td>{{ task.planned_qty }}</td><td>{{ task.reported_qty }} / {{ task.approved_good_qty }}</td><td><span :class="{ 'text-danger': task.pending_qty > 0 }">{{ task.pending_qty }}</span></td><td>{{ task.remaining_qty }}</td><td><span class="status" :class="task.status">{{ statusText[task.status] }}</span></td><td><button v-if="!task.parent_task_id && task.status !== 'completed' && canAssign" class="button link" @click="openAssign(task)">派工</button><button v-if="!task.parent_task_id && task.status !== 'completed' && canAssign" class="button link" @click="openBundleAssign(task)">扎派工</button><button v-else-if="task.remaining_qty > 0 && !['paused','completed','cancelled'].includes(task.status) && canReport" class="button link" @click="openReport(task)">报工</button></td></tr></tbody></table><PageState :empty="!filtered.length" /></div></section>
  <ModalDialog v-if="modal === 'assign'" wide :title="`多人派工 · ${current?.process_name}`" @close="modal = ''"><div class="quantity-summary"><span>父任务计划<strong>{{ current?.planned_qty }}</strong></span><span>已分配<strong>{{ assignUsed }}</strong></span><span>可分配<strong>{{ assignRemaining }}</strong></span><span>本次分配<strong :class="{ 'text-danger': assignedTotal > assignRemaining }">{{ assignedTotal }}</strong></span></div><div v-if="error" class="notice error">{{ error }}</div><div class="toolbar" style="padding:0 0 14px;border:0"><span>选择员工并填写每人的计划量</span><span class="spacer"></span><button v-if="canAssign" class="button secondary small" @click="distribute">均分余额</button></div><div class="table-wrap"><table><thead><tr><th>选择</th><th>工号</th><th>姓名</th><th>计划数量</th></tr></thead><tbody><tr v-for="employee in employees" :key="employee.id"><td><input v-model="selected[employee.id]" type="checkbox" @change="selected[employee.id] && !quantities[employee.id] && (quantities[employee.id] = 0)"></td><td>{{ employee.code }}</td><td>{{ employee.name }}</td><td><input v-model.number="quantities[employee.id]" type="number" min="0" :max="assignRemaining" :disabled="!selected[employee.id]" style="width:130px"></td></tr></tbody></table></div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button v-if="canAssign" class="button" :disabled="saving || assignedTotal <= 0 || assignedTotal > assignRemaining" @click="assign">确认派工</button></template></ModalDialog>
  <ModalDialog v-if="modal === 'bundle-assign'" wide :title="`扎级派工 · ${current?.process_name}`" @close="modal = ''"><div v-if="error" class="notice error">{{ error }}</div><div class="form-grid"><div class="field"><label class="required">派工员工</label><FilterSelect v-model="bundleEmployee" :options="employeeOptions" placeholder="选择员工"/></div></div><div class="table-wrap"><table><thead><tr><th>选择</th><th>扎号</th><th>颜色/尺码</th><th>件数</th><th>已派</th><th>可派</th><th>本次数量</th></tr></thead><tbody><tr v-for="row in bundleRows" :key="row.bundle_id"><td><input v-model="bundleSelected[row.bundle_id]" type="checkbox"></td><td>{{ row.bundle_no }}</td><td>{{ row.color }}/{{ row.size }}</td><td>{{ row.bundle_qty }}</td><td>{{ row.assigned_qty }}</td><td>{{ row.assignable_qty }}</td><td><input v-model.number="bundleQty[row.bundle_id]" type="number" min="1" :max="row.assignable_qty" style="width:110px"></td></tr></tbody></table><PageState :empty="!bundleRows.length"/></div><div class="notice">仅模式 A 工单需要扎级派工；派工后由该员工按扎报工。</div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !bundleEmployee || !bundleRows.some(r => bundleSelected[r.bundle_id])" @click="submitBundleAssign">确认派工</button></template></ModalDialog>
  <ModalDialog v-if="modal === 'report'" :title="`任务报工 · ${current?.process_name}`" @close="modal = ''"><div class="quantity-summary"><span>计划<strong>{{ current?.planned_qty }}</strong></span><span>已报<strong>{{ current?.reported_qty }}</strong></span><span>未审批<strong>{{ current?.pending_qty }}</strong></span><span>剩余<strong>{{ current?.remaining_qty }}</strong></span></div><div v-if="error" class="notice error">{{ error }}</div><div class="form-grid"><div v-if="!isModeB" class="field full"><label class="required">扎</label><FilterSelect v-model="report.bundle_id" :options="bundleOptions" placeholder="选择扎" @change="onBundleChange"/><small v-if="!bundles.length" class="text-danger">该工单没有可报的扎，或未派给你；请先完成裁床确认/扎级派工</small></div><div v-else class="field full"><div class="notice" style="margin:0">当前工单为模式 B（自主选工序），按任务数量报工，无需选择扎。</div></div><div class="field"><label>设备</label><input v-model.trim="report.device_code" placeholder="如 平车08"></div><div class="field"><label class="required">良品数量</label><input v-model.number="report.good_qty" type="number" min="0" :max="maxQty"></div><div class="field"><label>不良数量</label><input v-model.number="report.defect_qty" type="number" min="0" :max="maxQty"></div><div class="field"><label>实际总工时（秒，可空）</label><input v-model.number="report.actual_work_seconds" type="number" min="0"></div><div class="field full"><label>备注</label><textarea v-model="report.remark" rows="3"></textarea></div></div><div class="notice">本次生产数量：{{ Number(report.good_qty || 0) + Number(report.defect_qty || 0) }}<template v-if="!isModeB"> / 扎件数 {{ currentBundle?.current_qty ?? '—' }}</template> / 任务剩余 {{ current?.remaining_qty }}。<template v-if="!isModeB">同一扎同一工序只能报工一次，报工使用持久幂等键。</template><template v-else>模式 B 按任务数量报工，不需选择扎。</template></div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button v-if="canReport" class="button" :disabled="saving || (!isModeB && !report.bundle_id) || Number(report.good_qty || 0) + Number(report.defect_qty || 0) <= 0 || Number(report.good_qty || 0) + Number(report.defect_qty || 0) > maxQty" @click="submitReport">提交报工</button></template></ModalDialog>
</template>
