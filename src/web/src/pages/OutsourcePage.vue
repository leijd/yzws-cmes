<script setup lang="ts">
/* 外协管理（设计 4.25/5.14/6.8）：厂商档案、协作授权、委外单、受托加工、结算 五页签 */
import { computed, onMounted, reactive, ref } from 'vue'
import { api, formatTime } from '../api'
import { toastError, toastSuccess } from '../toast'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import FilterSelect from '../components/FilterSelect.vue'
import PageState from '../components/PageState.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('outsource:write'))
const canSettle = computed(() => has('outsource:settle'))

const tab = ref<'partners' | 'collabs' | 'orders' | 'incoming' | 'settlements'>('orders')
const loading = ref(false)

const orderStatus: Record<string, string> = { draft: '草稿', issued: '已发出', in_transit: '在途', processing: '加工中', partially_returned: '部分回货', completed: '已完成', settled: '已结算', cancelled: '已取消' }
const collabStatus: Record<string, string> = { pending: '待确认', active: '生效中', suspended: '已暂停', terminated: '已终止' }
const settleStatus: Record<string, string> = { draft: '草稿', confirmed: '已确认', invoiced: '已开票', paid: '已收付' }
const scopeText: Record<string, string> = { order: '整单', style: '款号', processes: '工序' }

/* ===== 厂商档案 ===== */
const partners = ref<any[]>([])
const partnerModal = ref(false)
const partnerForm = reactive({ id: '', code: '', name: '', partner_tenant_code: '', contact_name: '', contact_phone: '', address: '', qualification_note: '', remark: '', version: 0 })
async function loadPartners() {
  loading.value = true
  try { partners.value = (await api<{ items: any[] }>('/outsource/partners')).items } finally { loading.value = false }
}
function openPartner(item?: any) {
  Object.assign(partnerForm, item ? { ...item, partner_tenant_code: '' } : { id: '', code: '', name: '', partner_tenant_code: '', contact_name: '', contact_phone: '', address: '', qualification_note: '', remark: '', version: 0 })
  partnerModal.value = true
}
async function savePartner() {
  try {
    if (partnerForm.id) await api(`/outsource/partners/${partnerForm.id}`, { method: 'PATCH', body: JSON.stringify(partnerForm) })
    else await api('/outsource/partners', { method: 'POST', body: JSON.stringify(partnerForm) })
    toastSuccess('已保存'); partnerModal.value = false; await loadPartners()
  } catch { /* toast 已提示 */ }
}
async function disablePartner(item: any) {
  if (!window.confirm(`确认停用厂商「${item.name}」？`)) return
  try { await api(`/outsource/partners/${item.id}/disable`, { method: 'POST', body: '{}' }); toastSuccess('已停用'); await loadPartners() } catch { /* */ }
}

/* ===== 协作授权 ===== */
const collabs = ref<any[]>([])
const collabModal = ref(false)
const collabForm = reactive({ peer_tenant_code: '', my_role: 'consignor', scope_note: '' })
async function loadCollabs() {
  try { collabs.value = (await api<{ items: any[] }>('/outsource/collaborations')).items } catch { /* */ }
}
async function createCollab() {
  try { await api('/outsource/collaborations', { method: 'POST', body: JSON.stringify(collabForm) }); toastSuccess('已发起，待对方确认'); collabModal.value = false; await loadCollabs() } catch { /* */ }
}
async function collabAction(item: any, action: string) {
  if (action === 'terminate' && !window.confirm('终止后不可逆，历史数据保留但对方立即可见性切断。确认终止？')) return
  const reason = action === 'terminate' ? (window.prompt('请输入终止原因') || '') : ''
  if (action === 'terminate' && !reason.trim()) { toastError('终止必须填写原因'); return }
  try { await api(`/outsource/collaborations/${item.id}/${action}`, { method: 'POST', body: JSON.stringify({ reason }) }); toastSuccess('操作成功'); await loadCollabs() } catch { /* */ }
}

