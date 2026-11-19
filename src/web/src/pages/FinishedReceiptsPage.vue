<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import ThreeStateTabs from '../components/ThreeStateTabs.vue'
import SceneBadge from '../components/SceneBadge.vue'
import EvidenceCapture from '../components/EvidenceCapture.vue'

/* 生产管理 · 成品入库（设计 6.12.2）：复用既有 /finished-receipts 接口，三态操作面 + 取证入口。 */
const { has } = usePermissions()
const canWrite = computed(() => has('work_order:write'))
const state = ref('running')
const items = ref<any[]>([])
const workOrders = ref<any[]>([])
const warehouses = ref<any[]>([])
const bundles = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const form = reactive({ receipt_no: '', work_order_id: '', warehouse_id: '', lines: [] as any[] })
const filtered = computed(() => items.value.filter(r => {
  if (state.value === 'new') return r.status === 'draft'
  if (state.value === 'history') return r.status === 'approved'
  return r.status !== 'approved'
}))
const stateCounts = computed(() => ({
  new: items.value.filter(r => r.status === 'draft').length,
  running: items.value.filter(r => r.status !== 'approved').length,
  history: items.value.filter(r => r.status === 'approved').length,
}))
const statusText: Record<string, string> = { draft: '草稿', submitted: '待审核', approved: '已入库', reversed: '已冲销' }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [r, w, wh, b] = await Promise.all([
      api<{ items: any[] }>('/finished-receipts', {}, { silent: true }),
      api<{ items: any[]; next_cursor?: string }>('/work-orders', {}, { silent: true }),
      api<{ items: any[] }>('/warehouses', {}, { silent: true }),
      api<{ items: any[] }>('/bundles', {}, { silent: true }),
    ])
    items.value = r.items || []; workOrders.value = w.items || []; warehouses.value = wh.items || []; bundles.value = b.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, { receipt_no: '', work_order_id: '', warehouse_id: '', lines: [{ bundle_id: '', color: '', size: '', qty: 1, inspection_id: '' }] })
  error.value = ''; modal.value = 'create'
}
async function create() {
  saving.value = true; error.value = ''
  try {
    await api('/finished-receipts', { method: 'POST', body: JSON.stringify({
      ...form, lines: form.lines.map(l => ({ ...l, qty: Number(l.qty) })) }) })
    modal.value = ''; notice.value = '成品入库单已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function approve(row: any) {
  saving.value = true; error.value = ''
  try {
    await api(`/finished-receipts/${row.id}/approve`, { method: 'POST', body: JSON.stringify({ version: row.version }) })
    notice.value = '成品入库已审核'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div><h2>成品入库</h2><p class="sub">总检通过后成品入库；本厂与外协回货合格品统一入库</p></div>
      <div class="head-actions">
        <ThreeStateTabs v-model="state" :counts="stateCounts" />
        <button v-if="canWrite" type="button" class="btn-primary" @click="openCreate">＋ 新建入库单</button>
      </div>
    </header>
    <p v-if="error" class="alert-error">{{ error }}</p>
    <p v-if="notice" class="alert-ok">{{ notice }}</p>
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap">
      <table>
        <thead><tr><th>入库单号</th><th>工单</th><th>仓库</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="r in filtered" :key="r.id">
            <td><b>{{ r.receipt_no || r.id }}</b></td>
            <td>{{ r.work_order_code || r.work_order_id || '—' }}</td>
            <td>{{ r.warehouse_name || r.warehouse_id || '—' }}</td>
            <td><span class="status" :class="r.status">{{ statusText[r.status] || r.status }}</span></td>
            <td>{{ r.created_at ? new Date(r.created_at / 1000).toLocaleDateString('zh-CN') : '—' }}</td>
            <td class="ops">
              <button v-if="canWrite && r.status !== 'approved'" type="button" class="btn-mini" :disabled="saving" @click="approve(r)">审核入库</button>
              <EvidenceCapture biz-type="finished_receipt" :biz-id="r.id" label="入库取证" />
            </td>
          </tr>
          <tr v-if="!filtered.length"><td colspan="6" class="empty">当前状态下暂无入库单</td></tr>
        </tbody>
      </table>
    </div>

    <ModalDialog v-if="modal === 'create'" title="新建成品入库单" @close="modal = ''">
      <div class="form-grid">
        <label>入库单号*<input v-model="form.receipt_no"></label>
        <label>工单*<select v-model="form.work_order_id"><option value="">请选择</option>
          <option v-for="w in workOrders" :key="w.id" :value="w.id">{{ w.work_order_code }}</option></select></label>
        <label>仓库*<select v-model="form.warehouse_id"><option value="">请选择</option>
          <option v-for="w in warehouses" :key="w.id" :value="w.id">{{ w.code }} · {{ w.name }}</option></select></label>
      </div>
      <h4>入库明细</h4>
      <div v-for="(l, i) in form.lines" :key="i" class="line-row">
        <select v-model="l.bundle_id"><option value="">扎号</option>
          <option v-for="b in bundles" :key="b.id" :value="b.id">{{ b.bundle_no }} · {{ b.color }}/{{ b.size }}</option></select>
        <input v-model="l.color" placeholder="颜色"><input v-model="l.size" placeholder="尺码">
        <input v-model.number="l.qty" type="number" min="1" placeholder="数量">
        <input v-model="l.inspection_id" placeholder="总检记录 ID">
        <button type="button" class="btn-mini danger" @click="form.lines.splice(i, 1)">删除</button>
      </div>
      <button type="button" class="btn-outline" @click="form.lines.push({ bundle_id: '', color: '', size: '', qty: 1, inspection_id: '' })">＋ 添加明细</button>
      <template #footer>
        <button type="button" class="btn-outline" @click="modal = ''">取消</button>
        <button type="button" class="btn-primary" :disabled="saving || !form.receipt_no || !form.work_order_id || !form.warehouse_id" @click="create">创建</button>
      </template>
    </ModalDialog>
  </div>
</template>

<style scoped>
.page-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; flex-wrap: wrap; margin-bottom: 14px; }
h2 { margin: 0; font-size: 18px; font-weight: 600; }
h4 { font-size: 13px; margin: 14px 0 8px; }
.sub { margin: 4px 0 0; color: #8A9099; font-size: 12px; }
.head-actions { display: flex; gap: 10px; align-items: center; }
.btn-primary { padding: 7px 14px; border: 0; border-radius: 7px; background: linear-gradient(135deg,#2F6BFF,#5B8BFF); color: #fff; font-size: 13px; cursor: pointer; }
.btn-primary:disabled { opacity: .55; }
.btn-outline { padding: 6px 12px; border: 1px solid #DDE3EE; border-radius: 7px; background: #fff; font-size: 12px; cursor: pointer; }
.btn-mini { padding: 3px 8px; margin-right: 4px; border: 1px solid #E5E8EF; border-radius: 6px; background: #fff; font-size: 11px; cursor: pointer; }
.btn-mini.danger { color: #DC2626; }
.alert-error { color: #DC2626; background: #FEF2F2; border: 1px solid #FECACA; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.alert-ok { color: #16A34A; background: #F0FDF4; border: 1px solid #BBF7D0; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.status { padding: 2px 8px; border-radius: 9px; font-size: 11px; background: #F1F3F7; }
.status.approved { background: #DCFCE7; color: #16A34A; }
.empty { text-align: center; color: #8A9099; padding: 20px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-grid label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: #4E5562; }
.form-grid input, .form-grid select, .line-row input, .line-row select { padding: 6px 8px; border: 1px solid #DDE3EE; border-radius: 6px; font-size: 12px; }
.line-row { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 6px; }
</style>
