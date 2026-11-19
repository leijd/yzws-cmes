<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import DatePicker from '../components/DatePicker.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('inventory:receipt'))
const receipts = ref<any[]>([])
const customers = ref<any[]>([])
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
const editingId = ref('')
const editingReceiptDate = ref(0)
const pickModal = ref<'' | 'warehouse' | 'supplier'>('')
const pickForm = reactive({ code: '', name: '', warehouse_type: 'raw', manager_employee_id: '', contact_name: '', phone: '', address: '' })
const pickError = ref('')
const pickSaving = ref(false)
const current = ref<any>(null)
const detail = ref<any>(null)
const iqc = reactive({ line_id: '', result: 'pass', defect_description: '' })

const form = reactive({ receipt_no: '', receipt_date: '', project_id: '', customer_id: '', product_name: '', style_no: '', style_name: '', warehouse_id: '', supplier_id: '', receiver_employee_id: '', lines: [] as any[] })
const customerOptions = computed(() => customers.value.map(c => ({ value: c.id, label: `${c.code} · ${c.name}` })))
const projectOptions = computed(() => projects.value.map(p => ({ value: p.id, label: `${p.project_code} · ${p.style_no || p.product_name || ''}${p.customer_name ? ' · ' + p.customer_name : ''}` })))
const warehouseOptions = computed(() => warehouses.value.map(w => ({ value: w.id, label: `${w.code} · ${w.name}` })))
const supplierOptions = computed(() => suppliers.value.map(s => ({ value: s.id, label: `${s.code} · ${s.name}` })))
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: `${e.code} · ${e.name}` })))
const materialOptions = computed(() => materials.value.map(m => ({ value: m.id, label: `${m.code} · ${m.name}${m.specification ? ' · ' + m.specification : ''}` })))
const warehouseTypeOptions = [{ value: 'raw', label: '原料仓' }, { value: 'finished', label: '成品仓' }]
const formCollapsed = ref(false)
const materialPickModal = ref(false)
const materialPick = reactive({ keyword: '', page: 1, page_size: 20, total: 0, items: [] as any[], selected: [] as string[], loading: false })
const statusText: Record<string, string> = { draft: '草稿', submitted: '已入库', reversed: '已冲销' }
const iqcText: Record<string, string> = { pass: '合格', fail: '不合格', concession: '让步接收' }

function blankLine() {
  return { material_id: '', bed_no: '', color_no: '', batch_no: '', received_qty: '', pass_qty: '', concession_qty: '0', pending_qty: '0', accepted_qty: '', unit_price: '', remark: '', selected: false }
}
function addLine() { form.lines.push(blankLine()) }
function removeLine(index: number) { if (form.lines.length > 1) form.lines.splice(index, 1) }
const selectedLineCount = computed(() => form.lines.filter(l => l.selected).length)
const allLinesSelected = computed(() => form.lines.length > 0 && form.lines.every(l => l.selected))
function toggleAllLines(checked: boolean) { form.lines.forEach(l => { l.selected = checked }) }
function onToggleAllLines(event: Event) { toggleAllLines((event.target as HTMLInputElement).checked) }
function removeSelectedLines() {
  if (!selectedLineCount.value) { error.value = '请先勾选要删除的明细行'; return }
  const remaining = form.lines.filter(l => !l.selected)
  form.lines.splice(0, form.lines.length, ...(remaining.length ? remaining : [blankLine()]))
  error.value = ''
}
function lineReject(line: any) { const value = Number(line.received_qty || 0) - Number(line.pass_qty || 0); return value > 0 ? value : 0 }
function lineAmount(line: any) { return fmtMoney(Number(line.accepted_qty || line.pass_qty || 0) * Number(line.unit_price || 0)) }
function fmtQty(v: any) {
  if (v === '' || v === null || v === undefined) return ''
  const n = Number(v)
  return Number.isFinite(n) ? String(Math.round(n)) : String(v)
}
function fmtMoney(v: any) {
  if (v === '' || v === null || v === undefined) return ''
  const n = Number(v)
  return Number.isFinite(n) ? String(Number(n.toFixed(2))) : String(v)
}