/* ===== 委外单 ===== */
const orders = ref<any[]>([])
const orderModal = ref(false)
const orderForm = reactive({ partner_id: '', scope_type: 'order', pricing_type: 'piece', agreed_due_date: '', remark: '', items: [] as any[] })
const processes = ref<any[]>([])
async function loadOrders() {
  loading.value = true
  try { orders.value = (await api<{ items: any[] }>('/outsource/orders')).items } finally { loading.value = false }
}
async function loadProcesses() {
  try { processes.value = (await api<{ items: any[] }>('/processes?status=active')).items || [] } catch { processes.value = [] }
}
function openOrderCreate() {
  Object.assign(orderForm, { partner_id: '', scope_type: 'order', pricing_type: 'piece', agreed_due_date: '', remark: '', items: [{ process_id: '', segment: '', qty: 0, unit_price: null }] })
  orderModal.value = true
}
function addItem() { orderForm.items.push({ process_id: '', segment: '', qty: 0, unit_price: null }) }
async function saveOrder() {
  try { await api('/outsource/orders', { method: 'POST', body: JSON.stringify(orderForm) }); toastSuccess('委外单已创建（草稿）'); orderModal.value = false; await loadOrders() } catch { /* */ }
}

/* 委外单详情 */
const detail = ref<any>(null)
const detailTab = ref<'items' | 'flows' | 'progress' | 'settle'>('items')
const progressData = ref<any>(null)
async function openDetail(item: any) {
  try {
    detail.value = await api(`/outsource/orders/${item.id}`)
    detailTab.value = 'items'
    progressData.value = await api(`/outsource/orders/${item.id}/progress`)
  } catch { /* */ }
}
async function orderAction(action: string) {
  if (!detail.value) return
  const id = detail.value.order.id
  if (action === 'cancel' && !window.confirm('确认取消该委外单？')) return
  try {
    await api(`/outsource/orders/${id}/${action}`, { method: 'POST', body: '{}' })
    toastSuccess(action === 'issue' ? '委外单已发出' : '已取消')
    await openDetail(detail.value.order); await loadOrders()
  } catch { /* */ }
}
const flowForm = reactive({ flow_type: 'issue', item_id: '', material_name: '', qty: 0, flow_date: new Date().toISOString().slice(0, 10), voucher_no: '', remark: '' })
const flowModal = ref(false)
async function saveFlow() {
  if (!detail.value) return
  try {
    await api(`/outsource/orders/${detail.value.order.id}/material-flows`, { method: 'POST', body: JSON.stringify(flowForm) })
    toastSuccess('已登记'); flowModal.value = false; await openDetail(detail.value.order); await loadOrders()
  } catch { /* */ }
}
const progressForm = reactive({ completed_qty: 0, in_process_qty: 0, quality_pass_qty: 0, stage_note: '' })
const progressModal = ref(false)
async function saveProgress() {
  if (!detail.value) return
  try {
    await api(`/outsource/orders/${detail.value.order.id}/progress-report`, { method: 'POST', body: JSON.stringify(progressForm) })
    toastSuccess('进度已填报'); progressModal.value = false; await openDetail(detail.value.order)
  } catch { /* */ }
}
const settleForm = reactive({ outsourcing_order_id: '', direction: 'payable', qualified_qty: 0, unit_price: 0, deduction_amount: 0, remark: '' })
const settleModal = ref(false)
function openSettle() {
  if (!detail.value) return
  Object.assign(settleForm, { outsourcing_order_id: detail.value.order.id, direction: 'payable', qualified_qty: 0, unit_price: 0, deduction_amount: 0, remark: '' })
  settleModal.value = true
}
async function saveSettle() {
  try { await api('/outsource/settlements', { method: 'POST', body: JSON.stringify(settleForm) }); toastSuccess('结算单已创建'); settleModal.value = false; await openDetail(detail.value.order); await loadSettlements() } catch { /* */ }
}

