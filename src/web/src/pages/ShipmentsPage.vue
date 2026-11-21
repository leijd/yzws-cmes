<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { todayISO } from '../date'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import DatePicker from '../components/DatePicker.vue'
import PageState from '../components/PageState.vue'
import SceneBadge from '../components/SceneBadge.vue'
import ThreeStateTabs from '../components/ThreeStateTabs.vue'
import EvidenceCapture from '../components/EvidenceCapture.vue'

/* 发货（设计 6.12.2 / 6.12.7）：A 出货给客商、B/C 回交委托方；
 * 外协单据标题带 [外协] 前缀、列表行外协角标，委托方视角可分「本厂/外协」。 */
const { has } = usePermissions()
const canWrite = computed(() => has('shipment:write') || has('work_order:write'))
const state = ref('running')
const partyFilter = ref('')
const items = ref<any[]>([])
const projects = ref<any[]>([])
const bundles = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const current = ref<any>(null)
const form = reactive({ shipment_no: '', project_id: '', shipment_date: '', lines: [] as any[], remark: '' })

const filtered = computed(() => items.value.filter(s => {
  if (partyFilter.value && s.party !== partyFilter.value) return false
  if (state.value === 'new') return s.status === 'draft'
  if (state.value === 'history') return s.status === 'confirmed'
  return s.status !== 'confirmed'
}))
const stateCounts = computed(() => ({
  new: items.value.filter(s => s.status === 'draft').length,
  running: items.value.filter(s => s.status !== 'confirmed').length,
  history: items.value.filter(s => s.status === 'confirmed').length,
}))
function blankLine() { return { work_order_id: '', bundle_id: '', color: '', size: '', qty: 1 } }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [s, p, b] = await Promise.all([
      api<{ items: any[] }>('/scene/shipments'),
      api<{ items: any[] }>('/projects', {}, { silent: true }),
      api<{ items: any[] }>('/bundles', {}, { silent: true }),
    ])
    items.value = s.items; projects.value = p.items; bundles.value = b.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, { shipment_no: '', project_id: '', shipment_date: todayISO(), lines: [blankLine()], remark: '' })
  error.value = ''; modal.value = 'create'
}
async function create() {
  saving.value = true; error.value = ''
  try {
    await api('/shipments', { method: 'POST', body: JSON.stringify({
      shipment_no: form.shipment_no, project_id: form.project_id,
      shipment_date: new Date(form.shipment_date || Date.now()).getTime(),
      lines: form.lines.map(l => ({ ...l, qty: Number(l.qty) })), remark: form.remark }) })
    modal.value = ''; notice.value = '发货单已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function confirm(row: any) {
  saving.value = true; error.value = ''
  try {
    await api(`/shipments/${row.id}/confirm`, { method: 'POST', body: JSON.stringify({ version: row.version }) })
    notice.value = '发货单已确认'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function openDetail(row: any) { current.value = row; modal.value = 'detail' }
onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div><h2>发货</h2><p class="sub">A 场景出货给客商；B/C 场景回交委托方（外协单据带 [外协] 标识）</p></div>
      <div class="head-actions">
        <ThreeStateTabs v-model="state" :counts="stateCounts" />
        <select v-model="partyFilter" class="filter"><option value="">全部</option><option value="本厂">本厂</option><option value="外协">外协</option></select>
        <button v-if="canWrite" type="button" class="btn-primary" @click="openCreate">＋ 新建发货单</button>
      </div>
    </header>
    <p v-if="error" class="alert-error">{{ error }}</p>
    <p v-if="notice" class="alert-ok">{{ notice }}</p>
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap">
      <table>
        <thead><tr><th>发货单号</th><th>外协标识</th><th>项目</th><th>对方</th><th>类型</th><th>数量</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="s in filtered" :key="s.id">
            <td><b>{{ s.title_prefix }}{{ s.shipment_no }}</b></td>
            <td><SceneBadge :scene-type="s.scene_type" :scene-sub="s.scene_sub" :outsource="s.outsource" :party="s.party" compact /></td>
            <td>{{ s.project_code || '—' }}</td>
            <td>{{ s.counterparty || '—' }}<small v-if="s.source_order_ref" class="sub-ref"> · {{ s.source_order_ref }}</small></td>
            <td>{{ s.delivery_type }}</td>
            <td>{{ s.qty }}</td>
            <td><span class="status" :class="s.status">{{ s.status === 'confirmed' ? '已确认' : '草稿' }}</span></td>
            <td class="ops">
              <button type="button" class="btn-mini" @click="openDetail(s)">详情</button>
              <button v-if="canWrite && s.status === 'draft'" type="button" class="btn-mini" :disabled="saving" @click="confirm(s)">确认</button>
            </td>
          </tr>
          <tr v-if="!filtered.length"><td colspan="8" class="empty">当前状态下暂无发货单</td></tr>
        </tbody>
      </table>
    </div>

    <ModalDialog v-if="modal === 'create'" title="新建发货单" @close="modal = ''">
      <div class="form-grid">
        <label>发货单号*<input v-model="form.shipment_no"></label>
        <label>项目*<select v-model="form.project_id"><option value="">请选择</option>
          <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.project_code }} · {{ p.product_name }}</option></select></label>
        <label>发货日期<DatePicker v-model="form.shipment_date"/></label>
        <label class="full">备注<input v-model="form.remark"></label>
      </div>
      <h4>发货明细（按扎）</h4>
      <div v-for="(l, i) in form.lines" :key="i" class="line-row">
        <select v-model="l.work_order_id"><option value="">工单</option>
          <option v-for="b in bundles" :key="b.id" :value="b.work_order_id">{{ b.work_order_id }}</option></select>
        <select v-model="l.bundle_id"><option value="">扎号</option>
          <option v-for="b in bundles" :key="b.id" :value="b.id">{{ b.bundle_no }} · {{ b.color }}/{{ b.size }}</option></select>
        <input v-model="l.color" placeholder="颜色"><input v-model="l.size" placeholder="尺码">
        <input v-model.number="l.qty" type="number" min="1" placeholder="数量">
        <button type="button" class="btn-mini danger" @click="form.lines.splice(i, 1)">删除</button>
      </div>
      <button type="button" class="btn-outline" @click="form.lines.push(blankLine())">＋ 添加明细</button>
      <template #footer>
        <button type="button" class="btn-outline" @click="modal = ''">取消</button>
        <button type="button" class="btn-primary" :disabled="saving || !form.shipment_no || !form.project_id" @click="create">创建</button>
      </template>
    </ModalDialog>

    <ModalDialog v-if="modal === 'detail' && current" :title="`${current.title_prefix}发货单 ${current.shipment_no}`" @close="modal = ''">
      <div class="outsource-banner" v-if="current.outsource">
        <b>[外协]</b> 对方：{{ current.counterparty || '—' }} · 对方单号：{{ current.source_order_ref || '—' }} · 类型：{{ current.delivery_type }}
      </div>
      <div class="detail-grid">
        <div><span>业务场景</span><SceneBadge :scene-type="current.scene_type" :scene-sub="current.scene_sub" :outsource="current.outsource" /></div>
        <div><span>项目</span>{{ current.project_code || '—' }}</div>
        <div><span>数量</span>{{ current.qty }}</div>
        <div><span>状态</span>{{ current.status === 'confirmed' ? '已确认' : '草稿' }}</div>
      </div>
      <div class="evidence-row">
        <EvidenceCapture biz-type="shipment" :biz-id="current.id" :scene-type="current.scene_type" :scene-sub="current.scene_sub" label="发货取证 / 直播" />
      </div>
      <template #footer><button type="button" class="btn-outline" @click="modal = ''">关闭</button></template>
    </ModalDialog>
  </div>
</template>

<style scoped>
.page-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; flex-wrap: wrap; margin-bottom: 14px; }
h2 { margin: 0; font-size: 18px; font-weight: 600; }
.sub { margin: 4px 0 0; font-size: 12px; color: #8A9099; }
h4 { font-size: 13px; margin: 16px 0 8px; }
.head-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.filter, .form-grid input, .form-grid select, .line-row input, .line-row select { padding: 6px 8px; border: 1px solid #DDE3EE; border-radius: 6px; font-size: 12px; }
.btn-primary { padding: 7px 14px; border: 0; border-radius: 7px; background: linear-gradient(135deg,#2F6BFF,#5B8BFF); color: #fff; font-size: 13px; cursor: pointer; }
.btn-primary:disabled { opacity: .55; }
.btn-outline { padding: 6px 12px; border: 1px solid #DDE3EE; border-radius: 7px; background: #fff; font-size: 12px; cursor: pointer; }
.btn-mini { padding: 3px 8px; border: 1px solid #E5E8EF; border-radius: 6px; background: #fff; font-size: 11px; cursor: pointer; margin-right: 4px; }
.btn-mini.danger { color: #DC2626; }
.alert-error { color: #DC2626; background: #FEF2F2; border: 1px solid #FECACA; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.alert-ok { color: #16A34A; background: #F0FDF4; border: 1px solid #BBF7D0; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.status { padding: 2px 8px; border-radius: 9px; font-size: 11px; background: #F1F3F7; }
.status.confirmed { background: #DCFCE7; color: #16A34A; }
.sub-ref { color: #8A9099; font-size: 11px; }
.empty { text-align: center; color: #8A9099; padding: 20px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-grid label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: #4E5562; }
.form-grid .full { grid-column: 1 / -1; }
.line-row { display: flex; gap: 6px; margin-bottom: 6px; flex-wrap: wrap; }
.outsource-banner { padding: 10px 12px; border-radius: 8px; background: #FEF3C7; border: 1px solid #FCD34D; color: #92400E; font-size: 12px; margin-bottom: 14px; }
.detail-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; font-size: 13px; }
.detail-grid span { display: block; color: #8A9099; font-size: 11px; margin-bottom: 4px; }
.evidence-row { margin-top: 16px; padding-top: 12px; border-top: 1px solid #EEF1F6; }
</style>
