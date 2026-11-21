<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { toDataURL } from 'qrcode'
import { api } from '../api'
import { toastError, toastSuccess } from '../toast'
import { usePermissions } from '../permissions'
import { formatMicros, todayISO } from '../date'
import DatePicker from '../components/DatePicker.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import SceneBadge from '../components/SceneBadge.vue'
import ThreeStateTabs from '../components/ThreeStateTabs.vue'
import EvidenceCapture from '../components/EvidenceCapture.vue'

/* 裁床单两级录入（设计 4.19.1 / 5.17 / 6.11）。
 * 上级表格：床次行，支持增删插、排序、整行复制粘贴；唛架允许空。
 * 下级表格：扎号明细，支持自动生成、增删、复制行插入 n 行、排序；部位为分号分隔文本，可手输或勾选。
 * 每分钟自动保存草稿；正式保存回写上级分配扎号段与码数明细；每个部位单独一张菲票。 */

const { has } = usePermissions()
const canWrite = computed(() => has('cutting:write'))
const state = ref('running')
const sheets = ref<any[]>([])
const detail = ref<any>(null)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const activeBed = ref<any>(null)
const detailRows = ref<any[]>([])
const selectedDetails = ref<string[]>([])
const unsavedBedDetail = ref(false)
const sceneFilter = ref('')
const copyRows = ref('1')
const printCopies = ref(1)
const printOrder = ref<'ticket' | 'copy'>('ticket')
const partOptions = ref<string[]>(['前幅', '后幅', '袖子', '袖英', '门筒', '下摆', '领子', '口袋'])
const pickerRow = ref<any | null>(null)
const pickerChecked = ref<string[]>([])

const form = reactive({ cutting_no: '', scene_type: 'A', scene_sub: 'A1', project_id: '', customer_id: '', cutting_date: '', total_qty: null as number | null, remark: '' })
const customers = ref<any[]>([])
const workOrders = ref<any[]>([])
const scenes = ref<any[]>([])
const projects = ref<any[]>([])

const sceneSubs = computed(() => scenes.value.find(s => s.scene_type === form.scene_type)?.subs || [])
const customerOptions = computed(() => customers.value.map(c => ({ value: c.id, label: c.name })))
const projectOptions = computed(() => projects.value.map(p => ({ value: p.id, label: `${p.project_code} · ${p.style_no || p.product_name || ''}${p.customer_name ? ' · ' + p.customer_name : ''}` })))
const workOrderOptions = computed(() => workOrders.value.map(w => ({ value: w.id, label: w.work_order_code })))
const stateCounts = computed(() => ({
  new: sheets.value.filter(s => s.status === 'draft').length,
  running: sheets.value.filter(s => s.status === 'saved' || s.status === 'published').length,
  history: sheets.value.filter(s => s.status === 'published').length,
}))
const filteredSheets = computed(() => sheets.value.filter(s => {
  if (sceneFilter.value && s.scene_type !== sceneFilter.value) return false
  if (state.value === 'new') return s.status === 'draft'
  if (state.value === 'history') return s.status === 'published'
  return s.status !== 'draft'
}))
const selectedBeds = computed<any[]>(() => detail.value?.beds || [])
const sheetStatusText: Record<string, string> = { draft: '草稿', saved: '已保存', published: '已发布', cancelled: '已作废' }
const idx = (v: string | number) => Number(v)

/* 部位统一以分号分隔文本编辑；兼容旧数据中的数组。允许手工输入模板外名称。 */
function splitParts(v: any): string[] {
  const list = Array.isArray(v) ? v : String(v ?? '').split(/[;；]/)
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of list) {
    const name = String(item ?? '').trim()
    if (name && !seen.has(name)) { seen.add(name); out.push(name) }
  }
  return out
}
function partsText(r: any): string {
  if (r.parts_text !== undefined && r.parts_text !== null) return String(r.parts_text)
  return splitParts(r.parts).join(';')
}
function normalizePartsText(value: any): string {
  return String(value ?? '').replace(/[；﹔]/g, ';')
}
function onPartsInput(r: any, e: Event) {
  const input = e.target as HTMLTextAreaElement
  const normalized = normalizePartsText(input.value)
  r.parts_text = normalized
  if (input.value !== normalized) input.value = normalized
}
function ticketCount(r: any): number { return Math.max(1, splitParts(partsText(r)).length) }
const selectedTicketCount = computed(() => detailRows.value
  .filter(r => r.id && selectedDetails.value.includes(rowKey(r)))
  .reduce((n, r) => n + ticketCount(r), 0))

function blankBed() {
  return { id: '', work_order_id: '', project_id: '', style_no: '', style_name: '', customer_id: '', color: '',
    bed_no: '', marker_no: '', fabric_layers: 0, cut_qty: 0, bundle_range: '', size_ratio: [],
    cutter_id: '', reviewer_id: '', remark: '', sort_no: detail.value?.beds?.length + 1 || 1, status: 'draft' }
}
/* 后端开启严格 JSON 解析，只提交 bedInput 声明的字段，剔除 status/detail_count 等前端本地状态 */
function bedPayload(b: any) {
  return {
    id: b.id || '', work_order_id: b.work_order_id || '', project_id: b.project_id || '',
    style_no: b.style_no || '', style_name: b.style_name || '', customer_id: b.customer_id || '',
    color: b.color || '', bed_no: b.bed_no || '', marker_no: b.marker_no || '',
    fabric_layers: b.fabric_layers || 0, cut_qty: b.cut_qty || 0,
    bundle_range: b.bundle_range || '', size_ratio: b.size_ratio || [],
    cutter_id: b.cutter_id || '', reviewer_id: b.reviewer_id || '', remark: b.remark || '',
    sort_no: b.sort_no || 1,
  }
}

