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
const receipts = ref<any[]>([])
const projects = ref<any[]>([])
const warehouses = ref<any[]>([])
const suppliers = ref<any[]>([])
const employees = ref<any[]>([])
const materials = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const current = ref<any>(null)
const detail = ref<any>(null)
const iqc = reactive({ line_id: '', result: 'pass', defect_description: '' })

const form = reactive({ receipt_no: '', receipt_date: '', project_id: '', warehouse_id: '', supplier_id: '', receiver_employee_id: '', lines: [] as any[] })
const projectOptions = computed(() => projects.value.map(p => ({ value: p.id, label: `${p.project_code} · ${p.product_name}` })))
const warehouseOptions = computed(() => warehouses.value.map(w => ({ value: w.id, label: `${w.code} · ${w.name}` })))
const supplierOptions = computed(() => suppliers.value.map(s => ({ value: s.id, label: s.name })))
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: `${e.code} · ${e.name}` })))
const materialOptions = computed(() => materials.value.map(m => ({ value: m.id, label: `${m.code} · ${m.name}${m.specification ? ' · ' + m.specification : ''}` })))
const statusText: Record<string, string> = { draft: '草稿', submitted: '已入库', reversed: '已冲销' }
const iqcText: Record<string, string> = { pass: '合格', fail: '不合格', concession: '让步接收' }

