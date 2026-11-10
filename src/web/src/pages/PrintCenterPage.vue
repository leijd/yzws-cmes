<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canPrint = computed(() => has('print:write'))
const printers = ref<any[]>([])
const templates = ref<any[]>([])
const orders = ref<any[]>([])
const bundles = ref<any[]>([])
const parts = ref<any[]>([])
const printerID = ref('')
const templateID = ref('')
const orderID = ref('')
const currentBundle = ref<any>(null)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const printerForm = reactive({ name: '', model: '', connection_type: 'usb', address: '' })
const templateForm = reactive({ name: '', template_type: 'ticket', paper_width: '58', content: '{"layout":"ticket"}' })

const printerOptions = computed(() => printers.value.map(p => ({ value: p.id, label: `${p.name}${p.model ? ' · ' + p.model : ''}` })))
const templateOptions = computed(() => templates.value.map(t => ({ value: t.id, label: `${t.name}${t.paper_width ? ' · ' + t.paper_width + 'mm' : ''}` })))
const orderOptions = computed(() => orders.value.map(o => ({ value: o.id, label: `${o.work_order_code} · ${o.product_name}` })))
const selectedBundle = computed(() => bundles.value.find(b => b.id === currentBundle.value?.id))

async function load() {
  loading.value = true; error.value = ''
  try {
    const [p, t, o] = await Promise.all([api<{ items: any[] }>('/printers'), api<{ items: any[] }>('/print-templates'), api<{ items: any[] }>('/work-orders')])
    printers.value = p.items; templates.value = t.items; orders.value = o.items
    if (!printerID.value && printers.value.length) printerID.value = printers.value[0].id
    if (!templateID.value && templates.value.length) templateID.value = templates.value[0].id
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function loadBundles() {
  bundles.value = []; parts.value = []; currentBundle.value = null
  if (!orderID.value) return
  try {
    bundles.value = (await api<{ items: any[] }>(`/bundles?work_order_id=${orderID.value}`)).items
    if (bundles.value.length) await selectBundle(bundles.value[0])
  } catch (e) { error.value = (e as Error).message }
}
async function selectBundle(bundle: any) {
  currentBundle.value = bundle
  parts.value = []
  try { parts.value = (await api<{ items: any[] }>(`/bundles/${bundle.id}/parts`)).items } catch (e) { error.value = (e as Error).message }
}
function requireSetup() {
  if (!printerID.value) { error.value = '请先创建并选择打印机'; return false }
  if (!templateID.value) { error.value = '请先创建并选择打印模板'; return false }
  return true
}
async function printPart(part: any) {
  if (!requireSetup() || !part.ticket_id) return
  saving.value = true; error.value = ''
  try {
    const job = await api<{ id: string }>('/print-jobs', { method: 'POST', body: JSON.stringify({ printer_id: printerID.value, template_id: templateID.value, ticket_id: part.ticket_id, copies: 1 }) })
    lastJobByTicket[part.ticket_id] = job.id
    await api(`/print-jobs/${job.id}/receipt`, { method: 'POST', body: JSON.stringify({ result: 'succeeded' }) })
    notice.value = `已打印 ${currentBundle.value?.bundle_no} · ${part.part_name}`
    await selectBundle(currentBundle.value)
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function reprintPart(part: any) {
  if (!requireSetup() || !part.ticket_id) return
  const reason = window.prompt('请输入补打原因')
  if (!reason) return
  const originalJobID = lastJobByTicket[part.ticket_id]
  if (!originalJobID) { error.value = '未找到原打印任务，请先在本页打印一次后再补打'; return }
  saving.value = true; error.value = ''
  try {
    const reprint = await api<{ id: string }>(`/print-jobs/${originalJobID}/reprint`, { method: 'POST', body: JSON.stringify({ reason, copies: 1 }) })
    await api(`/print-jobs/${reprint.id}/receipt`, { method: 'POST', body: JSON.stringify({ result: 'succeeded' }) })
    notice.value = `已补打 ${currentBundle.value?.bundle_no} · ${part.part_name}`
    await selectBundle(currentBundle.value)
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function createPrinter() {
  if (!printerForm.name.trim()) { error.value = '请输入打印机名称'; return }
  saving.value = true; error.value = ''
  try {
    await api('/printers', { method: 'POST', body: JSON.stringify({ name: printerForm.name.trim(), model: printerForm.model || null, connection_type: printerForm.connection_type, address: printerForm.address || null, capabilities: {} }) })
    modal.value = ''; notice.value = '打印机已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function createTemplate() {
  if (!templateForm.name.trim()) { error.value = '请输入模板名称'; return }
  let content: any = {}
  try { content = JSON.parse(templateForm.content) } catch { content = { layout: templateForm.template_type } }
  saving.value = true; error.value = ''
  try {
    await api('/print-templates', { method: 'POST', body: JSON.stringify({ name: templateForm.name.trim(), template_type: templateForm.template_type, paper_width: templateForm.paper_width || null, content }) })
    modal.value = ''; notice.value = '打印模板已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
const printText: Record<string, string> = { pending: '未打印', printed: '已打印', voided: '已作废' }
const lastJobByTicket = reactive<Record<string, string>>({})
const scanPayload = ref('')
const scanResult = ref<any>(null)
async function resolveScan() {
  if (!scanPayload.value.trim()) return
  error.value = ''
  try { scanResult.value = await api<any>('/scan/resolve?payload=' + encodeURIComponent(scanPayload.value.trim())) } catch (e) { scanResult.value = null; error.value = (e as Error).message }
}
async function voidTicket(part: any) {
  if (!part.ticket_id) return
  const reason = window.prompt('请输入作废原因')
  if (!reason) return
  saving.value = true; error.value = ''
  try { await api(`/tickets/${part.ticket_id}/void`, { method: 'POST', body: JSON.stringify({ reason }) }); notice.value = '菲票已作废'; await selectBundle(currentBundle.value) } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>菲票打印中心</h1><p>按扎打印、回执与补打；补打保留原票号并递增版本</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button><button v-if="canPrint" class="button secondary" @click="modal = 'printer'">+ 打印机</button><button v-if="canPrint" class="button" @click="modal = 'template'">+ 打印模板</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><div class="toolbar"><FilterSelect v-model="printerID" :options="printerOptions" placeholder="选择打印机"/><FilterSelect v-model="templateID" :options="templateOptions" placeholder="选择模板"/><FilterSelect v-model="orderID" :options="orderOptions" placeholder="选择工单" @change="loadBundles"/><span class="spacer"></span><span class="text-muted" v-if="!printers.length || !templates.length">请先创建打印机和模板</span></div><div class="toolbar"><input v-model.trim="scanPayload" class="search-input" placeholder="扫描/输入菲票码或扎号" @keyup.enter="resolveScan"><button class="button secondary" @click="resolveScan">解析</button><span class="text-muted" v-if="scanResult">{{ scanResult.work_order_code }} · 扎 {{ scanResult.bundle_no }} · {{ scanResult.part_name || '整件' }} · 模式 {{ scanResult.report_mode }} · 工序 {{ scanResult.processes?.length || 0 }} 道</span></div>
    <PageState :loading="loading"/>
    <div v-if="!loading && !orderID" class="panel-body"><div class="notice" style="margin:0">选择工单后按扎查看部位与菲票状态。</div></div>
    <div v-if="!loading && orderID" class="panel-body">
      <div v-if="!bundles.length" class="notice" style="margin:0">该工单暂无扎，请先完成裁床确认。</div>
      <div v-else class="form-grid">
        <div class="field full"><label>扎</label><FilterSelect :model-value="currentBundle?.id || ''" :options="bundles.map(b => ({ value: b.id, label: `${b.bundle_no} · ${b.color}/${b.size} · ${b.current_qty} 件` }))" @change="(id: string) => selectBundle(bundles.find(b => b.id === id))"/></div>
        <div class="field full"><div class="quantity-summary"><span>扎号<strong>{{ currentBundle?.bundle_no }}</strong></span><span>颜色/尺码<strong>{{ currentBundle?.color }}/{{ currentBundle?.size }}</strong></span><span>件数<strong>{{ currentBundle?.current_qty }}</strong></span><span>部位数<strong>{{ parts.length }}</strong></span></div></div>
      </div>
      <div v-if="parts.length" class="table-wrap"><table><thead><tr><th>部位</th><th>件数</th><th>票号</th><th>打印状态</th><th>打印次数</th><th>版本</th><th>操作</th></tr></thead><tbody><tr v-for="part in parts" :key="part.id"><td>{{ part.part_name }}</td><td>{{ part.qty }}</td><td class="text-muted">{{ part.ticket_id || '未生成' }}</td><td><span class="status" :class="part.print_status === 'printed' ? 'approved' : 'pending'">{{ printText[part.print_status] || part.print_status }}</span></td><td>{{ part.print_count }}</td><td>v{{ part.print_revision }}</td><td><button v-if="canPrint && part.ticket_id && part.print_status !== 'printed' && part.print_status !== 'voided'" class="button small" :disabled="saving" @click="printPart(part)">打印</button><button v-if="canPrint && part.ticket_id && part.print_status === 'printed'" class="button link" :disabled="saving" @click="reprintPart(part)">补打</button><button v-if="canPrint && part.ticket_id && part.print_status !== 'voided'" class="button link text-danger" :disabled="saving" @click="voidTicket(part)">作废</button></td></tr></tbody></table></div>
    </div>
  </section>

  <ModalDialog v-if="modal === 'printer'" title="新增打印机" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid"><div class="field"><label class="required">名称</label><input v-model.trim="printerForm.name" placeholder="如 芯烨XP-58"></div><div class="field"><label>型号</label><input v-model.trim="printerForm.model"></div><div class="field"><label>连接方式</label><FilterSelect v-model="printerForm.connection_type" :options="[{value:'usb',label:'USB'},{value:'network',label:'网络'}]"/></div><div class="field"><label>地址</label><input v-model.trim="printerForm.address" placeholder="IP 或设备路径"></div></div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !printerForm.name" @click="createPrinter">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'template'" title="新增打印模板" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid"><div class="field"><label class="required">名称</label><input v-model.trim="templateForm.name" placeholder="如 菲票58mm"></div><div class="field"><label>类型</label><FilterSelect v-model="templateForm.template_type" :options="[{value:'ticket',label:'菲票'},{value:'label',label:'产品标签'}]"/></div><div class="field"><label>纸宽(mm)</label><input v-model.trim="templateForm.paper_width"></div><div class="field full"><label>内容(JSON)</label><textarea v-model="templateForm.content" rows="3"></textarea></div></div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !templateForm.name" @click="createTemplate">保存</button></template>
  </ModalDialog>
</template>