async function load() {
  loading.value = true; error.value = ''
  try {
    const [r, c, pj, w, s, e, m] = await Promise.all([
      api<{ items: any[] }>('/material-receipts'), api<{ items: any[] }>('/customers'), api<{ items: any[] }>('/projects', {}, { silent: true }), api<{ items: any[] }>('/warehouses'),
      api<{ items: any[] }>('/suppliers'), api<{ items: any[] }>('/employees'), api<{ items: any[] }>('/materials'),
    ])
    receipts.value = r.items; customers.value = c.items; projects.value = pj.items || []; warehouses.value = w.items; suppliers.value = s.items; employees.value = e.items; materials.value = m.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, { receipt_no: '', receipt_date: new Date().toISOString().slice(0, 10), project_id: '', customer_id: '', product_name: '', style_no: '', style_name: '', warehouse_id: '', supplier_id: '', receiver_employee_id: '', lines: [] })
  form.lines.push(blankLine())
  editingId.value = ''; editingReceiptDate.value = 0
  error.value = ''; formCollapsed.value = false; modal.value = 'receipt'
}
async function openEdit(item: any) {
  error.value = ''
  try {
    const data = await api<any>(`/material-receipts/${item.id}`)
    const h = data.header || {}
    const lines = (data.lines || []).map((l: any) => ({ material_id: l.material_id || '', bed_no: l.bed_no || '', color_no: l.color_no || '', batch_no: l.batch_no || '', received_qty: fmtQty(l.received_qty), pass_qty: fmtQty(l.pass_qty), concession_qty: fmtQty(l.concession_qty), pending_qty: fmtQty(l.pending_qty), accepted_qty: fmtQty(l.accepted_qty), unit_price: fmtMoney(l.unit_price), remark: l.remark || '', selected: false }))
    Object.assign(form, {
      receipt_no: h.receipt_no || '', receipt_date: h.receipt_date ? new Date(h.receipt_date / 1000).toISOString().slice(0, 10) : '',
      project_id: h.project_id || '', customer_id: h.customer_id || '', product_name: h.product_name || '', style_no: h.style_no || '', style_name: h.style_name || '',
      warehouse_id: h.warehouse_id || '', supplier_id: h.supplier_id || '', receiver_employee_id: h.receiver_employee_id || '',
      lines: lines.length ? lines : [blankLine()],
    })
    editingId.value = item.id; editingReceiptDate.value = h.receipt_date || 0
    formCollapsed.value = false; modal.value = 'receipt'
  } catch (e) { error.value = (e as Error).message }
}
function payloadLines() {
  return form.lines.map(l => ({ material_id: l.material_id, bed_no: l.bed_no || null, color_no: l.color_no || null, batch_no: l.batch_no || null, received_qty: l.received_qty || '0', pass_qty: l.pass_qty || '0', concession_qty: l.concession_qty || '0', pending_qty: l.pending_qty || '0', accepted_qty: l.accepted_qty || l.pass_qty || '0', unit_price: l.unit_price || '0', remark: l.remark || null }))
}
function onReceiptProjectChange() {
  const project = projects.value.find(item => item.id === form.project_id)
  if (!project) return
  const matched = customers.value.find(c => c.name === project.customer_name)
  if (matched) form.customer_id = matched.id
  form.style_no = project.style_no || form.style_no
  form.style_name = project.style_name || form.style_name
  form.product_name = form.product_name || project.product_name || ''
}
async function save() {
  if (!form.receipt_no.trim() || !form.warehouse_id || !form.supplier_id || !form.lines.length) { error.value = '入库单号、仓库、供应商和明细必填'; return }
  if (form.lines.some(l => !l.material_id || Number(l.received_qty || 0) <= 0)) { error.value = '每行都必须选择物料并填写来料数量'; return }
  saving.value = true; error.value = ''
  try {
    const payload = { receipt_no: form.receipt_no.trim(), receipt_date: editingId.value ? editingReceiptDate.value : Date.now() * 1000, project_id: form.project_id, customer_id: form.customer_id, product_name: form.product_name.trim(), style_no: form.style_no.trim(), style_name: form.style_name.trim(), warehouse_id: form.warehouse_id, supplier_id: form.supplier_id, receiver_employee_id: form.receiver_employee_id, lines: payloadLines() }
    if (editingId.value) await api(`/material-receipts/${editingId.value}`, { method: 'PATCH', body: JSON.stringify(payload) })
    else await api('/material-receipts', { method: 'POST', body: JSON.stringify(payload) })
    modal.value = ''; notice.value = editingId.value ? '入库单已更新' : '入库单已保存为草稿'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

function openPick(type: 'warehouse' | 'supplier') {
  Object.assign(pickForm, { code: '', name: '', warehouse_type: 'raw', manager_employee_id: '', contact_name: '', phone: '', address: '' })
  pickError.value = ''; pickModal.value = type
}
async function savePick() {
  const type = pickModal.value
  if (!type) return
  if (!pickForm.name.trim()) { pickError.value = type === 'warehouse' ? '请填写仓库名称' : '请填写供应商名称'; return }
  saving.value = true; pickSaving.value = true; pickError.value = ''
  try {
    const path = type === 'warehouse' ? '/material-receipts/warehouses' : '/material-receipts/suppliers'
    const body = type === 'warehouse'
      ? { code: pickForm.code, name: pickForm.name, warehouse_type: pickForm.warehouse_type, manager_employee_id: pickForm.manager_employee_id }
      : { code: pickForm.code, name: pickForm.name, contact_name: pickForm.contact_name, phone: pickForm.phone, address: pickForm.address }
    const created = await api<{ id: string }>(path, { method: 'POST', body: JSON.stringify(body) })
    if (type === 'warehouse') {
      warehouses.value = (await api<{ items: any[] }>('/warehouses')).items
      form.warehouse_id = created.id
    } else {
      suppliers.value = (await api<{ items: any[] }>('/suppliers')).items
      form.supplier_id = created.id
    }
    notice.value = type === 'warehouse' ? '仓库已新增并选中' : '供应商已新增并选中'
    pickModal.value = ''
  } catch (e) { pickError.value = (e as Error).message } finally { saving.value = false; pickSaving.value = false }
}
function choosePick(item: any) {
  if (pickModal.value === 'warehouse') form.warehouse_id = item.id
  if (pickModal.value === 'supplier') form.supplier_id = item.id
  pickModal.value = ''
}

async function loadSupplierMaterials() {
  if (!form.supplier_id) { pickError.value = '请先选择供应商'; return }
  materialPick.loading = true; pickError.value = ''
  try {
    const qs = new URLSearchParams({ supplier_id: form.supplier_id, keyword: materialPick.keyword, page: String(materialPick.page), page_size: String(materialPick.page_size) })
    const data = await api<{ items: any[]; total: number }>(`/material-receipts/supplier-materials?${qs.toString()}`)
    materialPick.items = data.items; materialPick.total = data.total
  } catch (e) { pickError.value = (e as Error).message } finally { materialPick.loading = false }
}
function openSupplierMaterialPick() {
  materialPick.keyword = ''; materialPick.page = 1; materialPick.total = 0; materialPick.items = []; materialPick.selected = []
  pickError.value = ''; materialPickModal.value = true
  if (form.supplier_id) loadSupplierMaterials()
}
function changeMaterialPage(page: number) { materialPick.page = page; loadSupplierMaterials() }
function toggleMaterialPick(id: string) {
  const index = materialPick.selected.indexOf(id)
  if (index >= 0) materialPick.selected.splice(index, 1)
  else materialPick.selected.push(id)
}
function confirmSupplierMaterials() {
  const chosen = materialPick.items.filter(m => materialPick.selected.includes(m.material_id))
  if (!chosen.length) { pickError.value = '请至少选择一项物料'; return }
  const firstEmpty = form.lines.length === 1 && !form.lines[0].material_id && !form.lines[0].received_qty
  if (firstEmpty) form.lines.splice(0, 1)
  chosen.forEach(m => {
    const line = blankLine()
    line.material_id = m.name || m.code || m.material_id
    line.color_no = m.color_no || ''
    line.bed_no = m.bed_no || ''
    form.lines.push(line)
  })
  materialPickModal.value = false
  notice.value = `已将 ${chosen.length} 项物料加入明细`
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
    <div v-if="!loading && receipts.length" class="table-wrap"><table><thead><tr><th>入库单号</th><th>日期</th><th>客户</th><th>款号 / 款式</th><th>产品名称</th><th>项目</th><th>供应商</th><th>仓库</th><th>行数</th><th>来料合计</th><th>合格合计</th><th>金额合计</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in receipts" :key="item.id"><td><strong>{{ item.receipt_no }}</strong></td><td>{{ new Date(item.receipt_date / 1000).toLocaleDateString('zh-CN') }}</td><td>{{ item.customer_name || '—' }}</td><td>{{ item.style_no || item.product_code || '—' }}{{ item.style_name ? ' · ' + item.style_name : '' }}</td><td>{{ item.product_name || '—' }}</td><td>{{ item.project_code || '—' }}</td><td>{{ item.supplier_name || '—' }}</td><td>{{ item.warehouse_name || '—' }}</td><td>{{ item.line_count }}</td><td>{{ fmtQty(item.received_total) }}</td><td>{{ fmtQty(item.pass_total) }}</td><td>{{ fmtMoney(item.amount_total) }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td><button class="button link" @click="openDetail(item)">详情</button><button v-if="canWrite && item.status === 'draft'" class="button link" @click="openEdit(item)">编辑</button><button v-if="canWrite && item.status === 'draft'" class="button link text-danger" @click="deleteReceipt(item)">删除</button></td></tr></tbody></table></div>
    <PageState v-if="!loading" :empty="!receipts.length"/>
  </section>

  <ModalDialog v-if="modal === 'receipt'" wide width="min(1500px, 96vw)" body-class="receipt-body" :title="editingId ? '编辑物料入库单' : '新增物料入库单'" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="receipt-form-section">
      <button type="button" class="form-collapse-icon" :title="formCollapsed ? '展开单据信息' : '收起单据信息'" @click="formCollapsed = !formCollapsed">{{ formCollapsed ? '▼' : '▲' }}</button>
      <div v-show="!formCollapsed" class="form-grid receipt-form">
      <div class="field"><label class="required">入库单号</label><input v-model.trim="form.receipt_no" :disabled="!!editingId" placeholder="RK20261103001"></div>
      <div class="field"><label>入库日期</label><DatePicker v-model="form.receipt_date"/></div>
      <div class="field"><label>项目号</label><FilterSelect v-model="form.project_id" :options="projectOptions" placeholder="选择订单项目（可选）" @change="onReceiptProjectChange"/></div>
      <div class="field"><label>客户</label><FilterSelect v-model="form.customer_id" :options="customerOptions" placeholder="选择客户（可选）"/></div>
      <div class="field"><label>产品名称</label><input v-model.trim="form.product_name" placeholder="产品名称"></div>
      <div class="field"><label>产品款号</label><input v-model.trim="form.style_no" placeholder="款号"></div>
      <div class="field"><label>款式</label><input v-model.trim="form.style_name" placeholder="款式名称"></div>
      <div class="field"><label class="required">仓库</label><div class="field-with-action"><FilterSelect v-model="form.warehouse_id" :options="warehouseOptions" placeholder="选择仓库"/><button type="button" class="button secondary small" @click="openPick('warehouse')">新增 / 浏览</button></div></div>
      <div class="field"><label class="required">供应商</label><div class="field-with-action"><FilterSelect v-model="form.supplier_id" :options="supplierOptions" placeholder="选择供应商"/><button type="button" class="button secondary small" @click="openPick('supplier')">新增 / 浏览</button></div></div>
      <div class="field"><label class="required">收货人</label><FilterSelect v-model="form.receiver_employee_id" :options="employeeOptions" placeholder="选择员工"/></div>
      </div>
      <div v-show="formCollapsed" class="form-collapsed-hint">单据信息已折叠 · 点击右上角图标展开</div>
    </div>
    <div class="detail-head">
      <div class="detail-head-left"><h3>明细</h3><span class="detail-tip">实收可小于合格，但必须填写差异说明；IQC 不合格的行应走隔离/让步流程。</span></div>
      <div class="detail-actions"><button type="button" class="button secondary small" @click="openSupplierMaterialPick">选择</button><button type="button" class="button secondary small" @click="addLine">+ 增加明细</button><button type="button" class="button secondary small" :disabled="!selectedLineCount" @click="removeSelectedLines">批量删除{{ selectedLineCount ? `(${selectedLineCount})` : '' }}</button></div>
    </div>
    <div class="table-wrap receipt-lines"><table><thead><tr><th style="width:36px"><input type="checkbox" :checked="allLinesSelected" @change="onToggleAllLines"></th><th>物料</th><th>颜色/色号</th><th>缸号</th><th>来料</th><th>合格</th><th>不合格</th><th>让步</th><th>待检</th><th>实收</th><th>单价</th><th>金额</th><th>备注</th><th>操作</th></tr></thead><tbody><tr v-for="(line, index) in form.lines" :key="index"><td><input type="checkbox" v-model="line.selected"></td><td style="min-width:220px"><input v-model.trim="line.material_id" placeholder="物料编码/名称，可手填"></td><td><input v-model.trim="line.color_no" placeholder="颜色" style="width:80px"><input v-model.trim="line.bed_no" placeholder="色号" style="width:80px"></td><td><input v-model.trim="line.batch_no" style="width:90px"></td><td><input v-model="line.received_qty" inputmode="decimal" style="width:80px"></td><td><input v-model="line.pass_qty" inputmode="decimal" style="width:80px"></td><td>{{ fmtQty(lineReject(line)) }}</td><td><input v-model="line.concession_qty" inputmode="decimal" style="width:70px"></td><td><input v-model="line.pending_qty" inputmode="decimal" style="width:70px"></td><td><input v-model="line.accepted_qty" inputmode="decimal" :placeholder="line.pass_qty || '0'" style="width:80px"></td><td><input v-model="line.unit_price" inputmode="decimal" style="width:80px"></td><td>{{ lineAmount(line) }}</td><td><input v-model.trim="line.remark" placeholder="差异说明" style="width:120px"></td><td><button type="button" class="button link text-danger" @click="removeLine(index)">删除</button></td></tr></tbody></table></div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="save">保存草稿</button></template>
  </ModalDialog>

  <ModalDialog v-if="pickModal === 'warehouse'" title="仓库资料 · 新增 / 浏览" @close="pickModal = ''">
    <div v-if="pickError" class="notice error">{{ pickError }}</div>
    <div class="pick-form">
      <input v-model.trim="pickForm.code" placeholder="仓库编码（留空自动生成）">
      <input v-model.trim="pickForm.name" placeholder="仓库名称">
      <FilterSelect v-model="pickForm.warehouse_type" :options="warehouseTypeOptions" placeholder="仓库类型"/>
      <FilterSelect v-model="pickForm.manager_employee_id" :options="employeeOptions" placeholder="负责人（可选）"/>
      <button type="button" class="button small" :disabled="pickSaving" @click="savePick">新增并选中</button>
    </div>
    <div class="table-wrap" style="max-height: 300px"><table><thead><tr><th>编码</th><th>名称</th><th>类型</th><th>状态</th><th>操作</th></tr></thead><tbody>
      <tr v-for="w in warehouses" :key="w.id"><td>{{ w.code }}</td><td>{{ w.name }}</td><td>{{ w.warehouse_type === 'finished' ? '成品仓' : '原料仓' }}</td><td>{{ w.status === 'active' ? '启用' : '停用' }}</td><td><button type="button" class="button link" @click="choosePick(w)">选择</button></td></tr>
    </tbody></table></div>
  </ModalDialog>

  <ModalDialog v-if="pickModal === 'supplier'" title="供应商资料 · 新增 / 浏览" @close="pickModal = ''">
    <div v-if="pickError" class="notice error">{{ pickError }}</div>
    <div class="pick-form">
      <input v-model.trim="pickForm.code" placeholder="供应商编码（留空自动生成）">
      <input v-model.trim="pickForm.name" placeholder="供应商名称">
      <input v-model.trim="pickForm.contact_name" placeholder="联系人（可选）">
      <input v-model.trim="pickForm.phone" placeholder="电话（可选）">
      <input v-model.trim="pickForm.address" placeholder="地址（可选）">
      <button type="button" class="button small" :disabled="pickSaving" @click="savePick">新增并选中</button>
    </div>
    <div class="table-wrap" style="max-height: 300px"><table><thead><tr><th>编码</th><th>名称</th><th>联系人</th><th>电话</th><th>状态</th><th>操作</th></tr></thead><tbody>
      <tr v-for="s in suppliers" :key="s.id"><td>{{ s.code }}</td><td>{{ s.name }}</td><td>{{ s.contact_name || '—' }}</td><td>{{ s.phone || '—' }}</td><td>{{ s.status === 'active' ? '启用' : '停用' }}</td><td><button type="button" class="button link" @click="choosePick(s)">选择</button></td></tr>
    </tbody></table></div>
  </ModalDialog>

  <ModalDialog v-if="materialPickModal" title="选择供应商物料" @close="materialPickModal = false">
    <div v-if="pickError" class="notice error">{{ pickError }}</div>
    <div class="pick-form">
      <input v-model.trim="materialPick.keyword" placeholder="物料编码/名称关键字" @keyup.enter="materialPick.page = 1; loadSupplierMaterials()">
      <button type="button" class="button small" :disabled="materialPick.loading" @click="materialPick.page = 1; loadSupplierMaterials()">查询</button>
      <span class="text-muted">按最近提供时间倒序，可多选</span>
    </div>
    <div class="table-wrap" style="max-height: 320px"><table><thead><tr><th></th><th>物料编码</th><th>名称</th><th>颜色</th><th>色号</th><th>规格</th><th>单位</th><th>最近提供</th></tr></thead><tbody>
      <tr v-for="m in materialPick.items" :key="m.material_id"><td><input type="checkbox" :checked="materialPick.selected.includes(m.material_id)" @change="toggleMaterialPick(m.material_id)"></td><td>{{ m.code }}</td><td>{{ m.name }}</td><td><input v-model.trim="m.color_no" placeholder="颜色" style="width:80px"></td><td><input v-model.trim="m.bed_no" placeholder="色号" style="width:80px"></td><td>{{ m.specification || '—' }}</td><td>{{ m.unit || '—' }}</td><td>{{ m.supplied_at ? new Date(m.supplied_at / 1000).toLocaleDateString('zh-CN') : '—' }}</td></tr>
      <tr v-if="!materialPick.items.length"><td colspan="8" class="text-muted" style="text-align:center;padding:16px">暂无该供应商的物料，可关闭后直接手填</td></tr>
    </tbody></table></div>
    <div class="pick-pager"><button type="button" class="button secondary small" :disabled="materialPick.page <= 1" @click="changeMaterialPage(materialPick.page - 1)">上一页</button><span>第 {{ materialPick.page }} 页 / 共 {{ Math.max(1, Math.ceil(materialPick.total / materialPick.page_size)) }} 页（{{ materialPick.total }} 项）</span><button type="button" class="button secondary small" :disabled="materialPick.page * materialPick.page_size >= materialPick.total" @click="changeMaterialPage(materialPick.page + 1)">下一页</button></div>
    <template #footer><button class="button secondary" @click="materialPickModal = false">取消</button><button class="button" @click="confirmSupplierMaterials">添加 {{ materialPick.selected.length }} 项到明细</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'detail'" wide :title="`入库单 · ${detail?.header?.receipt_no || ''}`" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="quantity-summary"><span>客户<strong>{{ detail?.header?.customer_name || '—' }}</strong></span><span>产品名称<strong>{{ detail?.header?.product_name || '—' }}</strong></span><span>款号<strong>{{ detail?.header?.style_no || detail?.header?.product_code || '—' }}</strong></span><span>款式<strong>{{ detail?.header?.style_name || '—' }}</strong></span><span>项目<strong>{{ detail?.header?.project_code || '—' }}</strong></span><span>仓库<strong>{{ detail?.header?.warehouse_name }}</strong></span><span>供应商<strong>{{ detail?.header?.supplier_name || '—' }}</strong></span><span>状态<strong>{{ statusText[detail?.header?.status] || detail?.header?.status }}</strong></span></div>
    <div class="table-wrap" style="max-height:46vh"><table><thead><tr><th>物料</th><th>颜色/色号</th><th>缸号</th><th>来料</th><th>合格</th><th>不合格</th><th>让步</th><th>待检</th><th>实收</th><th>单价</th><th>金额</th><th>IQC</th><th>检验人</th><th>操作</th></tr></thead><tbody><tr v-for="line in detail?.lines || []" :key="line.id"><td>{{ line.material_name || line.material_id }}<small class="text-muted" style="display:block">{{ line.specification }}</small></td><td>{{ line.color_no }}/{{ line.bed_no }}</td><td>{{ line.batch_no || '—' }}</td><td>{{ fmtQty(line.received_qty) }}</td><td>{{ fmtQty(line.pass_qty) }}</td><td>{{ fmtQty(line.reject_qty) }}</td><td>{{ fmtQty(line.concession_qty) }}</td><td>{{ fmtQty(line.pending_qty) }}</td><td>{{ fmtQty(line.accepted_qty) }}</td><td>{{ fmtMoney(line.unit_price) }}</td><td>{{ fmtMoney(line.amount) }}</td><td>{{ iqcText[line.iqc_result] || '未检' }}</td><td>{{ line.inspector_name || '—' }}</td><td><button v-if="canWrite && detail?.header?.status !== 'reversed'" class="button link" @click="openIQC(line)">IQC</button></td></tr></tbody></table></div>
    <template #footer><button class="button secondary" @click="modal = ''">关闭</button><button v-if="canWrite && detail?.header?.status === 'draft'" class="button" :disabled="saving" @click="submitReceipt">提交入库</button><button v-if="canWrite && detail?.header?.status === 'submitted'" class="button danger" :disabled="saving" @click="reverseReceipt">冲销</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'iqc'" title="IQC 检验" @close="modal = 'detail'">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid"><div class="field"><label class="required">检验结果</label><FilterSelect v-model="iqc.result" :options="[{value:'pass',label:'合格'},{value:'fail',label:'不合格'},{value:'concession',label:'让步接收'}]"/></div><div class="field full"><label>缺陷描述</label><textarea v-model="iqc.defect_description" rows="3"></textarea></div></div>
    <template #footer><button class="button secondary" @click="modal = 'detail'">取消</button><button class="button" :disabled="saving" @click="saveIQC">保存检验</button></template>
  </ModalDialog>
</template>
<style scoped>
/* 入库单表单：FilterSelect 撑满所在列，与普通 input 等宽（项目号=客户，产品名称=产品款号） */
.receipt-form :deep(.filter-select) { display: block; width: 100%; }
.receipt-form :deep(.filter-select > input) { width: 100%; min-width: 0; }
.field-with-action { display: flex; align-items: center; gap: 6px; min-width: 0; }
.field-with-action > :first-child { flex: 1 1 auto; min-width: 0; }
.field-with-action :deep(.filter-select) { display: block; }
.field-with-action :deep(.filter-select > input) { width: 100%; }
.pick-form { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 14px; }
.pick-form input { flex: 1 1 150px; min-width: 120px; }
.receipt-form-section { position: relative; flex: none; }
.form-collapse-icon { position: absolute; top: 0; right: 0; z-index: 2; border: 0; background: transparent; color: #2563eb; font-size: 14px; cursor: pointer; padding: 2px 6px; border-radius: 6px; }
.form-collapse-icon:hover { background: #eff4ff; }
.form-collapsed-hint { height: 34px; display: flex; align-items: center; color: #98a2b3; font-size: 12px; }
.detail-actions { display: flex; gap: 8px; flex: none; }
.pick-pager { display: flex; align-items: center; justify-content: flex-end; gap: 10px; margin-top: 10px; }
</style>
