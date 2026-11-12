<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('masterdata:write'))
const canIssue = computed(() => has('work_order:write'))
const tab = ref<'materials' | 'suppliers' | 'warehouses' | 'stock' | 'issues'>('materials')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref<'' | 'material' | 'supplier' | 'warehouse' | 'issue'>('')
const editing = ref<any>(null)

const materials = ref<any[]>([])
const suppliers = ref<any[]>([])
const warehouses = ref<any[]>([])
const employees = ref<any[]>([])
const stock = ref<any[]>([])
const issues = ref<any[]>([])
const workOrders = ref<any[]>([])

const categoryOptions = [
  { value: 'fabric', label: '面料' },
  { value: 'accessory', label: '辅料' },
  { value: 'packaging', label: '包装' },
  { value: 'other', label: '其他' },
]
const warehouseTypeOptions = [
  { value: 'raw', label: '原料仓' },
  { value: 'finished', label: '成品仓' },
]
const supplierOptions = computed(() => suppliers.value.map(s => ({ value: s.id, label: `${s.code} · ${s.name}` })))
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: `${e.code} · ${e.name}` })))
const workOrderOptions = computed(() => workOrders.value.map(w => ({ value: w.id, label: `${w.work_order_code} · ${w.product_name}` })))
const categoryText: Record<string, string> = { fabric: '面料', accessory: '辅料', packaging: '包装', other: '其他' }
const warehouseTypeText: Record<string, string> = { raw: '原料仓', finished: '成品仓' }
const issueStatusText: Record<string, string> = { posted: '已领料', reversed: '已冲销' }
const alertText: Record<string, string> = { normal: '正常', low: '低于安全库存', out_of_stock: '缺料' }
const stockAlerts = computed(() => stock.value.filter(r => r.alert_status && r.alert_status !== 'normal'))
const supplierName = (id: string) => suppliers.value.find(s => s.id === id)?.name || '—'
const employeeName = (id: string) => employees.value.find(e => e.id === id)?.name || '—'

const materialForm = reactive({ code: '', name: '', category: 'fabric', specification: '', unit: '米', default_supplier_id: '', safety_stock: '0', version: 0 })
const supplierForm = reactive({ code: '', name: '', contact_name: '', phone: '', address: '', remark: '', version: 0 })
const warehouseForm = reactive({ code: '', name: '', warehouse_type: 'raw', manager_employee_id: '', version: 0 })
const issueForm = reactive({ issue_no: '', issue_date: '', work_order_id: '', warehouse_id: '', material_id: '', qty: '', purpose: '', operator_employee_id: '', remark: '' })
const currentStock = computed(() => {
  if (!issueForm.material_id || !issueForm.warehouse_id) return '—'
  const row = stock.value.find(r => r.material_id === issueForm.material_id && r.warehouse_id === issueForm.warehouse_id)
  return row ? `${row.qty} ${row.unit}` : '0'
})