/* ===== 受托加工 ===== */
const assigned = ref<any[]>([])
const incoming = ref<any[]>([])
const acceptModal = ref(false)
const acceptForm = reactive({ id: '', order_no: '', product_id: '', qty: 0, scope_note: '' })
const incomingModal = ref(false)
const incomingForm = reactive({ customer_id: '', product_id: '', qty: 0, source_order_ref: '', scope_note: '' })
const customers = ref<any[]>([])
const products = ref<any[]>([])
async function loadAssigned() { try { assigned.value = (await api<{ items: any[] }>('/outsource/assigned')).items } catch { /* */ } }
async function loadIncoming() { try { incoming.value = (await api<{ items: any[] }>('/outsource/incoming')).items } catch { /* */ } }
async function loadRefs() {
  try { customers.value = (await api<{ items: any[] }>('/customers')).items || [] } catch { customers.value = [] }
  try { products.value = (await api<{ items: any[] }>('/products')).items || [] } catch { products.value = [] }
}
function openAccept(item: any) { Object.assign(acceptForm, { id: item.id, order_no: item.order_no, product_id: '', qty: item.total_qty, scope_note: '' }); acceptModal.value = true }
async function doAccept() {
  try { await api(`/outsource/assigned/${acceptForm.id}/accept`, { method: 'POST', body: JSON.stringify(acceptForm) }); toastSuccess('已接受，受托工单已生成'); acceptModal.value = false; await loadAssigned(); await loadIncoming() } catch { /* */ }
}
async function doDecline(item: any) {
  const reason = window.prompt('请输入婉拒原因') || ''
  if (!reason.trim()) return
  try { await api(`/outsource/assigned/${item.id}/decline`, { method: 'POST', body: JSON.stringify({ reason }) }); toastSuccess('已婉拒'); await loadAssigned() } catch { /* */ }
}
async function saveIncoming() {
  try { await api('/outsource/incoming', { method: 'POST', body: JSON.stringify(incomingForm) }); toastSuccess('受托订单已登记'); incomingModal.value = false; await loadIncoming() } catch { /* */ }
}
async function exportProgress(item: any) {
  try {
    const data = await api<any>(`/outsource/incoming/${item.id}/progress-export`)
    const text = `受托工单进度单\n工单号：${data.work_order_code}\n对方单号：${data.source_order_ref || '—'}\n产品：${data.product_name}\n委托方：${data.customer_name}\n计划数量：${data.planned_qty}\n已报工：${data.reported_qty}\n已验收：${data.approved_qty}\n状态：${data.status}\n导出时间：${formatTime(data.exported_at)}`
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = `进度单-${data.work_order_code}.txt`; a.click()
    URL.revokeObjectURL(a.href)
  } catch { /* */ }
}

/* ===== 结算 ===== */
const settlements = ref<any[]>([])
async function loadSettlements() { try { settlements.value = (await api<{ items: any[] }>('/outsource/settlements')).items } catch { /* */ } }
async function settleAction(item: any, action: string) {
  const label: Record<string, string> = { confirm: '确认（金额锁定）', invoice: '登记开票', pay: '登记收付款' }
  if (!window.confirm(`确认${label[action]}结算单 ${item.settlement_no}？`)) return
  try { await api(`/outsource/settlements/${item.id}/${action}`, { method: 'POST', body: '{}' }); toastSuccess('操作成功'); await loadSettlements() } catch { /* */ }
}

onMounted(() => { void loadOrders(); void loadPartners(); void loadCollabs(); void loadAssigned(); void loadIncoming(); void loadSettlements(); void loadRefs(); void loadProcesses() })

const partnerOptions = computed(() => partners.value.filter(x => x.status === 'active').map(x => ({ value: x.id, label: `${x.code} · ${x.name}` })))
const processOptions = computed(() => processes.value.map((x: any) => ({ value: x.id, label: x.name || x.code })))
const customerOptions = computed(() => customers.value.map((x: any) => ({ value: x.id, label: `${x.code} · ${x.name}` })))
const productOptions = computed(() => products.value.map((x: any) => ({ value: x.id, label: `${x.code} · ${x.name}` })))
const scopeOptions = [{ value: 'order', label: '整单外包' }, { value: 'style', label: '按款号' }, { value: 'processes', label: '按工序' }]
const pricingOptions = [{ value: 'piece', label: '计件' }, { value: 'lump', label: '整单包价' }]
const itemOptions = computed(() => (detail.value?.items || []).map((i: any) => ({ value: i.id, label: `${i.process_name || i.segment || '整单'}（委外 ${i.qty}）` })))
</script>

