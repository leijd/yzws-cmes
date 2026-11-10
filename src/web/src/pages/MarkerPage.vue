<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import DatePicker from '../components/DatePicker.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('marker:write'))
const canReview = computed(() => has('marker:review'))

const markers = ref<any[]>([])
const orders = ref<any[]>([])
const materials = ref<any[]>([])
const employees = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const uploading = ref(false)
const error = ref('')
const notice = ref('')
const filterOrder = ref('')
const modal = ref('')
const current = ref<any>(null)
const references = ref<any[]>([])

const form = reactive({ marker_no: '', marker_date: '', work_order_id: '', color: '', material_id: '', fabric_width_cm: '150', fabric_layers: 1, marker_length_m: '', utilization_rate: '', drawing_attachment_id: '', layout_employee_id: '', remark: '', ratios: [{ size: '', ratio_qty: 0, sort_no: 1 }] })
const orderOptions = computed(() => orders.value.map(o => ({ value: o.id, label: `${o.work_order_code} · ${o.product_name}` })))
const materialOptions = computed(() => materials.value.map(m => ({ value: m.id, label: `${m.code} · ${m.name}` })))
const employeeOptions = computed(() => employees.value.map(e => ({ value: e.id, label: `${e.code} · ${e.name}` })))
const reviewText: Record<string, string> = { draft: '草稿', pending: '待审核', approved: '已审核', rejected: '已驳回' }
const preview = computed(() => {
  const pieces = form.ratios.reduce((sum, r) => sum + (Number(r.ratio_qty) || 0), 0)
  const layers = Number(form.fabric_layers) || 0
  const cut = pieces * layers
  const fabric = (Number(form.marker_length_m) || 0) * layers
  return { pieces, cut, fabric: fabric.toFixed(2) }
})