function blankLine() {
  return { material_id: '', bed_no: '', color_no: '', batch_no: '', received_qty: '', pass_qty: '', concession_qty: '0', pending_qty: '0', accepted_qty: '', unit_price: '', remark: '' }
}
function addLine() { form.lines.push(blankLine()) }
function removeLine(index: number) { if (form.lines.length > 1) form.lines.splice(index, 1) }
function lineReject(line: any) { const value = Number(line.received_qty || 0) - Number(line.pass_qty || 0); return value > 0 ? value : 0 }
function lineAmount(line: any) { return (Number(line.accepted_qty || line.pass_qty || 0) * Number(line.unit_price || 0)).toFixed(2) }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [r, p, w, s, e, m] = await Promise.all([
      api<{ items: any[] }>('/material-receipts'), api<{ items: any[] }>('/projects'), api<{ items: any[] }>('/warehouses'),
      api<{ items: any[] }>('/suppliers'), api<{ items: any[] }>('/employees'), api<{ items: any[] }>('/materials'),
    ])
    receipts.value = r.items; projects.value = p.items; warehouses.value = w.items; suppliers.value = s.items; employees.value = e.items; materials.value = m.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, { receipt_no: '', receipt_date: new Date().toISOString().slice(0, 10), project_id: '', warehouse_id: '', supplier_id: '', receiver_employee_id: '', lines: [] })
  form.lines.push(blankLine())
  error.value = ''; modal.value = 'receipt'
}
function payloadLines() {
  return form.lines.map(l => ({ material_id: l.material_id, bed_no: l.bed_no || null, color_no: l.color_no || null, batch_no: l.batch_no || null, received_qty: l.received_qty || '0', pass_qty: l.pass_qty || '0', concession_qty: l.concession_qty || '0', pending_qty: l.pending_qty || '0', accepted_qty: l.accepted_qty || l.pass_qty || '0', unit_price: l.unit_price || '0', remark: l.remark || null }))
}
async function save() {
  if (!form.receipt_no.trim() || !form.project_id || !form.warehouse_id || !form.supplier_id || !form.lines.length) { error.value = '入库单号、项目、仓库、供应商和明细必填'; return }
  if (form.lines.some(l => !l.material_id || Number(l.received_qty || 0) <= 0)) { error.value = '每行都必须选择物料并填写来料数量'; return }
  saving.value = true; error.value = ''
  try {
    const payload = { receipt_no: form.receipt_no.trim(), receipt_date: Date.now() * 1000, project_id: form.project_id, warehouse_id: form.warehouse_id, supplier_id: form.supplier_id, receiver_employee_id: form.receiver_employee_id, lines: payloadLines() }
    await api('/material-receipts', { method: 'POST', body: JSON.stringify(payload) })
    modal.value = ''; notice.value = '入库单已保存为草稿'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function openDetail(item: any) {
  error.value = ''
  try { detail.value = await api<any>(`/material-receipts/${item.id}`); current.value = item; modal.value = 'detail' } catch (e) { error.value = (e as Error).message }
}
async function submitReceipt() {
  if (!current.value) return
  if (!window.confirm(`提交入库单 ${detail.value.header.receipt_no}？提交后将写入库存流水。`)) return
  saving.value = true; error.value = ''
  try { await api(`/material-receipts/${current.value.id}/submit`, { method: 'POST', body: JSON.stringify({ version: detail.value.header.version }) }); modal.value = ''; notice.value = '入库单已提交并写入库存'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function reverseReceipt() {
  if (!current.value) return
  const reason = window.prompt('请输入冲销原因')
  if (!reason) return
  saving.value = true; error.value = ''
  try { await api(`/material-receipts/${current.value.id}/reverse`, { method: 'POST', body: JSON.stringify({ version: detail.value.header.version, reason }) }); modal.value = ''; notice.value = '入库单已冲销'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function deleteReceipt(item: any) {
  if (!window.confirm(`删除草稿入库单 ${item.receipt_no}？`)) return
  error.value = ''
  try { await api(`/material-receipts/${item.id}`, { method: 'DELETE' }); notice.value = '入库单已删除'; await load() } catch (e) { error.value = (e as Error).message }
}
function openIQC(line: any) { iqc.line_id = line.id; iqc.result = line.iqc_result || 'pass'; iqc.defect_description = ''; modal.value = 'iqc' }
async function saveIQC() {
  saving.value = true; error.value = ''
  try {
    await api(`/material-receipt-lines/${iqc.line_id}/iqc`, { method: 'POST', body: JSON.stringify({ result: iqc.result, defect_description: iqc.defect_description || null, attachment_ids: [] }) })
    notice.value = 'IQC 检验结果已记录'
    const data = await api<any>(`/material-receipts/${current.value.id}`)
    detail.value = data
    modal.value = 'detail'
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>物料入库</h1><p>来料、IQC、合格/让步与实收入库分开记录，提交后写库存流水</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button><button v-if="canWrite" class="button" @click="openCreate">+ 入库单</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><PageState :loading="loading"/>
    <div v-if="!loading && receipts.length" class="table-wrap"><table><thead><tr><th>入库单号</th><th>日期</th><th>项目</th><th>款号</th><th>供应商</th><th>仓库</th><th>行数</th><th>来料合计</th><th>合格合计</th><th>金额合计</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in receipts" :key="item.id"><td><strong>{{ item.receipt_no }}</strong></td><td>{{ new Date(item.receipt_date / 1000).toLocaleDateString('zh-CN') }}</td><td>{{ item.project_code }}</td><td>{{ item.product_code }} · {{ item.product_name }}</td><td>{{ item.supplier_name || '—' }}</td><td>{{ item.warehouse_name || '—' }}</td><td>{{ item.line_count }}</td><td>{{ item.received_total }}</td><td>{{ item.pass_total }}</td><td>{{ item.amount_total }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td><button class="button link" @click="openDetail(item)">详情</button><button v-if="canWrite && item.status === 'draft'" class="button link text-danger" @click="deleteReceipt(item)">删除</button></td></tr></tbody></table></div>
    <PageState v-if="!loading" :empty="!receipts.length"/>
  </section>

  <ModalDialog v-if="modal === 'receipt'" wide title="新增物料入库单" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label class="required">入库单号</label><input v-model.trim="form.receipt_no" placeholder="RK20261103001"></div>
      <div class="field"><label>入库日期</label><DatePicker v-model="form.receipt_date"/></div>
      <div class="field"><label class="required">项目</label><FilterSelect v-model="form.project_id" :options="projectOptions" placeholder="选择项目"/></div>
      <div class="field"><label class="required">仓库</label><FilterSelect v-model="form.warehouse_id" :options="warehouseOptions" placeholder="选择仓库"/></div>
      <div class="field"><label class="required">供应商</label><FilterSelect v-model="form.supplier_id" :options="supplierOptions" placeholder="选择供应商"/></div>
      <div class="field"><label class="required">收货人</label><FilterSelect v-model="form.receiver_employee_id" :options="employeeOptions" placeholder="选择员工"/></div>
    </div>
    <h3 style="margin-top:20px">明细</h3>
    <div class="table-wrap" style="max-height:40vh"><table><thead><tr><th>物料</th><th>颜色/色号</th><th>缸号</th><th>来料</th><th>合格</th><th>不合格</th><th>让步</th><th>待检</th><th>实收</th><th>单价</th><th>金额</th><th>备注</th><th></th></tr></thead><tbody><tr v-for="(line, index) in form.lines" :key="index"><td style="min-width:220px"><FilterSelect v-model="line.material_id" :options="materialOptions" placeholder="选择物料"/></td><td><input v-model.trim="line.color_no" placeholder="颜色" style="width:80px"><input v-model.trim="line.bed_no" placeholder="色号" style="width:80px"></td><td><input v-model.trim="line.batch_no" style="width:90px"></td><td><input v-model="line.received_qty" inputmode="decimal" style="width:80px"></td><td><input v-model="line.pass_qty" inputmode="decimal" style="width:80px"></td><td>{{ lineReject(line) }}</td><td><input v-model="line.concession_qty" inputmode="decimal" style="width:70px"></td><td><input v-model="line.pending_qty" inputmode="decimal" style="width:70px"></td><td><input v-model="line.accepted_qty" inputmode="decimal" :placeholder="line.pass_qty || '0'" style="width:80px"></td><td><input v-model="line.unit_price" inputmode="decimal" style="width:80px"></td><td>{{ lineAmount(line) }}</td><td><input v-model.trim="line.remark" placeholder="差异说明" style="width:120px"></td><td><button type="button" class="button link text-danger" @click="removeLine(index)">删除</button></td></tr></tbody></table></div>
    <div class="form-actions"><button type="button" class="button secondary small" @click="addLine">+ 增加明细</button></div>
    <div class="notice">实收可小于合格，但必须填写差异说明；IQC 不合格的行应走隔离/让步流程。</div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="save">保存草稿</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'detail'" wide :title="`入库单 · ${detail?.header?.receipt_no || ''}`" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="quantity-summary"><span>项目<strong>{{ detail?.header?.project_code }}</strong></span><span>仓库<strong>{{ detail?.header?.warehouse_name }}</strong></span><span>供应商<strong>{{ detail?.header?.supplier_name || '—' }}</strong></span><span>状态<strong>{{ statusText[detail?.header?.status] || detail?.header?.status }}</strong></span></div>
    <div class="table-wrap" style="max-height:46vh"><table><thead><tr><th>物料</th><th>颜色/色号</th><th>缸号</th><th>来料</th><th>合格</th><th>不合格</th><th>让步</th><th>待检</th><th>实收</th><th>单价</th><th>金额</th><th>IQC</th><th>检验人</th><th>操作</th></tr></thead><tbody><tr v-for="line in detail?.lines || []" :key="line.id"><td>{{ line.material_code }} · {{ line.material_name }}<small class="text-muted" style="display:block">{{ line.specification }}</small></td><td>{{ line.color_no }}/{{ line.bed_no }}</td><td>{{ line.batch_no || '—' }}</td><td>{{ line.received_qty }}</td><td>{{ line.pass_qty }}</td><td>{{ line.reject_qty }}</td><td>{{ line.concession_qty }}</td><td>{{ line.pending_qty }}</td><td>{{ line.accepted_qty }}</td><td>{{ line.unit_price }}</td><td>{{ line.amount }}</td><td>{{ iqcText[line.iqc_result] || '未检' }}</td><td>{{ line.inspector_name || '—' }}</td><td><button v-if="canWrite && detail?.header?.status !== 'reversed'" class="button link" @click="openIQC(line)">IQC</button></td></tr></tbody></table></div>
    <template #footer><button class="button secondary" @click="modal = ''">关闭</button><button v-if="canWrite && detail?.header?.status === 'draft'" class="button" :disabled="saving" @click="submitReceipt">提交入库</button><button v-if="canWrite && detail?.header?.status === 'submitted'" class="button danger" :disabled="saving" @click="reverseReceipt">冲销</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'iqc'" title="IQC 检验" @close="modal = 'detail'">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid"><div class="field"><label class="required">检验结果</label><FilterSelect v-model="iqc.result" :options="[{value:'pass',label:'合格'},{value:'fail',label:'不合格'},{value:'concession',label:'让步接收'}]"/></div><div class="field full"><label>缺陷描述</label><textarea v-model="iqc.defect_description" rows="3"></textarea></div></div>
    <template #footer><button class="button secondary" @click="modal = 'detail'">取消</button><button class="button" :disabled="saving" @click="saveIQC">保存检验</button></template>
  </ModalDialog>
</template>