<template>
  <section class="page">
    <header class="page-header">
      <h2>外协管理</h2>
      <div class="toolbar">
        <button class="button" :class="{ secondary: tab !== 'orders' }" @click="tab = 'orders'">委外单</button>
        <button class="button" :class="{ secondary: tab !== 'partners' }" @click="tab = 'partners'">厂商档案</button>
        <button class="button" :class="{ secondary: tab !== 'collabs' }" @click="tab = 'collabs'">协作授权</button>
        <button class="button" :class="{ secondary: tab !== 'incoming' }" @click="tab = 'incoming'">受托加工</button>
        <button class="button" :class="{ secondary: tab !== 'settlements' }" @click="tab = 'settlements'">结算</button>
      </div>
    </header>

    <!-- 委外单 -->
    <div v-if="tab === 'orders'">
      <div class="toolbar">
        <button v-if="canWrite" class="button" @click="openOrderCreate">新增委外单</button>
        <button class="button secondary" @click="loadOrders">刷新</button>
      </div>
      <div class="table-wrap"><table><thead><tr><th>单号</th><th>受托方</th><th>范围</th><th>计价</th><th>委外数</th><th>已回货</th><th>约定回货</th><th>状态</th><th>操作</th></tr></thead>
        <tbody><tr v-for="o in orders" :key="o.id">
          <td><strong>{{ o.order_no }}</strong></td><td>{{ o.partner_name }}</td><td>{{ scopeText[o.scope_type] || o.scope_type }}</td>
          <td>{{ o.pricing_type === 'piece' ? '计件' : '包价' }}</td><td>{{ o.total_qty }}</td><td>{{ o.returned_qty }}</td>
          <td>{{ o.agreed_due_date || '—' }}</td><td><span class="status" :class="o.status">{{ orderStatus[o.status] || o.status }}</span></td>
          <td><button class="button link" @click="openDetail(o)">详情</button></td>
        </tr></tbody></table><PageState :empty="!orders.length" /></div>
    </div>

    <!-- 厂商档案 -->
    <div v-if="tab === 'partners'">
      <div class="toolbar">
        <button v-if="canWrite" class="button" @click="openPartner()">新增厂商</button>
        <button class="button secondary" @click="loadPartners">刷新</button>
      </div>
      <div class="table-wrap"><table><thead><tr><th>编码</th><th>名称</th><th>系统租户</th><th>联系人</th><th>电话</th><th>进行中单</th><th>状态</th><th>操作</th></tr></thead>
        <tbody><tr v-for="pt in partners" :key="pt.id">
          <td><strong>{{ pt.code }}</strong></td><td>{{ pt.name }}</td>
          <td>{{ pt.partner_tenant_name ? pt.partner_tenant_name : '非系统租户' }}</td>
          <td>{{ pt.contact_name || '—' }}</td><td>{{ pt.contact_phone || '—' }}</td><td>{{ pt.active_orders }}</td>
          <td><span class="status" :class="pt.status">{{ pt.status === 'active' ? '启用' : '停用' }}</span></td>
          <td><button v-if="canWrite" class="button link" @click="openPartner(pt)">编辑</button>
            <button v-if="canWrite && pt.status === 'active'" class="button link text-danger" @click="disablePartner(pt)">停用</button></td>
        </tr></tbody></table><PageState :empty="!partners.length" /></div>
    </div>

    <!-- 协作授权 -->
    <div v-if="tab === 'collabs'">
      <div class="toolbar">
        <button v-if="canWrite" class="button" @click="collabModal = true">发起协作</button>
        <button class="button secondary" @click="loadCollabs">刷新</button>
      </div>
      <div class="table-wrap"><table><thead><tr><th>委托方</th><th>受托方</th><th>我的角色</th><th>范围说明</th><th>状态</th><th>操作</th></tr></thead>
        <tbody><tr v-for="c in collabs" :key="c.id">
          <td>{{ c.consignor_name }}</td><td>{{ c.contractor_name }}</td>
          <td>{{ c.my_role === 'consignor' ? '委托方' : '受托方' }}</td><td>{{ c.scope_note || '—' }}</td>
          <td><span class="status" :class="c.status">{{ collabStatus[c.status] || c.status }}</span></td>
          <td>
            <button v-if="canWrite && c.status === 'pending' && !c.proposed_by_me" class="button link" @click="collabAction(c, 'confirm')">确认生效</button>
            <button v-if="canWrite && c.status === 'active'" class="button link" @click="collabAction(c, 'suspend')">暂停</button>
            <button v-if="canWrite && c.status !== 'terminated'" class="button link text-danger" @click="collabAction(c, 'terminate')">终止</button>
          </td>
        </tr></tbody></table><PageState :empty="!collabs.length" /></div>
    </div>

    <!-- 受托加工 -->
    <div v-if="tab === 'incoming'">
      <div class="toolbar">
        <button v-if="canWrite" class="button" @click="incomingModal = true">登记受托订单（非系统租户）</button>
        <button class="button secondary" @click="loadAssigned(); loadIncoming()">刷新</button>
      </div>
      <h3 style="margin:12px 0 8px">待我接受的委外（对方为系统租户）</h3>
      <div class="table-wrap"><table><thead><tr><th>委外单号</th><th>委托方</th><th>范围</th><th>数量</th><th>约定交期</th><th>状态</th><th>操作</th></tr></thead>
        <tbody><tr v-for="a in assigned" :key="a.id">
          <td><strong>{{ a.order_no }}</strong></td><td>{{ a.consignor_name }}</td><td>{{ scopeText[a.scope_type] || a.scope_type }}</td>
          <td>{{ a.total_qty }}</td><td>{{ a.agreed_due_date || '—' }}</td><td><span class="status">{{ orderStatus[a.status] || a.status }}</span></td>
          <td><button v-if="canWrite && a.status === 'issued'" class="button link" @click="openAccept(a)">接受</button>
            <button v-if="canWrite && a.status === 'issued'" class="button link text-danger" @click="doDecline(a)">婉拒</button></td>
        </tr></tbody></table><PageState :empty="!assigned.length" /></div>
      <h3 style="margin:16px 0 8px">本厂受托工单</h3>
      <div class="table-wrap"><table><thead><tr><th>工单号</th><th>对方单号</th><th>委托方</th><th>产品</th><th>计划数</th><th>已验收</th><th>状态</th><th>操作</th></tr></thead>
        <tbody><tr v-for="w in incoming" :key="w.id">
          <td><strong>{{ w.work_order_code }}</strong></td><td>{{ w.source_order_ref || '—' }}</td><td>{{ w.customer_name }}</td>
          <td>{{ w.product_name }}</td><td>{{ w.planned_qty }}</td><td>{{ w.approved_qty }}</td>
          <td><span class="status">{{ w.status }}</span></td>
          <td><button class="button link" @click="exportProgress(w)">导出进度单</button></td>
        </tr></tbody></table><PageState :empty="!incoming.length" /></div>
    </div>

    <!-- 结算 -->
    <div v-if="tab === 'settlements'">
      <div class="toolbar"><button class="button secondary" @click="loadSettlements">刷新</button></div>
      <div class="table-wrap"><table><thead><tr><th>结算单号</th><th>委外单</th><th>对方</th><th>方向</th><th>合格数</th><th>单价</th><th>金额</th><th>扣款</th><th>状态</th><th>操作</th></tr></thead>
        <tbody><tr v-for="s in settlements" :key="s.id">
          <td><strong>{{ s.settlement_no }}</strong></td><td>{{ s.order_no || '—' }}</td><td>{{ s.partner_name || '—' }}</td>
          <td>{{ s.direction === 'payable' ? '应付' : '应收' }}</td><td>{{ s.qualified_qty }}</td><td>{{ s.unit_price }}</td>
          <td>{{ s.amount }}</td><td>{{ s.deduction_amount }}</td>
          <td><span class="status" :class="s.status">{{ settleStatus[s.status] || s.status }}</span></td>
          <td>
            <button v-if="canSettle && s.status === 'draft'" class="button link" @click="settleAction(s, 'confirm')">确认</button>
            <button v-if="canSettle && s.status === 'confirmed'" class="button link" @click="settleAction(s, 'invoice')">开票</button>
            <button v-if="canSettle && (s.status === 'confirmed' || s.status === 'invoiced')" class="button link" @click="settleAction(s, 'pay')">收付</button>
          </td>
        </tr></tbody></table><PageState :empty="!settlements.length" /></div>
    </div>

    <!-- 新增厂商 -->
    <ModalDialog v-if="partnerModal" :title="partnerForm.id ? '编辑厂商' : '新增外协厂商'" @close="partnerModal = false">
      <div class="form-grid">
        <div class="field"><label>编码（留空自动）</label><input v-model.trim="partnerForm.code"></div>
        <div class="field"><label class="required">名称</label><input v-model.trim="partnerForm.name"></div>
        <div class="field full"><label>对方系统租户编码（非租户留空）</label><input v-model.trim="partnerForm.partner_tenant_code" placeholder="如 TD 或 TDZYC" :disabled="!!partnerForm.id"></div>
        <div class="field"><label>联系人</label><input v-model.trim="partnerForm.contact_name"></div>
        <div class="field"><label>联系电话</label><input v-model.trim="partnerForm.contact_phone"></div>
        <div class="field full"><label>地址</label><input v-model.trim="partnerForm.address"></div>
        <div class="field full"><label>资质说明</label><input v-model.trim="partnerForm.qualification_note"></div>
        <div class="field full"><label>备注</label><input v-model.trim="partnerForm.remark"></div>
      </div>
      <template #footer><button class="button secondary" @click="partnerModal = false">取消</button><button class="button" @click="savePartner">保存</button></template>
    </ModalDialog>

    <!-- 发起协作 -->
    <ModalDialog v-if="collabModal" title="发起跨租户协作" @close="collabModal = false">
      <div class="form-grid">
        <div class="field full"><label class="required">对方租户编码</label><input v-model.trim="collabForm.peer_tenant_code" placeholder="厂编码或短码"></div>
        <div class="field full"><label class="required">我的角色</label>
          <FilterSelect v-model="collabForm.my_role" :options="[{ value: 'consignor', label: '我委托对方加工' }, { value: 'contractor', label: '我承接对方加工' }]" /></div>
        <div class="field full"><label>协作范围说明</label><input v-model.trim="collabForm.scope_note" placeholder="工序段、品类等"></div>
      </div>
      <template #footer><button class="button secondary" @click="collabModal = false">取消</button><button class="button" @click="createCollab">发起</button></template>
    </ModalDialog>

    <!-- 新增委外单 -->
    <ModalDialog v-if="orderModal" title="新增委外单" @close="orderModal = false">
      <div class="form-grid">
        <div class="field"><label class="required">受托方</label><FilterSelect v-model="orderForm.partner_id" :options="partnerOptions" /></div>
        <div class="field"><label class="required">委外范围</label><FilterSelect v-model="orderForm.scope_type" :options="scopeOptions" /></div>
        <div class="field"><label class="required">计价方式</label><FilterSelect v-model="orderForm.pricing_type" :options="pricingOptions" /></div>
        <div class="field"><label>约定回货日期</label><input v-model="orderForm.agreed_due_date" type="date"></div>
        <div class="field full"><label>备注</label><input v-model.trim="orderForm.remark"></div>
      </div>
      <h4 style="margin:12px 0 6px">委外明细</h4>
      <div v-for="(it, idx) in orderForm.items" :key="idx" class="form-grid" style="margin-bottom:8px">
        <div class="field" v-if="orderForm.scope_type === 'processes'"><label class="required">工序</label><FilterSelect v-model="it.process_id" :options="processOptions" /></div>
        <div class="field" v-else><label>工序段/说明</label><input v-model.trim="it.segment"></div>
        <div class="field"><label class="required">数量</label><input v-model.number="it.qty" type="number" min="1"></div>
        <div class="field" v-if="orderForm.pricing_type === 'piece'"><label class="required">单价</label><input v-model.number="it.unit_price" type="number" min="0" step="0.01"></div>
        <div class="field" style="align-self:end"><button class="button link text-danger" @click="orderForm.items.splice(idx, 1)" v-if="orderForm.items.length > 1">删除</button></div>
      </div>
      <button class="button secondary" @click="addItem">添加明细行</button>
      <template #footer><button class="button secondary" @click="orderModal = false">取消</button><button class="button" @click="saveOrder">创建</button></template>
    </ModalDialog>

    <!-- 委外单详情 -->
    <ModalDialog v-if="detail" :title="`委外单 ${detail.order.order_no}`" @close="detail = null">
      <div class="quantity-summary">
        <span>受托方<strong>{{ detail.order.partner_name }}</strong></span>
        <span>范围<strong>{{ scopeText[detail.order.scope_type] }}</strong></span>
        <span>委外数<strong>{{ detail.order.total_qty }}</strong></span>
        <span>状态<strong>{{ orderStatus[detail.order.status] }}</strong></span>
      </div>
      <div class="toolbar" style="margin:8px 0">
        <button v-if="canWrite && detail.order.status === 'draft'" class="button" @click="orderAction('issue')">发出</button>
        <button v-if="canWrite && detail.order.status === 'draft'" class="button secondary" @click="orderAction('cancel')">取消</button>
        <button v-if="canWrite && ['issued', 'in_transit', 'processing', 'partially_returned'].includes(detail.order.status)" class="button" @click="flowModal = true">发料/回货</button>
        <button v-if="canWrite && ['issued', 'in_transit', 'processing', 'partially_returned'].includes(detail.order.status)" class="button secondary" @click="progressModal = true">填报对方进度</button>
        <button v-if="canSettle && ['partially_returned', 'completed'].includes(detail.order.status)" class="button secondary" @click="openSettle">生成结算</button>
      </div>
      <div class="toolbar">
        <button class="button link" @click="detailTab = 'items'">明细</button>
        <button class="button link" @click="detailTab = 'flows'">物料流水</button>
        <button class="button link" @click="detailTab = 'progress'">进度</button>
        <button class="button link" @click="detailTab = 'settle'">结算</button>
      </div>
      <div v-if="detailTab === 'items'" class="table-wrap"><table><thead><tr><th>工序/说明</th><th>委外数</th><th>单价</th><th>回货</th><th>返修</th><th>次品</th></tr></thead>
        <tbody><tr v-for="i in detail.items" :key="i.id"><td>{{ i.process_name || i.segment || '整单' }}</td><td>{{ i.qty }}</td><td>{{ i.unit_price ?? '—' }}</td><td>{{ i.returned_qty }}</td><td>{{ i.rework_qty }}</td><td>{{ i.defect_qty }}</td></tr></tbody></table></div>
      <div v-if="detailTab === 'flows'" class="table-wrap"><table><thead><tr><th>类型</th><th>物料</th><th>数量</th><th>日期</th><th>单号</th><th>状态</th></tr></thead>
        <tbody><tr v-for="f in detail.flows" :key="f.id"><td>{{ f.flow_type === 'issue' ? '发料' : '回货' }}</td><td>{{ f.material_name }}</td><td>{{ f.qty }}</td><td>{{ f.flow_date }}</td><td>{{ f.voucher_no || '—' }}</td><td>{{ f.status }}</td></tr></tbody></table><PageState :empty="!detail.flows.length" /></div>
      <div v-if="detailTab === 'progress'">
        <div class="quantity-summary" v-if="progressData">
          <span>本方报工<strong>{{ progressData.own.reported }}</strong></span>
          <span>本方验收<strong>{{ progressData.own.approved }}</strong></span>
          <span>对方完成<strong>{{ progressData.contractor.completed }}</strong></span>
          <span>对方在制<strong>{{ progressData.contractor.in_process }}</strong></span>
          <span>对方质检合格<strong>{{ progressData.contractor.quality_pass }}</strong></span>
        </div>
        <div class="table-wrap" style="margin-top:8px"><table><thead><tr><th>数据时间</th><th>完成</th><th>在制</th><th>质检合格</th><th>说明</th><th>填报人</th></tr></thead>
          <tbody><tr v-for="pr in detail.projections" :key="pr.id"><td>{{ formatTime(pr.reported_at) }}</td><td>{{ pr.completed_qty }}</td><td>{{ pr.in_process_qty }}</td><td>{{ pr.quality_pass_qty }}</td><td>{{ pr.stage_note || '—' }}</td><td>{{ pr.reported_by || '对方同步' }}</td></tr></tbody></table><PageState :empty="!detail.projections.length" /></div>
      </div>
      <div v-if="detailTab === 'settle'" class="table-wrap"><table><thead><tr><th>结算单号</th><th>合格数</th><th>单价</th><th>金额</th><th>扣款</th><th>状态</th></tr></thead>
        <tbody><tr v-for="s in detail.settlements" :key="s.id"><td>{{ s.settlement_no }}</td><td>{{ s.qualified_qty }}</td><td>{{ s.unit_price }}</td><td>{{ s.amount }}</td><td>{{ s.deduction_amount }}</td><td>{{ settleStatus[s.status] }}</td></tr></tbody></table><PageState :empty="!detail.settlements.length" /></div>
    </ModalDialog>

    <!-- 发料/回货 -->
    <ModalDialog v-if="flowModal" title="登记物料流水" @close="flowModal = false">
      <div class="form-grid">
        <div class="field"><label class="required">类型</label><FilterSelect v-model="flowForm.flow_type" :options="[{ value: 'issue', label: '发料' }, { value: 'return', label: '回货' }]" /></div>
        <div class="field"><label>关联明细（回货建议指定）</label><FilterSelect v-model="flowForm.item_id" :options="itemOptions" /></div>
        <div class="field"><label class="required">物料名称</label><input v-model.trim="flowForm.material_name"></div>
        <div class="field"><label class="required">数量</label><input v-model.number="flowForm.qty" type="number" min="1"></div>
        <div class="field"><label class="required">日期</label><input v-model="flowForm.flow_date" type="date"></div>
        <div class="field"><label>单据号</label><input v-model.trim="flowForm.voucher_no"></div>
      </div>
      <template #footer><button class="button secondary" @click="flowModal = false">取消</button><button class="button" @click="saveFlow">登记</button></template>
    </ModalDialog>

    <!-- 进度填报 -->
    <ModalDialog v-if="progressModal" title="填报受托方进度" @close="progressModal = false">
      <div class="form-grid">
        <div class="field"><label>完成数量</label><input v-model.number="progressForm.completed_qty" type="number" min="0"></div>
        <div class="field"><label>在制数量</label><input v-model.number="progressForm.in_process_qty" type="number" min="0"></div>
        <div class="field"><label>质检合格</label><input v-model.number="progressForm.quality_pass_qty" type="number" min="0"></div>
        <div class="field full"><label>阶段说明</label><input v-model.trim="progressForm.stage_note"></div>
      </div>
      <template #footer><button class="button secondary" @click="progressModal = false">取消</button><button class="button" @click="saveProgress">提交</button></template>
    </ModalDialog>

    <!-- 生成结算 -->
    <ModalDialog v-if="settleModal" title="生成结算单" @close="settleModal = false">
      <div class="form-grid">
        <div class="field"><label class="required">合格数量</label><input v-model.number="settleForm.qualified_qty" type="number" min="0"></div>
        <div class="field"><label class="required">单价</label><input v-model.number="settleForm.unit_price" type="number" min="0" step="0.01"></div>
        <div class="field"><label>扣款（超耗/质量）</label><input v-model.number="settleForm.deduction_amount" type="number" min="0" step="0.01"></div>
        <div class="field"><label>备注</label><input v-model.trim="settleForm.remark"></div>
      </div>
      <p class="text-muted" style="font-size:13px">应付金额 = 合格数 × 单价 − 扣款；确认后金额锁定。</p>
      <template #footer><button class="button secondary" @click="settleModal = false">取消</button><button class="button" @click="saveSettle">创建</button></template>
    </ModalDialog>

    <!-- 接受委外 -->
    <ModalDialog v-if="acceptModal" :title="`接受委外 ${acceptForm.order_no}`" @close="acceptModal = false">
      <div class="form-grid">
        <div class="field"><label class="required">对应本厂产品</label><FilterSelect v-model="acceptForm.product_id" :options="productOptions" /></div>
        <div class="field"><label class="required">承接数量</label><input v-model.number="acceptForm.qty" type="number" min="1"></div>
        <div class="field full"><label>承接范围说明（工序受托时填写）</label><input v-model.trim="acceptForm.scope_note"></div>
      </div>
      <template #footer><button class="button secondary" @click="acceptModal = false">取消</button><button class="button" @click="doAccept">接受并生成受托工单</button></template>
    </ModalDialog>

    <!-- 登记受托订单（非租户委托方） -->
    <ModalDialog v-if="incomingModal" title="登记受托订单" @close="incomingModal = false">
      <div class="form-grid">
        <div class="field"><label class="required">委托方（客户）</label><FilterSelect v-model="incomingForm.customer_id" :options="customerOptions" /></div>
        <div class="field"><label class="required">产品</label><FilterSelect v-model="incomingForm.product_id" :options="productOptions" /></div>
        <div class="field"><label class="required">数量</label><input v-model.number="incomingForm.qty" type="number" min="1"></div>
        <div class="field"><label>对方单号</label><input v-model.trim="incomingForm.source_order_ref"></div>
        <div class="field full"><label>承接范围说明（整单/分单/工序）</label><input v-model.trim="incomingForm.scope_note"></div>
      </div>
      <template #footer><button class="button secondary" @click="incomingModal = false">取消</button><button class="button" @click="saveIncoming">登记</button></template>
    </ModalDialog>
  </section>
</template>
