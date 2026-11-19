<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import PageState from '../components/PageState.vue'
import ThreeStateTabs from '../components/ThreeStateTabs.vue'

/* 基本资料 · 标准工序（设计 6.12.2，复用 5.13 /process-standards API：两确认 + 授权）。 */
const { has } = usePermissions()
const canWrite = computed(() => has('process:write'))
const items = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')
const state = ref('running')
const modal = ref('')
const current = ref<any>(null)
const form = reactive({ name: '', product_id: '', line_id: '', remark: '' })
const products = ref<any[]>([])
const lines = ref<any[]>([])

const filtered = computed(() => items.value.filter(s => {
  if (state.value === 'new') return s.status === 'draft' || s.status === 'pending_confirm'
  if (state.value === 'history') return s.status === 'superseded'
  return s.status === 'standard' || s.status === 'revising'
}))
const statusText: Record<string, string> = { draft: '草稿', pending_confirm: '待二次确认', standard: '生产标准', revising: '修订中', superseded: '已归档' }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [s, p, l] = await Promise.all([
      api<{ items: any[] }>('/process-standards', {}, { silent: true }),
      api<{ items: any[] }>('/products', {}, { silent: true }),
      api<{ items: any[] }>('/lines', {}, { silent: true }),
    ])
    items.value = s.items || []; products.value = p.items || []; lines.value = l.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openCreate() { Object.assign(form, { name: '', product_id: '', line_id: '', remark: '' }); error.value = ''; modal.value = 'create' }
async function create() {
  try {
    await api('/process-standards', { method: 'POST', body: JSON.stringify({ ...form }) })
    modal.value = ''; notice.value = '标准工序草稿已创建'; await load()
  } catch (e) { error.value = (e as Error).message }
}
async function action(id: string, act: string) {
  try {
    await api(`/process-standards/${id}/${act}`, { method: 'POST', body: JSON.stringify({}) })
    notice.value = `已完成 ${act}`; await load()
  } catch (e) { error.value = (e as Error).message }
}
function openDetail(row: any) { current.value = row; modal.value = 'detail' }
onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div><h2>标准工序</h2><p class="sub">工序表草稿 → 两次确认 → 生产标准；查看需授权（设计 4.18 / 5.13）</p></div>
      <div class="head-actions">
        <ThreeStateTabs v-model="state" />
        <button v-if="canWrite" type="button" class="btn-primary" @click="openCreate">＋ 新建工序表</button>
      </div>
    </header>
    <p v-if="error" class="alert-error">{{ error }}</p>
    <p v-if="notice" class="alert-ok">{{ notice }}</p>
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap">
      <table>
        <thead><tr><th>工序表编号</th><th>名称</th><th>产品</th><th>产线</th><th>状态</th><th>版本</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="s in filtered" :key="s.id">
            <td><b>{{ s.sheet_no || s.code || s.id }}</b></td>
            <td>{{ s.name || '—' }}</td><td>{{ s.product_name || s.product_id || '—' }}</td>
            <td>{{ s.line_name || s.line_id || '—' }}</td>
            <td><span class="status" :class="s.status">{{ statusText[s.status] || s.status }}</span></td>
            <td>{{ s.version || 1 }}</td>
            <td class="ops">
              <button type="button" class="btn-mini" @click="openDetail(s)">详情</button>
              <button v-if="canWrite && (s.status === 'draft' || s.status === 'revising')" type="button" class="btn-mini" @click="action(s.id, 'submit')">提交确认</button>
              <button v-if="canWrite && s.status === 'pending_confirm'" type="button" class="btn-mini" @click="action(s.id, 'confirm')">二次确认</button>
              <button v-if="canWrite && s.status === 'standard'" type="button" class="btn-mini" @click="action(s.id, 'revise')">新建修订</button>
            </td>
          </tr>
          <tr v-if="!filtered.length"><td colspan="7" class="empty">当前状态下暂无工序表</td></tr>
        </tbody>
      </table>
    </div>

    <div v-if="modal === 'create'" class="modal-mask" @click.self="modal = ''">
      <div class="modal-box">
        <h3>新建标准工序表</h3>
        <div class="form-grid">
          <label>名称<input v-model="form.name"></label>
          <label>产品<select v-model="form.product_id"><option value="">请选择</option>
            <option v-for="p in products" :key="p.id" :value="p.id">{{ p.name }}</option></select></label>
          <label>产线<select v-model="form.line_id"><option value="">请选择</option>
            <option v-for="l in lines" :key="l.id" :value="l.id">{{ l.name }}</option></select></label>
          <label class="full">备注<input v-model="form.remark"></label>
        </div>
        <div class="modal-actions">
          <button type="button" class="btn-outline" @click="modal = ''">取消</button>
          <button type="button" class="btn-primary" :disabled="!form.name" @click="create">创建草稿</button>
        </div>
      </div>
    </div>

    <div v-if="modal === 'detail' && current" class="modal-mask" @click.self="modal = ''">
      <div class="modal-box">
        <h3>{{ current.name || '标准工序表' }}</h3>
        <pre class="json">{{ JSON.stringify(current, null, 2) }}</pre>
        <div class="modal-actions"><button type="button" class="btn-outline" @click="modal = ''">关闭</button></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; flex-wrap: wrap; margin-bottom: 14px; }
h2 { margin: 0; font-size: 18px; font-weight: 600; }
h3 { margin: 0 0 14px; font-size: 15px; }
.sub { margin: 4px 0 0; color: #8A9099; font-size: 12px; }
.head-actions { display: flex; gap: 10px; align-items: center; }
.btn-primary { padding: 7px 14px; border: 0; border-radius: 7px; background: linear-gradient(135deg,#2F6BFF,#5B8BFF); color: #fff; font-size: 13px; cursor: pointer; }
.btn-primary:disabled { opacity: .55; }
.btn-outline { padding: 6px 12px; border: 1px solid #DDE3EE; border-radius: 7px; background: #fff; font-size: 12px; cursor: pointer; }
.btn-mini { padding: 3px 8px; margin-right: 4px; border: 1px solid #E5E8EF; border-radius: 6px; background: #fff; font-size: 11px; cursor: pointer; }
.alert-error { color: #DC2626; background: #FEF2F2; border: 1px solid #FECACA; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.alert-ok { color: #16A34A; background: #F0FDF4; border: 1px solid #BBF7D0; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.status { padding: 2px 8px; border-radius: 9px; font-size: 11px; background: #F1F3F7; }
.status.standard { background: #DCFCE7; color: #16A34A; }
.status.pending_confirm { background: #FEF3C7; color: #B45309; }
.empty { text-align: center; color: #8A9099; padding: 20px; }
.modal-mask { position: fixed; inset: 0; background: rgba(15,23,42,.42); display: flex; align-items: center; justify-content: center; z-index: 60; }
.modal-box { width: min(90vw, 640px); max-height: 90vh; overflow: auto; padding: 20px; border-radius: 12px; background: #fff; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-grid label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: #4E5562; }
.form-grid .full { grid-column: 1 / -1; }
.form-grid input, .form-grid select { padding: 7px 9px; border: 1px solid #DDE3EE; border-radius: 7px; font-size: 13px; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.json { background: #F5F7FA; padding: 12px; border-radius: 8px; font-size: 11px; max-height: 50vh; overflow: auto; }
</style>
