<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { usePermissions } from '../permissions'
import { todayISO } from '../date'
import DatePicker from '../components/DatePicker.vue'
import FilterSelect from '../components/FilterSelect.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'

/* 物料出库：仓管登记出库单（可指定领料人），领料人（通常线长）确认后扣减库存。 */
const { has } = usePermissions()
const route = useRoute()
const canIssue = computed(() => has('inventory:issue'))
const canConfirm = computed(() => has('inventory:issue_confirm') || has('inventory:issue'))
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const tab = ref<'issue' | 'confirm'>(route.query.tab === 'confirm' || !canIssue.value ? 'confirm' : 'issue')

const workOrders = ref<any[]>([])
const warehouses = ref<any[]>([])
const materials = ref<any[]>([])
const employees = ref<any[]>([])
const stock = ref<any[]>([])
const issues = ref<any[]>([])
const pending = ref<any[]>([])
const issueModal = ref(false)

const statusText: Record<string, string> = { posted: '已领料', pending_confirm: '待领取确认', reversed: '已冲销' }
const confirmText: Record<string, string> = { none: '直接发放', pending: '待确认', confirmed: '已确认' }
const form = reactive({ issue_no: '', issue_date: '', work_order_id: '', warehouse_id: '', material_id: '', qty: '', purpose: '', operator_employee_id: '', receiver_employee_id: '', remark: '' })

const workOrderOptions = computed(() => workOrders.value.map(w => ({ value: w.id, label: `${w.work_order_code} · ${w.product_name || ''}` })))
const warehouseOptions = computed(() => warehouses.value.map(w => ({ value: w.id, label: `${w.code} · ${w.name}` })))
const materialOptions = computed(() => materials.value.map(m => ({ value: m.id, label: `${m.code} · ${m.name}` })))
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: e.name })))
const currentStock = computed(() => {
  if (!form.material_id || !form.warehouse_id) return '—'
  const row = stock.value.find(r => r.material_id === form.material_id && r.warehouse_id === form.warehouse_id)
  return row ? `${row.qty} ${row.unit || ''}`.trim() : '0'
})