async function load() {
  loading.value = true; error.value = ''
  try {
    const [m, s, w, e] = await Promise.all([
      api<{ items: any[] }>('/materials'),
      api<{ items: any[] }>('/suppliers'),
      api<{ items: any[] }>('/warehouses'),
      api<{ items: any[] }>('/employees'),
    ])
    materials.value = m.items; suppliers.value = s.items; warehouses.value = w.items; employees.value = e.items
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
  try { stock.value = (await api<{ items: any[] }>('/inventory/summary')).items } catch { stock.value = [] }
  try { issues.value = (await api<{ items: any[] }>('/material-issues')).items } catch { issues.value = [] }
  try { workOrders.value = (await api<{ items: any[] }>('/work-orders')).items } catch { workOrders.value = [] }
}

function openIssue() {
  error.value = ''
  Object.assign(issueForm, { issue_no: '', issue_date: new Date().toISOString().slice(0, 10), work_order_id: '', warehouse_id: '', material_id: '', qty: '', purpose: '', operator_employee_id: '', remark: '' })
  modal.value = 'issue'
}

async function createIssue() {
  if (!issueForm.issue_no.trim() || !issueForm.work_order_id || !issueForm.warehouse_id || !issueForm.material_id || !issueForm.qty) { error.value = '领料单号、工单、仓库、物料和数量必填'; return }
  saving.value = true; error.value = ''
  try {
    await api('/material-issues', { method: 'POST', body: JSON.stringify({ ...issueForm, issue_no: issueForm.issue_no.trim(), purpose: issueForm.purpose || null, operator_employee_id: issueForm.operator_employee_id || null, remark: issueForm.remark || null }) })
    modal.value = ''; notice.value = '生产领料已提交并扣减物料库存'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

async function reverseIssue(item: any) {
  const reason = window.prompt(`请输入冲销领料单 ${item.issue_no} 的原因`)
  if (!reason) return
  error.value = ''
  try { await api(`/material-issues/${item.id}/reverse`, { method: 'POST', body: JSON.stringify({ reason }) }); notice.value = '领料单已冲销，库存已回补'; await load() } catch (e) { error.value = (e as Error).message }
}

function openCreate(type: 'material' | 'supplier' | 'warehouse') {
  editing.value = null; error.value = ''
  if (type === 'material') Object.assign(materialForm, { code: '', name: '', category: 'fabric', specification: '', unit: '米', default_supplier_id: '', safety_stock: '0', version: 0 })
  if (type === 'supplier') Object.assign(supplierForm, { code: '', name: '', contact_name: '', phone: '', address: '', remark: '', version: 0 })
  if (type === 'warehouse') Object.assign(warehouseForm, { code: '', name: '', warehouse_type: 'raw', manager_employee_id: '', version: 0 })
  modal.value = type
}

function openEdit(type: 'material' | 'supplier' | 'warehouse', item: any) {
  editing.value = item; error.value = ''
  if (type === 'material') Object.assign(materialForm, { code: item.code || '', name: item.name || '', category: item.category || 'fabric', specification: item.specification || '', unit: item.unit || '米', default_supplier_id: item.default_supplier_id || '', safety_stock: item.safety_stock || '0', version: item.version || 0 })
  if (type === 'supplier') Object.assign(supplierForm, { code: item.code || '', name: item.name || '', contact_name: item.contact_name || '', phone: item.phone || '', address: item.address || '', remark: item.remark || '', version: item.version || 0 })
  if (type === 'warehouse') Object.assign(warehouseForm, { code: item.code || '', name: item.name || '', warehouse_type: item.warehouse_type || 'raw', manager_employee_id: item.manager_employee_id || '', version: item.version || 0 })
  modal.value = type
}

async function save(type: 'material' | 'supplier' | 'warehouse') {
  saving.value = true; error.value = ''
  try {
    if (type === 'material') {
      const body: any = { code: materialForm.code.trim(), name: materialForm.name.trim(), category: materialForm.category, specification: materialForm.specification || null, unit: materialForm.unit, default_supplier_id: materialForm.default_supplier_id || null, safety_stock: materialForm.safety_stock || '0' }
      if (editing.value) await api(`/materials/${editing.value.id}`, { method: 'PATCH', body: JSON.stringify({ ...body, version: materialForm.version }) })
      else await api('/materials', { method: 'POST', body: JSON.stringify(body) })
      notice.value = editing.value ? '物料资料已保存' : '物料已创建'
    }
    if (type === 'supplier') {
      const body: any = { code: supplierForm.code.trim(), name: supplierForm.name.trim(), contact_name: supplierForm.contact_name || null, phone: supplierForm.phone || null, address: supplierForm.address || null, remark: supplierForm.remark || null }
      if (editing.value) await api(`/suppliers/${editing.value.id}`, { method: 'PATCH', body: JSON.stringify({ ...body, version: supplierForm.version }) })
      else await api('/suppliers', { method: 'POST', body: JSON.stringify(body) })
      notice.value = editing.value ? '供应商资料已保存' : '供应商已创建'
    }
    if (type === 'warehouse') {
      const body: any = { code: warehouseForm.code.trim(), name: warehouseForm.name.trim(), warehouse_type: warehouseForm.warehouse_type, manager_employee_id: warehouseForm.manager_employee_id || null }
      if (editing.value) await api(`/warehouses/${editing.value.id}`, { method: 'PATCH', body: JSON.stringify({ ...body, version: warehouseForm.version }) })
      else await api('/warehouses', { method: 'POST', body: JSON.stringify(body) })
      notice.value = editing.value ? '仓库资料已保存' : '仓库已创建'
    }
    modal.value = ''; await load()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    saving.value = false
  }
}

async function disable(type: 'material' | 'supplier' | 'warehouse', item: any) {
  const reason = window.prompt(`请输入停用 ${item.name} 的原因`)
  if (!reason) return
  error.value = ''
  try {
    await api(`/${type === 'material' ? 'materials' : type === 'supplier' ? 'suppliers' : 'warehouses'}/${item.id}/disable`, { method: 'POST', body: JSON.stringify({ version: item.version, reason }) })
    notice.value = `${item.name} 已停用`; await load()
  } catch (e) { error.value = (e as Error).message }
}

onMounted(load)
</script>

<template>
  <div class="page-header">
    <div><h1>物料管理</h1><p>物料、供应商、仓库主数据与物料库存汇总</p></div>
    <div class="header-actions">
      <button v-if="canWrite && tab === 'materials'" class="button" @click="openCreate('material')">+ 新增物料</button>
      <button v-if="canWrite && tab === 'suppliers'" class="button" @click="openCreate('supplier')">+ 新增供应商</button>
      <button v-if="canWrite && tab === 'warehouses'" class="button" @click="openCreate('warehouse')">+ 新增仓库</button>
      <button v-if="canIssue && tab === 'issues'" class="button" @click="openIssue">+ 新增领料</button>
      <button class="button secondary" @click="load">刷新</button>
    </div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>

  <section class="panel">
    <div class="tabs">
      <button :class="{ active: tab === 'materials' }" @click="tab = 'materials'">物料资料</button>
      <button :class="{ active: tab === 'suppliers' }" @click="tab = 'suppliers'">供应商</button>
      <button :class="{ active: tab === 'warehouses' }" @click="tab = 'warehouses'">仓库</button>
      <button :class="{ active: tab === 'stock' }" @click="tab = 'stock'">物料库存</button>
      <button :class="{ active: tab === 'issues' }" @click="tab = 'issues'">生产领料</button>
    </div>
    <PageState :loading="loading" />

    <div v-if="!loading && tab === 'materials'" class="table-wrap"><table>
      <thead><tr><th>物料编码</th><th>名称</th><th>类别</th><th>规格</th><th>单位</th><th>安全库存</th><th>默认供应商</th><th>状态</th><th>操作</th></tr></thead>
      <tbody><tr v-for="item in materials" :key="item.id"><td><strong>{{ item.code }}</strong></td><td>{{ item.name }}</td><td>{{ categoryText[item.category] || item.category || '—' }}</td><td>{{ item.specification || '—' }}</td><td>{{ item.unit }}</td><td>{{ item.safety_stock ?? '0' }}</td><td>{{ supplierName(item.default_supplier_id) }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td><button v-if="canWrite" class="button link" @click="openEdit('material', item)">编辑</button><button v-if="canWrite && item.status === 'active'" class="button link text-danger" @click="disable('material', item)">停用</button></td></tr></tbody>
    </table><PageState :empty="!materials.length" /></div>

    <div v-if="!loading && tab === 'suppliers'" class="table-wrap"><table>
      <thead><tr><th>供应商编码</th><th>名称</th><th>联系人</th><th>电话</th><th>地址</th><th>状态</th><th>操作</th></tr></thead>
      <tbody><tr v-for="item in suppliers" :key="item.id"><td><strong>{{ item.code }}</strong></td><td>{{ item.name }}</td><td>{{ item.contact_name || '—' }}</td><td>{{ item.phone || '—' }}</td><td>{{ item.address || '—' }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td><button v-if="canWrite" class="button link" @click="openEdit('supplier', item)">编辑</button><button v-if="canWrite && item.status === 'active'" class="button link text-danger" @click="disable('supplier', item)">停用</button></td></tr></tbody>
    </table><PageState :empty="!suppliers.length" /></div>

    <div v-if="!loading && tab === 'warehouses'" class="table-wrap"><table>
      <thead><tr><th>仓库编码</th><th>名称</th><th>类型</th><th>负责人</th><th>状态</th><th>操作</th></tr></thead>
      <tbody><tr v-for="item in warehouses" :key="item.id"><td><strong>{{ item.code }}</strong></td><td>{{ item.name }}</td><td>{{ warehouseTypeText[item.warehouse_type] || item.warehouse_type }}</td><td>{{ employeeName(item.manager_employee_id) }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td><button v-if="canWrite" class="button link" @click="openEdit('warehouse', item)">编辑</button><button v-if="canWrite && item.status === 'active'" class="button link text-danger" @click="disable('warehouse', item)">停用</button></td></tr></tbody>
    </table><PageState :empty="!warehouses.length" /></div>

    <div v-if="!loading && tab === 'stock'">
      <div v-if="stockAlerts.length" class="notice error" style="margin:12px 16px 0">库存预警：{{ stockAlerts.length }} 项物料低于安全库存或已缺料，请及时补料。</div>
      <div class="table-wrap"><table>
        <thead><tr><th>物料编码</th><th>物料名称</th><th>规格</th><th>单位</th><th>仓库</th><th>库存数量</th><th>安全库存</th><th>预警状态</th><th>缺口</th></tr></thead>
        <tbody><tr v-for="(item, index) in stock" :key="index"><td><strong>{{ item.material_code }}</strong></td><td>{{ item.material_name }}</td><td>{{ item.specification || '—' }}</td><td>{{ item.unit }}</td><td>{{ item.warehouse_code }} · {{ item.warehouse_name }}</td><td>{{ item.qty }}</td><td>{{ item.safety_stock }}</td><td><span class="status" :class="item.alert_status">{{ alertText[item.alert_status] || item.alert_status }}</span></td><td>{{ item.shortage }}</td></tr></tbody>
      </table><PageState :empty="!stock.length" /></div>
    </div>

    <div v-if="!loading && tab === 'issues'" class="table-wrap"><table>
      <thead><tr><th>领料单号</th><th>日期</th><th>生产工单</th><th>项目</th><th>物料</th><th>仓库</th><th>数量</th><th>用途</th><th>领料人</th><th>状态</th><th>操作</th></tr></thead>
      <tbody><tr v-for="item in issues" :key="item.id"><td><strong>{{ item.issue_no }}</strong></td><td>{{ item.issue_date }}</td><td>{{ item.work_order_code }}</td><td>{{ item.project_code }}</td><td>{{ item.material_code }} · {{ item.material_name }}</td><td>{{ item.warehouse_code }} · {{ item.warehouse_name }}</td><td>{{ item.qty }} {{ item.unit }}</td><td>{{ item.purpose || '—' }}</td><td>{{ item.operator_name || '—' }}</td><td><span class="status" :class="item.status">{{ issueStatusText[item.status] || item.status }}</span></td><td><button v-if="canIssue && item.status === 'posted'" class="button link text-danger" @click="reverseIssue(item)">冲销</button></td></tr></tbody>
    </table><PageState :empty="!issues.length" /></div>
  </section>

  <ModalDialog v-if="modal === 'material'" :title="editing ? '编辑物料' : '新增物料'" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label>物料编码</label><input v-model.trim="materialForm.code" :placeholder="editing ? '' : '留空自动生成（如 WL-00001）'"></div>
      <div class="field"><label class="required">物料名称</label><input v-model.trim="materialForm.name"></div>
      <div class="field"><label>类别</label><FilterSelect v-model="materialForm.category" :options="categoryOptions" /></div>
      <div class="field"><label>规格</label><input v-model.trim="materialForm.specification" placeholder="如 150cm"></div>
      <div class="field"><label class="required">单位</label><input v-model.trim="materialForm.unit" placeholder="米 / 个 / 卷"></div>
      <div class="field"><label>安全库存</label><input v-model.trim="materialForm.safety_stock" inputmode="decimal" placeholder="0"></div>
      <div class="field"><label>默认供应商</label><FilterSelect v-model="materialForm.default_supplier_id" :options="supplierOptions" placeholder="可不选" /></div>
    </div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !materialForm.name || !materialForm.unit || (editing && !materialForm.code)" @click="save('material')">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'supplier'" :title="editing ? '编辑供应商' : '新增供应商'" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label>供应商编码</label><input v-model.trim="supplierForm.code" :placeholder="editing ? '' : '留空自动生成（如 GYS-00001）'"></div>
      <div class="field"><label class="required">供应商名称</label><input v-model.trim="supplierForm.name"></div>
      <div class="field"><label>联系人</label><input v-model.trim="supplierForm.contact_name"></div>
      <div class="field"><label>电话</label><input v-model.trim="supplierForm.phone"></div>
      <div class="field full"><label>地址</label><input v-model.trim="supplierForm.address"></div>
      <div class="field full"><label>备注</label><textarea v-model="supplierForm.remark" rows="2"></textarea></div>
    </div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !supplierForm.name || (editing && !supplierForm.code)" @click="save('supplier')">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'warehouse'" :title="editing ? '编辑仓库' : '新增仓库'" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label>仓库编码</label><input v-model.trim="warehouseForm.code" :placeholder="editing ? '' : '留空自动生成（如 CK-00001）'"></div>
      <div class="field"><label class="required">仓库名称</label><input v-model.trim="warehouseForm.name"></div>
      <div class="field"><label>仓库类型</label><FilterSelect v-model="warehouseForm.warehouse_type" :options="warehouseTypeOptions" /></div>
      <div class="field"><label>负责人</label><FilterSelect v-model="warehouseForm.manager_employee_id" :options="employeeOptions" placeholder="可不选" /></div>
    </div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !warehouseForm.name || (editing && !warehouseForm.code)" @click="save('warehouse')">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'issue'" title="新增生产领料" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label class="required">领料单号</label><input v-model.trim="issueForm.issue_no" placeholder="如 MI-20261103-01"></div>
      <div class="field"><label>领料日期</label><input v-model.trim="issueForm.issue_date" placeholder="YYYY-MM-DD"></div>
      <div class="field full"><label class="required">生产工单</label><FilterSelect v-model="issueForm.work_order_id" :options="workOrderOptions" placeholder="选择工单" /></div>
      <div class="field"><label class="required">仓库</label><FilterSelect v-model="issueForm.warehouse_id" :options="warehouses.map(w => ({ value: w.id, label: `${w.code} · ${w.name}` }))" placeholder="选择仓库" /></div>
      <div class="field"><label class="required">物料</label><FilterSelect v-model="issueForm.material_id" :options="materials.map(m => ({ value: m.id, label: `${m.code} · ${m.name}` }))" placeholder="选择物料" /></div>
      <div class="field"><label class="required">领料数量</label><input v-model.trim="issueForm.qty" inputmode="decimal" placeholder="0.000000"></div>
      <div class="field"><label>用途</label><input v-model.trim="issueForm.purpose" placeholder="如 裁剪领料"></div>
      <div class="field"><label>领料人</label><FilterSelect v-model="issueForm.operator_employee_id" :options="employeeOptions" placeholder="选择员工" /></div>
      <div class="field full"><label>备注</label><input v-model.trim="issueForm.remark"></div>
    </div>
    <div class="notice">当前物料/仓库库存：{{ currentStock }}。领料提交后写入负向库存流水，库存不足会被拒绝。</div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !issueForm.issue_no || !issueForm.work_order_id || !issueForm.warehouse_id || !issueForm.material_id || !issueForm.qty" @click="createIssue">提交领料</button></template>
  </ModalDialog>
</template>
<style scoped>
.status.low { background: #fff4e5; color: #b54708; }
.status.out_of_stock { background: #fee4e2; color: #b42318; }
</style>
