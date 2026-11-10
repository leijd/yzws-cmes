<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, formatTime } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('settings:write'))
const tab = ref<'audit' | 'fields'>('audit')
const audits = ref<any[]>([])
const definitions = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const current = ref<any>(null)
const auditDetail = ref<any>(null)
const form = reactive({ owner_type: 'project', field_key: '', label: '', data_type: 'text', required: false, options: '[]', permission_code: '', sort_no: 100, status: 'active' })

const ownerTypes = [{ value: 'project', label: '项目' }, { value: 'work_order', label: '工单' }, { value: 'employee', label: '员工' }, { value: 'customer', label: '客户' }, { value: 'product', label: '产品' }]
const typeOptions = [{ value: 'text', label: '文本' }, { value: 'number', label: '数字' }, { value: 'date', label: '日期' }, { value: 'bool', label: '布尔' }, { value: 'select', label: '选项' }]

async function load() {
  loading.value = true; error.value = ''
  try {
    const [a, d] = await Promise.all([api<{ items: any[] }>('/audit-logs'), api<{ items: any[] }>('/custom-field-definitions')])
    audits.value = a.items; definitions.value = d.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function openAudit(item: any) {
  error.value = ''
  try { auditDetail.value = await api<any>(`/audit-logs/${item.id}`); modal.value = 'audit' } catch (e) { error.value = (e as Error).message }
}
function openCreate() {
  Object.assign(form, { owner_type: 'project', field_key: '', label: '', data_type: 'text', required: false, options: '[]', permission_code: '', sort_no: 100, status: 'active' })
  current.value = null; error.value = ''; modal.value = 'field'
}
function openEdit(item: any) {
  current.value = item
  Object.assign(form, { owner_type: item.owner_type, field_key: item.field_key, label: item.label, data_type: item.data_type, required: item.required, options: item.options || '[]', permission_code: item.permission_code || '', sort_no: item.sort_no, status: item.status })
  error.value = ''; modal.value = 'field'
}
async function saveField() {
  if (!form.field_key.trim() || !form.label.trim()) { error.value = '字段键和名称必填'; return }
  let options: any = []
  try { options = JSON.parse(form.options || '[]') } catch { error.value = '选项必须是合法 JSON'; return }
  saving.value = true; error.value = ''
  try {
    if (current.value) {
      await api(`/custom-field-definitions/${current.value.id}`, { method: 'PATCH', body: JSON.stringify({ label: form.label.trim(), options, sort_no: Number(form.sort_no), status: form.status, version: current.value.version }) })
    } else {
      await api('/custom-field-definitions', { method: 'POST', body: JSON.stringify({ owner_type: form.owner_type, field_key: form.field_key.trim(), label: form.label.trim(), data_type: form.data_type, required: form.required, options, display_scope: {}, permission_code: form.permission_code || null, sort_no: Number(form.sort_no) }) })
    }
    modal.value = ''; notice.value = current.value ? '字段已保存' : '字段已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function changeTab(value: 'audit' | 'fields') { tab.value = value }
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>审计与自定义字段</h1><p>操作审计追溯与业务字段扩展</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button><button v-if="tab === 'fields' && canWrite" class="button" @click="openCreate">+ 字段</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel">
    <div class="tabs"><button :class="{ active: tab === 'audit' }" @click="changeTab('audit')">审计日志</button><button :class="{ active: tab === 'fields' }" @click="changeTab('fields')">自定义字段</button></div>
    <PageState :loading="loading" />
    <div v-if="!loading && tab === 'audit'" class="table-wrap"><table><thead><tr><th>时间</th><th>动作</th><th>对象</th><th>原因</th><th>节点</th><th>请求ID</th><th>操作</th></tr></thead><tbody><tr v-for="item in audits" :key="item.id"><td>{{ formatTime(item.created_at) }}</td><td>{{ item.action }}</td><td>{{ item.owner_type }}<small class="text-muted" style="display:block">{{ item.owner_id }}</small></td><td>{{ item.reason || '—' }}</td><td>{{ item.created_node }}</td><td class="text-muted">{{ item.request_id }}</td><td><button class="button link" @click="openAudit(item)">详情</button></td></tr></tbody></table><PageState :empty="!audits.length"/></div>
    <div v-if="!loading && tab === 'fields'" class="table-wrap"><table><thead><tr><th>对象</th><th>字段键</th><th>名称</th><th>类型</th><th>必填</th><th>排序</th><th>状态</th><th>版本</th><th>操作</th></tr></thead><tbody><tr v-for="item in definitions" :key="item.id"><td>{{ item.owner_type }}</td><td><code>{{ item.field_key }}</code></td><td>{{ item.label }}</td><td>{{ item.data_type }}</td><td>{{ item.required ? '是' : '否' }}</td><td>{{ item.sort_no }}</td><td><span class="status" :class="item.status">{{ item.status === 'active' ? '启用' : '停用' }}</span></td><td>v{{ item.version }}</td><td><button v-if="canWrite" class="button link" @click="openEdit(item)">编辑</button></td></tr></tbody></table><PageState :empty="!definitions.length"/></div>
  </section>

  <ModalDialog v-if="modal === 'audit'" wide :title="`审计详情 · ${auditDetail?.action || ''}`" @close="modal = ''">
    <div class="quantity-summary"><span>动作<strong>{{ auditDetail?.action }}</strong></span><span>对象<strong>{{ auditDetail?.owner_type }}</strong></span><span>记录<strong>{{ auditDetail?.owner_id }}</strong></span><span>时间<strong>{{ formatTime(auditDetail?.created_at) }}</strong></span></div>
    <div class="form-grid"><div class="field full"><label>原因</label><input :value="auditDetail?.reason || '—'" disabled></div><div class="field"><label>操作人</label><input :value="auditDetail?.actor_id || '—'" disabled></div><div class="field"><label>请求ID</label><input :value="auditDetail?.request_id || '—'" disabled></div><div class="field full"><label>变更前</label><textarea :value="JSON.stringify(auditDetail?.before_values, null, 2)" disabled rows="5"></textarea></div><div class="field full"><label>变更后</label><textarea :value="JSON.stringify(auditDetail?.after_values, null, 2)" disabled rows="5"></textarea></div></div>
    <template #footer><button class="button secondary" @click="modal = ''">关闭</button></template>
  </ModalDialog>

  <ModalDialog v-if="modal === 'field'" :title="current ? `编辑字段 · ${current.field_key}` : '新增自定义字段'" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label>对象类型</label><FilterSelect v-model="form.owner_type" :options="ownerTypes" :disabled="!!current"/></div>
      <div class="field"><label class="required">字段键</label><input v-model.trim="form.field_key" :disabled="!!current" placeholder="例如 fabric_width"></div>
      <div class="field"><label class="required">名称</label><input v-model.trim="form.label"></div>
      <div class="field"><label>类型</label><FilterSelect v-model="form.data_type" :options="typeOptions" :disabled="!!current"/></div>
      <div class="field"><label>排序</label><input v-model.number="form.sort_no" type="number"></div>
      <div class="field"><label>必填</label><div style="display:flex;align-items:center;height:38px"><input v-model="form.required" type="checkbox"></div></div>
      <div class="field"><label>权限码</label><input v-model.trim="form.permission_code" placeholder="可空"></div>
      <div class="field"><label>状态</label><FilterSelect v-model="form.status" :options="[{value:'active',label:'启用'},{value:'disabled',label:'停用'}]"/></div>
      <div class="field full"><label>选项（JSON，select 类型使用）</label><textarea v-model="form.options" rows="2"></textarea></div>
    </div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="saveField">保存</button></template>
  </ModalDialog>
</template>