async function load() {
  loading.value = true; error.value = ''
  try {
    const query = filterOrder.value ? `?work_order_id=${filterOrder.value}` : ''
    const [m, o, mat, emp] = await Promise.all([
      api<{ items: any[] }>('/markers' + query),
      api<{ items: any[] }>('/work-orders'),
      api<{ items: any[] }>('/materials'),
      api<{ items: any[] }>('/employees'),
    ])
    markers.value = m.items; orders.value = o.items; materials.value = mat.items; employees.value = emp.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function resetForm() {
  Object.assign(form, { marker_no: '', marker_date: new Date().toISOString().slice(0, 10), work_order_id: filterOrder.value || '', color: '', material_id: '', fabric_width_cm: '150', fabric_layers: 1, marker_length_m: '', utilization_rate: '', drawing_attachment_id: '', layout_employee_id: '', remark: '', ratios: [{ size: '', ratio_qty: 0, sort_no: 1 }] })
}
function openCreate() { current.value = null; resetForm(); error.value = ''; modal.value = 'marker' }
async function openEdit(item: any) {
  current.value = item; error.value = ''
  try {
    const data = await api<any>(`/markers/${item.id}`)
    Object.assign(form, {
      marker_no: data.marker_no, marker_date: data.marker_date, work_order_id: data.work_order_id, color: data.color,
      material_id: data.material_id, fabric_width_cm: data.fabric_width_cm, fabric_layers: Number(data.fabric_layers) || 1,
      marker_length_m: data.marker_length_m, utilization_rate: data.utilization_rate || '', drawing_attachment_id: data.drawing_attachment_id || '',
      layout_employee_id: data.layout_employee_id || '', remark: data.remark || '', ratios: (data.ratios || [{ size: '', ratio_qty: 0, sort_no: 1 }]).map((r: any, i: number) => ({ size: r.size, ratio_qty: Number(r.ratio_qty), sort_no: r.sort_no || i + 1 })),
    })
    modal.value = 'marker'
  } catch (e) { error.value = (e as Error).message }
}
function addRatio() { form.ratios.push({ size: '', ratio_qty: 0, sort_no: form.ratios.length + 1 }) }
function removeRatio(index: number) { if (form.ratios.length > 1) form.ratios.splice(index, 1) }
async function save() {
  const ratios = form.ratios.filter(r => r.size.trim() && Number(r.ratio_qty) > 0).map((r, i) => ({ size: r.size.trim(), ratio_qty: Number(r.ratio_qty), sort_no: i + 1 }))
  if (!form.marker_no.trim() || !form.work_order_id || !form.color.trim() || !form.material_id || !ratios.length) { error.value = '唛架号、工单、颜色、面料和配比必填'; return }
  saving.value = true; error.value = ''
  const payload: any = { marker_no: form.marker_no.trim(), marker_date: form.marker_date, work_order_id: form.work_order_id, color: form.color.trim(), material_id: form.material_id, fabric_width_cm: form.fabric_width_cm, fabric_layers: Number(form.fabric_layers), marker_length_m: form.marker_length_m, utilization_rate: form.utilization_rate || '0', drawing_attachment_id: form.drawing_attachment_id || null, layout_employee_id: form.layout_employee_id || null, remark: form.remark || null, ratios }
  try {
    if (current.value) await api(`/markers/${current.value.id}`, { method: 'PATCH', body: JSON.stringify({ ...payload, version: current.value.version }) })
    else await api('/markers', { method: 'POST', body: JSON.stringify(payload) })
    modal.value = ''; notice.value = current.value ? '唛架已保存' : '唛架已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function submitReview(item: any) {
  if (!window.confirm(`提交唛架 ${item.marker_no} 审核？`)) return
  error.value = ''
  try { await api(`/markers/${item.id}/submit-review`, { method: 'POST', body: JSON.stringify({ version: item.version }) }); notice.value = '已提交审核'; await load() } catch (e) { error.value = (e as Error).message }
}
async function review(item: any, decision: 'approve' | 'reject') {
  const reason = decision === 'reject' ? window.prompt('请输入驳回原因') : ''
  if (decision === 'reject' && !reason) return
  error.value = ''
  try { await api(`/markers/${item.id}/review`, { method: 'POST', body: JSON.stringify({ version: item.version, decision, reason }) }); notice.value = decision === 'approve' ? '唛架审核通过' : '唛架已驳回'; await load() } catch (e) { error.value = (e as Error).message }
}
async function showReferences(item: any) {
  error.value = ''
  try { references.value = (await api<{ items: any[] }>(`/markers/${item.id}/references`)).items; modal.value = 'references' } catch (e) { error.value = (e as Error).message }
}
async function remove(item: any) {
  if (!window.confirm(`删除唛架 ${item.marker_no}？`)) return
  error.value = ''
  try { await api(`/markers/${item.id}`, { method: 'DELETE' }); notice.value = '唛架已删除'; await load() } catch (e) { error.value = (e as Error).message }
}
async function uploadDrawing(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true; error.value = ''
  try {
    const bytes = new Uint8Array(await file.arrayBuffer())
    const digest = await crypto.subtle.digest('SHA-256', bytes)
    const sha256 = Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('')
    const init = await api<{ attachment_id: string; chunk_size: number }>('/attachments/init', { method: 'POST', body: JSON.stringify({ sha256, size_bytes: file.size, mime_type: file.type || 'application/pdf', owner_type: 'marker' }) })
    const chunkSize = init.chunk_size || 4 * 1024 * 1024
    let index = 0
    for (let offset = 0; offset < file.size; offset += chunkSize) {
      const chunk = file.slice(offset, offset + chunkSize)
      const response = await fetch(`/api/v1/attachments/${init.attachment_id}/chunks/${index++}`, { method: 'PUT', headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}`, 'Content-Type': 'application/octet-stream' }, body: chunk })
      if (!response.ok) throw new Error('分片上传失败')
    }
    await api(`/attachments/${init.attachment_id}/complete`, { method: 'POST', body: JSON.stringify({ total_chunks: index }) })
    form.drawing_attachment_id = init.attachment_id
    notice.value = '唛架图上传完成'
  } catch (e) { error.value = (e as Error).message } finally { uploading.value = false; if (input) input.value = '' }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>唛架管理</h1><p>唛架排料、配比、审核与裁床引用</p></div><div class="header-actions"><FilterSelect v-model="filterOrder" :options="[{value:'',label:'全部工单'}].concat(orderOptions)" @change="load"/><button v-if="canWrite" class="button" @click="openCreate">+ 唛架</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><PageState :loading="loading"/>
    <div v-if="!loading && markers.length" class="table-wrap"><table><thead><tr><th>唛架号</th><th>工单</th><th>产品/颜色</th><th>面料</th><th>件/唛</th><th>裁片数</th><th>用布(m)</th><th>审核</th><th>版本</th><th>操作</th></tr></thead><tbody><tr v-for="item in markers" :key="item.id"><td><strong>{{ item.marker_no }}</strong><small class="text-muted" style="display:block">{{ item.marker_date }}</small></td><td class="text-muted">{{ item.work_order_id }}</td><td>{{ item.product_code }} · {{ item.color }}</td><td>{{ item.material_code }}</td><td>{{ item.pieces_per_marker }}</td><td>{{ item.cut_qty }}</td><td>{{ item.estimated_fabric_m }}</td><td><span class="status" :class="item.review_status">{{ reviewText[item.review_status] || item.review_status }}</span></td><td>v{{ item.version }}</td><td><button class="button link" @click="openEdit(item)">查看/编辑</button><button v-if="canWrite && item.review_status === 'draft'" class="button link" @click="submitReview(item)">提交审核</button><button v-if="canReview && item.review_status === 'pending'" class="button link" @click="review(item, 'approve')">通过</button><button v-if="canReview && item.review_status === 'pending'" class="button link text-danger" @click="review(item, 'reject')">驳回</button><button class="button link" @click="showReferences(item)">引用</button><button v-if="canWrite && item.review_status === 'draft'" class="button link text-danger" @click="remove(item)">删除</button></td></tr></tbody></table></div>
    <PageState v-if="!loading" :empty="!markers.length"/>
  </section>

  <ModalDialog v-if="modal === 'marker'" wide :title="current ? `唛架 · ${form.marker_no}` : '新增唛架'" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label class="required">唛架号</label><input v-model.trim="form.marker_no" placeholder="MK-20261103-01"></div>
      <div class="field"><label>唛架日期</label><DatePicker v-model="form.marker_date"/></div>
      <div class="field full"><label class="required">生产工单</label><FilterSelect v-model="form.work_order_id" :options="orderOptions" placeholder="选择工单"/></div>
      <div class="field"><label class="required">颜色</label><input v-model.trim="form.color"></div>
      <div class="field"><label class="required">面料</label><FilterSelect v-model="form.material_id" :options="materialOptions" placeholder="选择面料"/></div>
      <div class="field"><label>门幅(cm)</label><input v-model="form.fabric_width_cm" inputmode="decimal"></div>
      <div class="field"><label>层数</label><input v-model.number="form.fabric_layers" type="number" min="1"></div>
      <div class="field"><label>唛架长(m)</label><input v-model="form.marker_length_m" inputmode="decimal"></div>
      <div class="field"><label>利用率</label><input v-model="form.utilization_rate" inputmode="decimal" placeholder="0.85"></div>
      <div class="field"><label>排料人</label><FilterSelect v-model="form.layout_employee_id" :options="employeeOptions" placeholder="选择员工"/></div>
      <div class="field full"><label>唛架图</label><input type="file" accept="image/*,application/pdf" :disabled="uploading" @change="uploadDrawing"><small class="text-muted">{{ form.drawing_attachment_id ? '已上传，提交审核前必须有唛架图' : '支持图片/PDF，提交审核前必须上传' }}</small></div>
      <div class="field full"><label>配比（尺码 × 件数）</label><div v-for="(ratio, index) in form.ratios" :key="index" style="display:flex;gap:8px;align-items:center;margin-bottom:8px"><input v-model.trim="ratio.size" placeholder="尺码，如 M" style="width:140px"><input v-model.number="ratio.ratio_qty" type="number" min="1" placeholder="件数" style="width:120px"><button type="button" class="button link text-danger" @click="removeRatio(index)">删除</button></div><button type="button" class="button secondary small" @click="addRatio">+ 增加配比</button></div>
      <div class="field full"><div class="notice" style="margin:0">服务端计算：{{ preview.pieces }} 件/唛，裁片 {{ preview.cut }} 件，预计用布 {{ preview.fabric }} m</div></div>
      <div class="field full"><label>备注</label><textarea v-model="form.remark" rows="2"></textarea></div>
    </div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button v-if="canWrite" class="button" :disabled="saving || uploading || !form.marker_no || !form.work_order_id || !form.material_id || !form.ratios.some(r => r.size && r.ratio_qty > 0)" @click="save">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'references'" :title="`唛架引用 · ${current?.marker_no || ''}`" @close="modal = ''">
    <div class="table-wrap"><table><thead><tr><th>裁床单</th><th>床次</th><th>裁片数</th><th>状态</th></tr></thead><tbody><tr v-for="item in references" :key="item.id"><td>{{ item.cutting_no }}</td><td>{{ item.bed_no || '—' }}</td><td>{{ item.cut_qty }}</td><td>{{ item.status }}</td></tr></tbody></table><PageState :empty="!references.length"/></div>
    <template #footer><button class="button secondary" @click="modal = ''">关闭</button></template>
  </ModalDialog>
</template>