async function load() {
  if (!canIssue.value && !canConfirm.value) return
  if (tab.value === 'issue' && !canIssue.value) tab.value = 'confirm'
  if (tab.value === 'confirm' && !canConfirm.value) tab.value = 'issue'
  loading.value = true; error.value = ''
  try {
    const [wo, wh, m, e, inv] = await Promise.all([
      api<{ items: any[] }>('/work-orders'), api<{ items: any[] }>('/warehouses'),
      api<{ items: any[] }>('/materials'), api<{ items: any[] }>('/employees'),
      api<{ items: any[] }>('/inventory/summary'),
    ])
    workOrders.value = wo.items; warehouses.value = wh.items; materials.value = m.items; employees.value = e.items; stock.value = inv.items
    const tasks: Promise<any>[] = []
    if (canIssue.value) tasks.push(api<{ items: any[] }>('/material-issues').then(r => { issues.value = r.items }))
    if (canConfirm.value) tasks.push(api<{ items: any[] }>('/material-issues/pending-confirm').then(r => { pending.value = r.items }))
    await Promise.all(tasks)
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

function openIssue() {
  error.value = ''
  Object.assign(form, { issue_no: `CK${Date.now()}`, issue_date: todayISO(), work_order_id: '', warehouse_id: '', material_id: '', qty: '', purpose: '', operator_employee_id: '', receiver_employee_id: '', remark: '' })
  issueModal.value = true
}

async function createIssue() {
  if (!form.issue_no.trim() || !form.work_order_id || !form.warehouse_id || !form.material_id || !form.qty) {
    error.value = '出库单号、工单、仓库、物料和数量必填'; return
  }
  saving.value = true; error.value = ''
  try {
    const res = await api<{ status: string }>('/material-issues', { method: 'POST', body: JSON.stringify({
      ...form, issue_no: form.issue_no.trim(), purpose: form.purpose || null,
      operator_employee_id: form.operator_employee_id || null,
      receiver_employee_id: form.receiver_employee_id || null, remark: form.remark || null,
    }) })
    issueModal.value = false
    notice.value = res.status === 'pending_confirm' ? '出库单已开具，已通知领料人确认' : '出库单已发放并扣减库存'
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

async function confirmIssue(item: any) {
  error.value = ''
  try {
    await api(`/material-issues/${item.id}/confirm`, { method: 'POST', body: JSON.stringify({ version: item.version, reason: '' }) })
    notice.value = `出库单 ${item.issue_no} 已确认，库存已扣减`
    await load()
    window.dispatchEvent(new CustomEvent('clothp-material-outbound-changed'))
  } catch (e) { error.value = (e as Error).message }
}

async function reverseIssue(item: any) {
  const reason = window.prompt(`请输入冲销出库单 ${item.issue_no} 的原因`)
  if (!reason) return
  error.value = ''
  try { await api(`/material-issues/${item.id}/reverse`, { method: 'POST', body: JSON.stringify({ reason }) }); notice.value = '出库单已冲销，库存已回补'; await load() } catch (e) { error.value = (e as Error).message }
}

onMounted(() => { void load() })
</script>
<template>
  <div class="page-header">
    <div><h1>物料出库</h1><p>仓管登记出库并指定领料人；领料人确认后才扣减库存</p></div>
    <div class="header-actions"><button v-if="canIssue && tab === 'issue'" class="button" @click="openIssue">+ 新增出库单</button><button class="button secondary" @click="load">刷新</button></div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>
  <PageState :loading="loading" :error="error" @retry="load" />
  <div v-if="!loading" class="tabs">
    <button v-if="canIssue" :class="{ active: tab === 'issue' }" @click="tab = 'issue'; load()">出库登记</button>
    <button v-if="canConfirm" :class="{ active: tab === 'confirm' }" @click="tab = 'confirm'; load()">领取确认 <span v-if="pending.length" class="tab-badge">{{ pending.length }}</span></button>
  </div>

  <div v-if="tab === 'issue' && !loading" class="table-wrap">
    <table><thead><tr><th>出库单号</th><th>日期</th><th>工单</th><th>仓库</th><th>物料</th><th>数量</th><th>领料人</th><th>状态</th><th>操作</th></tr></thead><tbody>
      <tr v-for="item in issues" :key="item.id"><td><strong>{{ item.issue_no }}</strong></td><td>{{ item.issue_date }}</td><td>{{ item.work_order_code }}</td><td>{{ item.warehouse_name }}</td><td>{{ item.material_name }}</td><td>{{ item.qty }} {{ item.issue_unit }}</td><td>{{ item.receiver_name || item.operator_name || '—' }}</td><td>{{ statusText[item.status] || item.status }} / {{ confirmText[item.confirm_status] || '—' }}</td><td><button v-if="item.status === 'posted'" class="button link text-danger" @click="reverseIssue(item)">冲销</button><span v-else class="text-muted">—</span></td></tr>
    </tbody></table>
    <PageState :empty="!issues.length" />
  </div>

  <div v-if="tab === 'confirm' && !loading" class="table-wrap">
    <table><thead><tr><th>出库单号</th><th>日期</th><th>工单</th><th>物料</th><th>数量</th><th>仓库</th><th>用途</th><th>操作</th></tr></thead><tbody>
      <tr v-for="item in pending" :key="item.id"><td><strong>{{ item.issue_no }}</strong></td><td>{{ item.issue_date }}</td><td>{{ item.work_order_code }}</td><td>{{ item.material_name }}</td><td>{{ item.qty }} {{ item.unit }}</td><td>{{ item.warehouse_name }}</td><td>{{ item.purpose || '—' }}</td><td><button class="button" @click="confirmIssue(item)">确认领取</button></td></tr>
    </tbody></table>
    <PageState :empty="!pending.length" />
  </div>

  <ModalDialog v-if="issueModal" title="新增物料出库单" wide @close="issueModal = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label>出库单号</label><input v-model.trim="form.issue_no"></div>
      <div class="field"><label>日期</label><DatePicker v-model="form.issue_date"/></div>
      <div class="field"><label class="required">工单</label><FilterSelect v-model="form.work_order_id" :options="workOrderOptions" placeholder="选择工单"/></div>
      <div class="field"><label class="required">仓库</label><FilterSelect v-model="form.warehouse_id" :options="warehouseOptions" placeholder="选择仓库"/></div>
      <div class="field"><label class="required">物料</label><FilterSelect v-model="form.material_id" :options="materialOptions" placeholder="选择物料"/></div>
      <div class="field"><label>当前库存</label><input :value="currentStock" disabled></div>
      <div class="field"><label class="required">数量</label><input v-model="form.qty" inputmode="decimal"></div>
      <div class="field"><label>用途</label><input v-model.trim="form.purpose" placeholder="选填"></div>
      <div class="field"><label>经办人</label><FilterSelect v-model="form.operator_employee_id" :options="employeeOptions" placeholder="选填"/></div>
      <div class="field"><label>领料人</label><FilterSelect v-model="form.receiver_employee_id" :options="employeeOptions" placeholder="选填；填写后需对方确认"/></div>
      <div class="field full"><label>备注</label><input v-model.trim="form.remark" placeholder="选填"></div>
    </div>
    <template #footer><button class="button secondary" @click="issueModal = false">取消</button><button class="button" :disabled="saving" @click="createIssue">确认出库</button></template>
  </ModalDialog>
</template>
<style scoped>
.tabs{display:flex;gap:8px;margin:14px 0}
.tabs button{border:1px solid var(--border);background:#fff;border-radius:8px;padding:6px 14px;cursor:pointer}
.tabs button.active{border-color:#155eef;background:#eff4ff;color:#155eef}
.tab-badge{display:inline-block;margin-left:4px;background:#e5484d;color:#fff;border-radius:10px;padding:0 6px;font-size:11px}
.form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
.form-grid .full{grid-column:1/-1}
</style>