async function load() {
  loading.value = true; error.value = ''
  try {
    const [s, c, w, sc, pr] = await Promise.all([
      api<{ items: any[] }>('/cutting-sheets'),
      api<{ items: any[] }>('/customers', {}, { silent: true }),
      api<{ items: any[]; next_cursor?: string }>('/work-orders', {}, { silent: true }),
      api<{ items: any[] }>('/scenes', {}, { silent: true }),
      api<{ items: any[] }>('/projects', {}, { silent: true }),
    ])
    sheets.value = s.items; customers.value = c.items; workOrders.value = w.items || []; scenes.value = sc.items; projects.value = pr.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

function openCreate() {
  Object.assign(form, { cutting_no: '', scene_type: 'A', scene_sub: 'A1', project_id: '', customer_id: '',
    cutting_date: todayISO(), total_qty: null, remark: '' })
  error.value = ''; modal.value = 'create'
}
function onSheetProjectChange() {
  const project = projects.value.find(item => item.id === form.project_id)
  if (!project) return
  const matched = customers.value.find(c => c.name === project.customer_name)
  if (matched) form.customer_id = matched.id
}
/* 床次行订单号填入后自动带出款号/款式/客户（可再手动修正） */
function onBedProjectChange(b: any) {
  const project = projects.value.find(item => item.id === b.project_id)
  if (!project) return
  b.style_no = project.style_no || b.style_no
  b.style_name = project.style_name || b.style_name
  const matched = customers.value.find(c => c.name === project.customer_name)
  if (matched) b.customer_id = matched.id
}

async function createSheet() {
  if (!form.project_id) { error.value = '请选择订单号'; return }
  saving.value = true; error.value = ''
  try {
    const res = await api<{ id: string }>('/cutting-sheets', { method: 'POST', body: JSON.stringify({
      cutting_no: form.cutting_no, scene_type: form.scene_type, scene_sub: form.scene_sub,
      project_id: form.project_id, customer_id: form.customer_id, cutting_date: new Date(form.cutting_date || Date.now()).getTime(),
      total_qty: form.total_qty === null || (form.total_qty as any) === '' ? null : Number(form.total_qty),
      remark: form.remark, beds: [] }) })
    modal.value = ''; notice.value = '裁床单已创建'
    await load(); await openSheet(res.id)
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

async function openSheet(id: string) {
  loading.value = true; error.value = ''
  try {
    detail.value = await api<any>(`/cutting-sheets/${id}`)
    if (!detail.value.beds?.length) {
      const project = projects.value.find(item => item.id === detail.value.project_id)
      const bed = blankBed()
      bed.project_id = project?.project_code || ''
      bed.style_no = project?.style_no || ''
      bed.style_name = project?.style_name || ''
      bed.customer_id = project?.customer_id || ''
      detail.value.beds.push(bed)
    }
    modal.value = 'sheet'
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

/* ---- 上级表格操作 ---- */
function addBed(index?: number) {
  const row = blankBed()
  if (index === undefined) detail.value.beds.push(row)
  else detail.value.beds.splice(index + 1, 0, row)
  reSortBeds()
}
function reSortBeds() { detail.value.beds.forEach((b: any, i: number) => { b.sort_no = i + 1 }) }
function removeBed(index: number) {
  if (detail.value.beds[index].detail_count > 0) { error.value = '该床次已产生扎号明细，不能删除'; return }
  detail.value.beds.splice(index, 1); reSortBeds()
}
function moveBed(index: number, delta: number) {
  const target = index + delta
  if (target < 0 || target >= detail.value.beds.length) return
  const arr = detail.value.beds
  ;[arr[index], arr[target]] = [arr[target], arr[index]]
  reSortBeds()
}
/* 复制床次行：只带基础信息，唛架编号/拉布层数/裁片数/扎号段/码数明细及下级明细均不复制 */
function copiedBed(src: any) {
  return {
    id: '', work_order_id: '', project_id: src.project_id || '', style_no: src.style_no || '',
    style_name: src.style_name || '', customer_id: src.customer_id || '', color: src.color || '',
    bed_no: src.bed_no || '', marker_no: '', fabric_layers: 0, cut_qty: 0,
    bundle_range: '', size_ratio: [], cutter_id: src.cutter_id || '', reviewer_id: src.reviewer_id || '',
    remark: src.remark || '', sort_no: 0, status: 'draft', detail_count: 0,
  }
}
function copyBed(index: number) {
  const src = detail.value.beds[index]
  detail.value.beds.splice(index + 1, 0, copiedBed(src))
  reSortBeds()
}
function pasteBeds() {
  if (!activeBed.value) { error.value = '请先点击某一行再粘贴'; return }
  detail.value.beds.push(copiedBed(activeBed.value))
  reSortBeds()
}

async function saveBeds() {
  saving.value = true; error.value = ''
  try {
    const res = await api<{ version: number }>(`/cutting-sheets/${detail.value.id}`, {
      method: 'PATCH', body: JSON.stringify({
        version: detail.value.version, scene_type: detail.value.scene_type, scene_sub: detail.value.scene_sub,
        customer_id: '', remark: detail.value.remark, beds: detail.value.beds.map((b: any) => bedPayload(b)),
      }),
    })
    detail.value.version = res.version
    notice.value = '上级表格已保存'
    await load(); await openSheet(detail.value.id)
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

/* ---- 下级表格操作 ---- */
async function openDetails(bed: any) {
  activeBed.value = bed; selectedDetails.value = []; error.value = ''
  // 未保存的新行（如复制粘贴）：打开空明细窗口，先在本地录入，保存上级表格后再正式保存明细
  if (!bed.id) {
    unsavedBedDetail.value = true
    detailRows.value = (Array.isArray(bed._localRows) ? JSON.parse(JSON.stringify(bed._localRows)) : []).map(hydrateRow)
    notice.value = '该床次尚未保存，已打开空明细窗口供录入；请先「返回上级表格 → 保存上级表格」，再回来正式保存明细'
    modal.value = 'details'
    return
  }
  unsavedBedDetail.value = false
  loading.value = true
  try {
    const res = await api<{ rows: any[]; draft: boolean; saved_at?: number }>(`/cutting-sheets/${detail.value.id}/details?bed_id=${bed.id}`)
    detailRows.value = res.rows.map(r => hydrateRow({ ...r, parts: r.parts || [] }))
    modal.value = 'details'
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
/* 本地唯一标识：未保存行(无后端 id)也能勾选、批量打印 */
let uidSeq = 0
function assignUid(r: any) { if (!r._uid) r._uid = `u-${Date.now()}-${++uidSeq}`; return r }
function hydrateRow(r: any) { assignUid(r); r.parts_text = partsText(r); return r }
function rowKey(r: any) { return r.id || r._uid }
function blankDetail() {
  return hydrateRow({ id: '', size: 'S', bundle_no: '', piece_qty: 1, parts_text: '',
    work_order_no: activeBed.value?.work_order_id || '', style_no: activeBed.value?.style_no || '',
    color: activeBed.value?.color || '', bed_no: activeBed.value?.bed_no || '', sort_no: detailRows.value.length + 1 })
}
function addDetail() { detailRows.value.push(blankDetail()) }
function removeDetail(index: number) {
  if (detailRows.value[index].locked) { error.value = '该扎号已锁定，不能删除'; return }
  detailRows.value.splice(index, 1)
}
function duplicateDetail(index: number) {
  const n = Math.max(1, Math.min(50, Number(copyRows.value) || 1))
  const src = detailRows.value[index]
  for (let i = 0; i < n; i++) {
    detailRows.value.splice(index + 1 + i, 0, hydrateRow({ ...JSON.parse(JSON.stringify(src)), id: '', _uid: '', locked: false,
      bundle_no: '', parts_text: partsText(src) }))
  }
  detailRows.value.forEach((r, i) => { r.sort_no = i + 1 })
}
function sortDetails() {
  const sizeOrder = ['XS', 'S', 'M', 'L', 'XL', 'XXL', 'XXXL']
  detailRows.value.sort((a, b) => {
    const c = String(a.color).localeCompare(String(b.color)) || String(a.bed_no).localeCompare(String(b.bed_no))
    if (c) return c
    const si = sizeOrder.indexOf(a.size), sj = sizeOrder.indexOf(b.size)
    if (si !== sj) return (si < 0 ? 99 : si) - (sj < 0 ? 99 : sj)
    return (Number(a.bundle_no) || 0) - (Number(b.bundle_no) || 0)
  })
  detailRows.value.forEach((r, i) => { r.sort_no = i + 1 })
}
function detailPayload() {
  return detailRows.value.map((r, i) => ({
    id: r.id || '', size: r.size, bundle_no: String(r.bundle_no), piece_qty: Number(r.piece_qty) || 0,
    parts: splitParts(partsText(r)), work_order_no: r.work_order_no || '', style_no: r.style_no || '',
    color: r.color || activeBed.value.color, bed_no: r.bed_no || activeBed.value.bed_no, sort_no: i + 1,
  }))
}
async function saveDetails() {
  if (unsavedBedDetail.value || !activeBed.value?.id) { error.value = '该床次尚未保存，请先返回上级表格点击「保存上级表格」，再回来正式保存明细'; return }
  saving.value = true; error.value = ''
  try {
    const res = await api<any>(`/cutting-sheets/${detail.value.id}/details`, {
      method: 'PUT', body: JSON.stringify({ bed_id: activeBed.value.id, rows: detailPayload() }) })
    notice.value = `已正式保存 ${res.count} 条扎号，分配扎号段 ${res.bundle_range}`
    modal.value = 'sheet'
    await load(); await openSheet(detail.value.id)
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function publish() {
  if (unsavedBedDetail.value || !activeBed.value?.id) { error.value = '该床次尚未保存，请先保存上级表格及明细后再发布'; return }
  saving.value = true; error.value = ''
  try {
    const res = await api<{ bundles: number; tickets?: number }>(`/cutting-sheets/${detail.value.id}/publish`, {
      method: 'POST', body: JSON.stringify({ bed_id: activeBed.value.id }) })
    notice.value = `已发布到工单，生成 ${res.bundles} 个扎号、${res.tickets ?? res.bundles} 张菲票`
    await load(); await openSheet(detail.value.id)
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

async function printTickets(ids: string[], bedId = '') {
  if (!ids.length && !bedId) { toastError('请先选择要打印的扎号'); return }
  const copies = Math.max(1, Math.min(50, Number(printCopies.value) || 1))
  const collate = printOrder.value === 'copy'
  // 打印窗口必须在点击手势的同步调用栈内打开；放到 await 之后会被浏览器当作弹窗拦截
  const win = window.open('', '_blank', 'width=880,height=720')
  if (!win) { toastError('浏览器拦截了打印窗口，请允许本站点弹出窗口后重试'); return }
  try {
    const res = await api<{ items: any[]; count: number; mode: string }>('/tickets/print', {
      method: 'POST', body: JSON.stringify({ ids, bed_id: bedId, copies, collate }) })
    if (!res.count) { win.close(); toastError('没有可打印的菲票'); return }
    if (res.mode === 'print_job') {
      win.close()
      toastSuccess(`已提交打印任务，共 ${res.count} 张菲票 × ${copies} 份（${collate ? '逐份打印' : '逐张打印'}）`)
      return
    }
    const esc = (v: any) => String(v ?? '').replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] as string))
    // 逐张：A,A,...B,B,...；逐份：A,B,...,A,B,...
    const printItems: any[] = collate
      ? Array.from({ length: copies }).flatMap(() => res.items)
      : res.items.flatMap(t => Array.from({ length: copies }, () => t))
    // 为每张菲票生成真实二维码（扫码内容沿用后端 qrcode 字段）
    const qrs = await Promise.all(printItems.map((t: any) =>
      toDataURL(String(t.qrcode || ''), { width: 220, margin: 1, errorCorrectionLevel: 'M' }).catch(() => '')))
    const tickets = printItems.map((t: any, i: number) => `<article class="ticket">
      <div class="tk-title">菲 票</div>
      <div class="tk-bundle">扎号 <strong>${esc(t.bundle_no)}</strong></div>
      <div class="tk-qr"><img src="${qrs[i]}" alt="QR"></div>
      <div class="tk-code">${esc(t.qrcode)}</div>
      <div class="tk-rows">
        <div><span>裁床单</span><b>${esc(t.cutting_no)}</b></div>
        <div><span>款号</span><b>${esc(t.style_no)}</b></div>
        <div><span>颜色</span><b>${esc(t.color)}</b></div>
        <div><span>床号</span><b>${esc(t.bed_no)}</b></div>
        <div><span>码数</span><b>${esc(t.size)}</b></div>
        <div class="tk-qty"><span>件数</span><b>${esc(t.piece_qty)}</b></div>
        <div><span>部位</span><b>${esc(t.part_name || (t.parts || []).join(' / ') || '—')}</b></div>
      </div>
      <div class="tk-foot">扫码报工</div>
    </article>`).join('')
    win.document.write(`<!doctype html><html><head><meta charset="utf-8"><title>菲票打印</title>
      <style id="page-style">@page{size:58mm 90mm;margin:0}</style>
      <style>
      *{box-sizing:border-box;-webkit-print-color-adjust:exact;print-color-adjust:exact}
      html,body{margin:0;padding:0;background:#fff}
      body{font-family:"Microsoft YaHei","PingFang SC",ui-monospace,monospace;color:#111;font-size:10pt;line-height:1.35}
      .ticket{width:58mm;margin:0 auto;padding:3mm 2mm;text-align:center;break-inside:avoid;page-break-inside:avoid;page-break-after:always}
      .ticket:last-child{page-break-after:auto}
      .tk-title{font-size:15pt;font-weight:800;letter-spacing:4px;border-bottom:1px dashed #111;padding-bottom:1mm}
      .tk-bundle{font-size:11pt;margin:1.5mm 0}
      .tk-bundle strong{font-size:22pt;line-height:1}
      .tk-qr img{width:30mm;height:30mm;image-rendering:pixelated}
      .tk-code{font-family:ui-monospace,"Courier New",monospace;font-size:7pt;word-break:break-all;margin:0.5mm 0 1.5mm}
      .tk-rows{text-align:left;border-top:1px dashed #111;border-bottom:1px dashed #111;padding:1mm 0}
      .tk-rows>div{display:flex;justify-content:space-between;gap:2mm;padding:0.4mm 0}
      .tk-rows span{white-space:nowrap;color:#333}
      .tk-rows b{text-align:right;word-break:break-all}
      .tk-rows .tk-qty{border-top:1px dashed #999;margin-top:0.5mm;padding-top:1mm}
      .tk-rows .tk-qty b{font-size:14pt;color:#c0392b}
      .tk-foot{font-size:9pt;font-weight:700;margin-top:1.5mm}
      </style></head><body>
      ${tickets}
      <script>window.onload=()=>{
        var el=document.querySelector('.ticket');
        if(el){var h=Math.ceil(el.scrollHeight*25.4/96)+2;
          document.getElementById('page-style').textContent='@page{size:58mm '+h+'mm;margin:0}';}
        window.print();
      }<\/script></body></html>`)
    win.document.close()
    toastSuccess(`已生成 ${printItems.length} 张菲票（${copies} 份，${collate ? '逐份打印' : '逐张打印'}）`)
  } catch (e) {
    win.close()
    toastError((e as Error).message || '打印失败')
  }
}
function toggleSelect(key: string) {
  const i = selectedDetails.value.indexOf(key)
  if (i >= 0) selectedDetails.value.splice(i, 1); else selectedDetails.value.push(key)
}
function allSelected() { return detailRows.value.length > 0 && detailRows.value.every(r => selectedDetails.value.includes(rowKey(r))) }
function toggleSelectAll() {
  if (allSelected()) selectedDetails.value = []
  else selectedDetails.value = detailRows.value.map(rowKey)
}
/* 批量打印：仅打印已保存(有后端 id)的选中行 */
function printSelected() {
  const ids = detailRows.value.filter(r => selectedDetails.value.includes(rowKey(r)) && r.id).map(r => r.id)
  const unsaved = detailRows.value.filter(r => selectedDetails.value.includes(rowKey(r)) && !r.id).length
  if (unsaved) toastError(`已跳过 ${unsaved} 条未保存明细（请先保存上级表格及明细）`)
  if (!ids.length) { if (!unsaved) toastError('请先勾选要打印的扎号'); return }
  printTickets(ids)
}
/* 单行打印：未保存行提示先保存 */
function printOne(r: any) {
  if (!r.id) { toastError('该扎号尚未保存，请先保存明细后再打印'); return }
  printTickets([r.id])
}
/* 部位勾选弹窗：打开时解析输入框自动勾选；确认时按勾选结果同步，保留模板外的自定义部位 */
function openPartPicker(r: any) {
  pickerRow.value = r
  const current = splitParts(partsText(r))
  pickerChecked.value = partOptions.value.filter(p => current.includes(p))
  modal.value = 'parts'
}
function togglePickerPart(name: string) {
  const i = pickerChecked.value.indexOf(name)
  if (i >= 0) pickerChecked.value.splice(i, 1); else pickerChecked.value.push(name)
}
function confirmPartPicker() {
  if (!pickerRow.value) { modal.value = 'details'; return }
  const current = splitParts(partsText(pickerRow.value))
  const custom = current.filter(p => !partOptions.value.includes(p))
  pickerRow.value.parts_text = [...custom, ...partOptions.value.filter(p => pickerChecked.value.includes(p))].join(';')
  modal.value = 'details'
}
function cancelPartPicker() { pickerRow.value = null; pickerChecked.value = []; modal.value = 'details' }

function closeDetails() {
  // 未保存床次的本地明细暂存在行对象上，返回后可再次进入继续编辑
  if (unsavedBedDetail.value && activeBed.value) activeBed.value._localRows = JSON.parse(JSON.stringify(detailRows.value))
  modal.value = 'sheet'; activeBed.value = null; unsavedBedDetail.value = false
}

watch(() => form.scene_type, () => { form.scene_sub = sceneSubs.value[0]?.scene_sub || 'A1' })
onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div><h2>裁床单</h2><p class="sub">两级录入：上级床次行 + 下级扎号明细；分级数据必填，唛架非必填</p></div>
      <div class="head-actions">
        <ThreeStateTabs v-model="state" :counts="stateCounts" />
        <select v-model="sceneFilter" class="filter"><option value="">全部场景</option>
          <option v-for="s in scenes" :key="s.scene_type" :value="s.scene_type">{{ s.label }}</option></select>
        <button v-if="canWrite" type="button" class="btn-primary" @click="openCreate">＋ 新建裁床单</button>
      </div>
    </header>

    <p v-if="error" class="alert-error">{{ error }}</p>
    <p v-if="notice" class="alert-ok">{{ notice }}</p>
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap">
      <table>
        <thead><tr><th>裁床单号</th><th>订单号</th><th>场景</th><th>开裁日期</th><th>客户</th><th>床次数</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="s in filteredSheets" :key="s.id">
            <td><b>{{ s.cutting_no }}</b></td>
            <td>{{ s.project_code || '—' }}</td>
            <td><SceneBadge :scene-type="s.scene_type" :scene-sub="s.scene_sub" :outsource="s.outsource" compact /></td>
            <td>{{ formatMicros(s.cutting_date) }}</td>
            <td>{{ s.customer_name || '—' }}</td>
            <td>{{ s.bed_count }}</td>
            <td><span class="status" :class="s.status">{{ sheetStatusText[s.status] || s.status }}</span></td>
            <td><button type="button" class="btn-link" @click="openSheet(s.id)">{{ state === 'history' ? '查看' : '继续处理' }}</button></td>
          </tr>
          <tr v-if="!filteredSheets.length"><td colspan="8" class="empty">当前状态下暂无裁床单</td></tr>
        </tbody>
      </table>
    </div>

    <!-- 新建：弹窗 ≤90% 全屏，可逐级返回 -->
    <ModalDialog v-if="modal === 'create'" title="新建裁床单" wide width="min(900px, 96vw)" @close="modal = ''">
      <div class="form-grid create-form">
        <label>裁床单号<input v-model="form.cutting_no" placeholder="留空自动生成"></label>
        <label><span class="label-line">订单号<span class="req">*</span></span><select v-model="form.project_id" @change="onSheetProjectChange"><option value="">请选择项目</option><option v-for="p in projectOptions" :key="p.value" :value="p.value">{{ p.label }}</option></select></label>
        <label>业务场景<select v-model="form.scene_type"><option v-for="s in scenes" :key="s.scene_type" :value="s.scene_type">{{ s.label }}</option></select></label>
        <label>场景子类型<select v-model="form.scene_sub"><option v-for="s in sceneSubs" :key="s.scene_sub" :value="s.scene_sub">{{ s.label }}</option></select></label>
        <label>客户<select v-model="form.customer_id"><option value="">请选择</option><option v-for="c in customerOptions" :key="c.value" :value="c.value">{{ c.label }}</option></select></label>
        <label>开裁日期<DatePicker v-model="form.cutting_date"/></label>
        <label>总件数<input v-model.number="form.total_qty" type="number" min="0" placeholder="可留空，留空则取明细件数合计"></label>
        <label class="full">备注<input v-model="form.remark" placeholder="如：客供面料，注意色差"></label>
      </div>
      <template #footer>
        <button type="button" class="btn-outline" @click="modal = ''">取消</button>
        <button type="button" class="btn-primary" :disabled="saving" @click="createSheet">创建并添加更多内容</button>
      </template>
    </ModalDialog>

    <!-- 上级表格：床次行 -->
    <ModalDialog v-if="modal === 'sheet' && detail" :title="`裁床单 ${detail.cutting_no}`" wide width="90%" @close="modal = ''">
      <div class="sheet-banner">
        <span class="banner-text">订单号：{{ projects.find(p => p.id === detail.project_id)?.project_code || '—' }}</span>
        <span class="banner-text">总件数：<strong>{{ detail.total_qty ?? '—' }}</strong><small v-if="detail.total_qty_set">（手填）</small><small v-else>（明细件数合计）</small></span>
      </div>
      <div class="toolbar">
        <button v-if="canWrite" type="button" class="btn-outline" @click="addBed()">＋ 添加行</button>
        <button v-if="canWrite" type="button" class="btn-outline" @click="pasteBeds">粘贴行</button>
        <button v-if="canWrite" type="button" class="btn-primary" :disabled="saving" @click="saveBeds">保存</button>
        <span class="hint">份数 <input v-model.number="printCopies" class="n-input" type="number" min="1" max="50"></span>
        <select v-model="printOrder" title="逐张打印：同一张打印 n 份再打印下一张；逐份打印：一轮全部打完再重复">
          <option value="ticket">逐张打印</option><option value="copy">逐份打印</option>
        </select>
      </div>
      <div class="table-wrap scroll-x">
        <table class="bed-table">
          <thead><tr>
            <th>行</th><th>款号*</th><th>款式</th><th>颜色*</th>
            <th>床次*</th><th>唛架编号</th><th>拉布层数</th><th>本床裁片数</th>
            <th>分配扎号段</th><th>码数明细</th><th>裁剪人</th><th>复核人</th><th>备注</th><th>操作</th>
          </tr></thead>
          <tbody>
            <tr v-for="(b, i) in selectedBeds" :key="b.id || `new-${i}`" :class="{ selected: activeBed?.id === b.id }" @click="activeBed = b">
              <td>{{ idx(i) + 1 }}</td>
              <td class="readonly-cell">{{ b.style_no || '—' }}</td>
              <td class="readonly-cell">{{ b.style_name || '—' }}</td>
              <td><input v-model="b.color" required></td>
              <td><input v-model="b.bed_no" required></td>
              <td><input v-model="b.marker_no"></td>
              <td><input v-model.number="b.fabric_layers" type="number" min="0"></td>
              <td><input v-model.number="b.cut_qty" type="number" min="0"></td>
              <td><input v-model="b.bundle_range"></td>
              <td class="ratio-cell">{{ (b.size_ratio || []).map((x: any) => `${x.size}:${x.qty}`).join(' / ') || '—' }}</td>
              <td><input v-model="b.cutter_id"></td>
              <td><input v-model="b.reviewer_id"></td>
              <td><input v-model="b.remark"></td>
              <td class="ops">
                <button type="button" class="btn-mini" @click.stop="openDetails(b)">明细</button>
                <button v-if="canWrite" type="button" class="btn-mini" @click.stop="addBed(idx(i))">插入</button>
                <button v-if="canWrite" type="button" class="btn-mini" @click.stop="copyBed(idx(i))">复制</button>
                <button v-if="canWrite" type="button" class="btn-mini" @click.stop="moveBed(idx(i), -1)">↑</button>
                <button v-if="canWrite" type="button" class="btn-mini" @click.stop="moveBed(idx(i), 1)">↓</button>
                <button v-if="canWrite" type="button" class="btn-mini danger" @click.stop="removeBed(idx(i))">删除</button>
                <button type="button" class="btn-mini" @click.stop="printTickets([], b.id)">整床打印</button>
              </td>
            </tr>
            <tr v-if="!selectedBeds.length"><td colspan="14" class="empty">暂无床次行，点击「添加行」开始</td></tr>
          </tbody>
        </table>
      </div>
      <template #footer><button type="button" class="btn-outline" @click="modal = ''">返回</button></template>
    </ModalDialog>

    <!-- 下级表格：扎号明细 -->
    <ModalDialog v-if="modal === 'details' && activeBed" :title="`扎号明细 · ${activeBed.bed_no} / ${activeBed.color} / ${activeBed.style_no}`" wide width="90%" @close="closeDetails">
      <div class="sheet-banner">
        <EvidenceCapture biz-type="line_inspection" :biz-id="activeBed.id" :scene-type="detail.scene_type" label="扎号取证" />
      </div>
      <div class="toolbar">
        <button v-if="canWrite" type="button" class="btn-outline" @click="addDetail">＋ 添加行</button>
        <button type="button" class="btn-outline" @click="sortDetails">按颜色+床号+码数+扎号排序</button>
        <button type="button" class="btn-outline" @click="printSelected">批量打印菲票 ({{ selectedTicketCount }})</button>
        <span class="hint">份数 <input v-model.number="printCopies" class="n-input" type="number" min="1" max="50"></span>
        <select v-model="printOrder" title="逐张打印：同一张打印 n 份再打印下一张；逐份打印：一轮全部打完再重复">
          <option value="ticket">逐张打印</option><option value="copy">逐份打印</option>
        </select>
        <span class="hint">复制行后自动插入下面 <input v-model="copyRows" class="n-input" type="number" min="1" max="50"> 行</span>
        <button v-if="canWrite" type="button" class="btn-outline" :disabled="saving" @click="publish">发布到工单</button>
      </div>
      <div class="table-wrap scroll-x">
        <table class="detail-table">
          <thead><tr>
            <th class="chk-col"><input type="checkbox" :checked="allSelected()" :disabled="!detailRows.length" @change="toggleSelectAll" title="全选 / 取消全选"></th><th>款号</th><th>颜色</th><th>床号</th><th>码数*</th><th>扎号*</th><th>件数*</th>
            <th class="parts-col">部位（多个以 ; 分隔）</th><th>操作</th>
          </tr></thead>
          <tbody>
            <tr v-for="(r, i) in detailRows" :key="r.id || `d-${i}`">
              <td class="chk-col"><input type="checkbox" :checked="selectedDetails.includes(rowKey(r))" @change="toggleSelect(rowKey(r))"></td>
              <td class="fixed">{{ r.style_no || activeBed.style_no }}</td>
              <td class="fixed">{{ r.color || activeBed.color }}</td>
              <td class="fixed">{{ r.bed_no || activeBed.bed_no }}</td>
              <td><select v-model="r.size"><option v-for="s in ['XS','S','M','L','XL','XXL','XXXL']" :key="s" :value="s">{{ s }}</option></select></td>
              <td><input v-model="r.bundle_no" :disabled="r.locked" placeholder="扎号"></td>
              <td><input v-model.number="r.piece_qty" type="number" min="1" :disabled="r.locked"></td>
              <td class="parts-cell">
                <div class="parts-input">
                  <textarea v-model="r.parts_text" rows="2" placeholder="如：前幅;后幅;袖子" spellcheck="false" @input="onPartsInput(r, $event)"></textarea>
                  <button type="button" class="parts-more" title="勾选常用部位" @click="openPartPicker(r)">…</button>
                </div>
              </td>
              <td class="ops">
                <button type="button" class="btn-mini" @click="duplicateDetail(idx(i))">复制</button>
                <button v-if="canWrite" type="button" class="btn-mini danger" @click="removeDetail(idx(i))">删除</button>
                <button type="button" class="btn-mini" @click="printOne(r)">打印菲票</button>
              </td>
            </tr>
            <tr v-if="!detailRows.length"><td colspan="9" class="empty">暂无明细，点击「添加行」录入</td></tr>
          </tbody>
        </table>
      </div>
      <template #footer>
        <button type="button" class="btn-outline" @click="closeDetails">← 逐级返回上级表格</button>
        <button v-if="canWrite" type="button" class="btn-primary" :disabled="saving" @click="saveDetails">保存</button>
      </template>
    </ModalDialog>

    <!-- 部位勾选：仅追加输入框中尚不存在的部位 -->
    <ModalDialog v-if="modal === 'parts' && pickerRow" title="选择部位" width="420px" @close="cancelPartPicker">
      <p class="picker-hint">已根据输入框内容自动勾选对应项；确认后按勾选结果同步，模板外的自定义部位会保留。</p>
      <div class="picker-list">
        <label v-for="p in partOptions" :key="p" class="picker-item">
          <input type="checkbox" :checked="pickerChecked.includes(p)" @change="togglePickerPart(p)"> {{ p }}
        </label>
      </div>
      <template #footer>
        <button type="button" class="btn-outline" @click="cancelPartPicker">取消</button>
        <button type="button" class="btn-primary" @click="confirmPartPicker">确定</button>
      </template>
    </ModalDialog>
  </div>
</template>

<style scoped>
.page { padding: 4px 0; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; flex-wrap: wrap; margin-bottom: 14px; }
h2 { margin: 0; font-size: 18px; font-weight: 600; color: #1F2329; }
.sub { margin: 4px 0 0; color: #8A9099; font-size: 12pt; }
.head-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.filter, .form-grid input, .form-grid select, .toolbar select, .bed-table input, .bed-table select, .detail-table input, .detail-table select {
  padding: 5px 8px; border: 1px solid #DDE3EE; border-radius: 6px; font-size: 12pt; background: #fff; color: #1F2329; }
.btn-primary { padding: 7px 14px; border: 0; border-radius: 7px; background: linear-gradient(135deg, #2F6BFF, #5B8BFF); color: #fff; font-size: 12pt; cursor: pointer; }
.btn-primary:disabled { opacity: .55; cursor: not-allowed; }
.btn-outline { padding: 6px 12px; border: 1px solid #DDE3EE; border-radius: 7px; background: #fff; color: #4E5562; font-size: 12pt; cursor: pointer; }
.btn-outline:hover { border-color: #2F6BFF; color: #2F6BFF; }
.btn-link { border: 0; background: none; color: #2F6BFF; cursor: pointer; font-size: 12pt; }
.btn-mini { padding: 2px 6px; margin: 1px; border: 1px solid #E5E8EF; border-radius: 5px; background: #fff; font-size: 12pt; cursor: pointer; white-space: nowrap; }
.btn-mini.danger { color: #DC2626; }
.alert-error { color: #DC2626; background: #FEF2F2; border: 1px solid #FECACA; padding: 8px 12px; border-radius: 7px; font-size: 12pt; }
.alert-ok { color: #16A34A; background: #F0FDF4; border: 1px solid #BBF7D0; padding: 8px 12px; border-radius: 7px; font-size: 12pt; }
.status { padding: 2px 8px; border-radius: 9px; font-size: 12pt; background: #F1F3F7; color: #4E5562; }
.status.published, .status.sealed { background: #DCFCE7; color: #16A34A; }
.status.saved { background: #DBEAFE; color: #1D4ED8; }
.status.locked { background: #FEE2E2; color: #B91C1C; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-grid label { display: flex; flex-direction: column; gap: 5px; font-size: 12pt; color: #4E5562; min-width: 0; }
.label-line { display: inline-flex; align-items: center; }
.form-grid .full { grid-column: 1 / -1; }
/* 新建弹窗：组件占满列且不溢出，避免出现横向滚动条；文字不小于 12px */
.create-form label, .create-form input, .create-form select, .create-form textarea { min-width: 0; font-size: 12pt; box-sizing: border-box; }
.create-form > label > input, .create-form > label > select { width: 100%; }
.sheet-banner { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 9px 12px; margin-bottom: 10px;
  background: linear-gradient(90deg, #F5F8FF, #fff); border: 1px solid #E5EBF8; border-radius: 8px; }
.banner-text { font-size: 12pt; color: #4E5562; }
.draft-hint { font-size: 12pt; color: #16A34A; }
.toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; }
.toolbar .hint { font-size: 12pt; color: #8A9099; margin-left: auto; }
.n-input { width: 48px; }
.scroll-x { overflow-x: auto; max-height: 58vh; overflow-y: auto; }
.bed-table { min-width: 1200px; font-size: 12pt; border-collapse: separate; border-spacing: 0; }
.detail-table { width: 100%; min-width: 1080px; table-layout: fixed; font-size: 12pt; border-collapse: separate; border-spacing: 0; }
.bed-table th, .detail-table th { position: sticky; top: 0; background: #E4E7EC; z-index: 1; white-space: nowrap; border: 1px solid #fff; }
.bed-table input, .bed-table select, .detail-table input, .detail-table select { width: 100%; min-width: 74px; padding: 4px 5px; font-size: 12pt; }
/* 内容可折行、行高自适应，字体不小于 12px */
.bed-table th, .detail-table th { font-size: 12pt; white-space: normal; word-break: break-word; line-height: 1.5; }
.bed-table td, .detail-table td { padding: 4px 5px; white-space: normal; word-break: break-word; line-height: 1.5; font-size: 12pt; background: #fff; border: 1px solid #fff; }
.bed-table tr.selected { background: #F5F8FF; }
.bed-table .ratio-cell, .bed-table .readonly-cell { color: #4E5562; font-size: 12pt; }
.bed-table .readonly-cell { background: #fff; min-width: 90px; }
.fixed { color: #6B7280; background: #fff; }
.ops { white-space: normal; min-width: 150px; }
.req { color: #DC2626; }
.wo-hint { font-size: 12pt; font-weight: 400; color: #8A9099; }
/* 固定列：行固定左侧，操作列固定右侧；床次收窄，码数明细加宽 */
.bed-table th:nth-child(1),.bed-table td:nth-child(1){position:sticky;left:0;width:40px;min-width:40px;z-index:3;background:#fff}
.bed-table th:nth-child(5),.bed-table td:nth-child(5){width:50px;min-width:50px}
.bed-table td:nth-child(5) input{min-width:0}
.bed-table th:nth-child(10),.bed-table td:nth-child(10){width:112px;min-width:112px}
.bed-table th:last-child,.bed-table td:last-child{position:sticky;right:0;width:150px;min-width:150px;z-index:3;background:#fff}
.bed-table thead th:nth-child(1),.bed-table thead th:last-child{background:#E4E7EC;z-index:6}
.detail-table th.chk-col, .detail-table td.chk-col { width: 40px; min-width: 40px; padding: 2px; text-align: center; }
.detail-table .chk-col input[type=checkbox] { width: 14px; height: 14px; min-width: 0; padding: 0; }
/* 扎号、件数固定 80px；其余横向空间优先留给部位 */
.detail-table th:nth-child(2), .detail-table td:nth-child(2) { width: 160px; }
.detail-table th:nth-child(3), .detail-table td:nth-child(3) { width: 64px; }
.detail-table th:nth-child(4), .detail-table td:nth-child(4) { width: 52px; }
.detail-table th:nth-child(5), .detail-table td:nth-child(5) { width: 76px; }
.detail-table th:nth-child(6), .detail-table td:nth-child(6),
.detail-table th:nth-child(7), .detail-table td:nth-child(7) { width: 80px; min-width: 80px; max-width: 80px; }
.detail-table td:nth-child(6) input, .detail-table td:nth-child(7) input { width: 100%; min-width: 0; box-sizing: border-box; }
.detail-table th:last-child, .detail-table td:last-child { width: 130px; }
.detail-table th.parts-col, .detail-table td.parts-cell { width: auto; min-width: 0; }
.parts-input { display: flex; align-items: stretch; gap: 4px; }
.parts-input textarea { flex: 1; min-width: 0; width: 100%; min-height: 44px; padding: 4px 5px; border: 1px solid #DDE3EE; border-radius: 6px; font-size: 12pt; background: #fff; color: #1F2329; line-height: 1.35; white-space: pre-wrap; overflow-wrap: anywhere; word-break: break-all; resize: vertical; box-sizing: border-box; }
.parts-more { width: 28px; min-width: 28px; align-self: center; padding: 3px 0; border: 1px solid #DDE3EE; border-radius: 6px; background: #fff; color: #2F6BFF; font-size: 12pt; cursor: pointer; }
.picker-hint { margin: 0 0 10px; color: #4E5562; font-size: 12pt; line-height: 1.6; }
.picker-list { display: grid; grid-template-columns: repeat(2, 1fr); gap: 8px 12px; }
.picker-item { display: flex; align-items: center; gap: 6px; font-size: 12pt; color: #1F2329; cursor: pointer; }
.picker-item input { width: 14px; height: 14px; min-width: 0; padding: 0; }
.empty { text-align: center; color: #8A9099; padding: 20px; }
</style>
