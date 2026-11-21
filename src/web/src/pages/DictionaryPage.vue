<script setup lang="ts">
/**
 * 字典维护页（基础资料）：下拉选项的数据源维护。
 * 左侧字典类型（本厂已有类型），右侧选项项列表；新增时编码留空按自动编码规则生成
 * （{类型名拼音首字母}-{5位流水}，如 ZDX-00001）。选项经 /dictionaries/{type}/options
 * 对外提供（60 秒缓存），保存后服务端自动失效对应缓存。
 * 权限：查看 masterdata:read，维护 masterdata:write。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { api, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('masterdata:write'))
const canWriteMaterial = computed(() => has('inventory:receipt'))
const isMaterialType = computed(() => currentType.value === 'material_type')

const TYPE_NAMES: Record<string, string> = {
  nation: '民族', position: '岗位', skill_grade: '技能评级', rate_level: '单价档位',
  material_type: '物料类型', test_type: '测试类型',
}
const typeName = (t: string) => TYPE_NAMES[t] || t
function matTypeName(code: string) {
  return matTypeList.value.find(i => i.code === code)?.name || items.value.find(i => i.code === code)?.name || code
}

const types = ref<{ type: string; total: number; active: number }[]>([])
const currentType = ref('')
const items = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')

/* 物料类型 → 物料名称 两级字典状态 */
const matTypeList = ref<any[]>([])
const currentMatType = ref('')
const matItems = ref<any[]>([])
const matTotal = ref(0)
const matPage = ref(1)
const matPageSize = ref(20)
const matKeyword = ref('')
const matSort = ref('code')
const matOrder = ref<'asc' | 'desc'>('asc')
const matTypeNameOrder = ref<'' | 'asc' | 'desc'>('')
const displayedMatTypes = computed(() => {
  if (!isMaterialType.value || !matTypeNameOrder.value) return items.value
  const dir = matTypeNameOrder.value === 'asc' ? 1 : -1
  return [...items.value].sort((a, b) => dir * String(a.name || '').localeCompare(String(b.name || ''), 'zh-CN'))
})
const matTypeNameMark = computed(() => matTypeNameOrder.value === 'asc' ? '▲' : matTypeNameOrder.value === 'desc' ? '▼' : '↕')
const matLoading = ref(false)
const matTypeSaving = ref(false)
const matEditing = ref<any>(null)
const matDraft = ref(false)
const matForm = reactive({ code: '', name: '', specification: '', unit: '件', version: 0 })

async function loadTypes(keepSelection = true) {
  try {
    const r = await api<{ items: { type: string; total: number; active: number }[] }>('/dictionaries/types')
    types.value = r.items || []
    if (!keepSelection || !types.value.some(t => t.type === currentType.value)) {
      currentType.value = types.value[0]?.type || ''
    }
    if (currentType.value) {
      await loadItems()
      if (isMaterialType.value) await loadMatTypes()
    }
  } catch (e) { error.value = (e as Error).message }
}
async function loadItems() {
  if (!currentType.value) { items.value = []; matTypeNameOrder.value = ''; return }
  matTypeNameOrder.value = ''
  loading.value = true; error.value = ''
  try {
    const r = await api<{ items: any[] }>(`/dictionaries/${currentType.value}/items`)
    items.value = r.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function selectType(t: string) {
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  currentType.value = t; void loadItems()
  if (isMaterialType.value) void loadMatTypes(); else { matTypeList.value = []; matItems.value = [] }
}

/* ===== 新增 / 编辑 ===== */
const showForm = ref(false)
const editing = ref<any>(null)
const form = reactive({ code: '', name: '', sort_no: 0 })
const saving = ref(false)

function openCreate() {
  editing.value = null
  Object.assign(form, { code: '', name: '', sort_no: (items.value.length + 1) })
  error.value = ''
  showForm.value = true
}
function openEdit(item: any) {
  editing.value = item
  Object.assign(form, { code: item.code, name: item.name, sort_no: item.sort_no })
  error.value = ''
  showForm.value = true
}
async function save() {
  if (!form.name.trim()) { error.value = '名称必填'; return }
  saving.value = true; error.value = ''
  try {
    if (editing.value) {
      await api(`/dictionaries/items/${editing.value.id}`, { method: 'PATCH', body: JSON.stringify({ name: form.name.trim(), sort_no: form.sort_no, status: editing.value.status, version: editing.value.version }) })
      notice.value = '选项已保存'
    } else {
      await api(`/dictionaries/${currentType.value}/items`, { method: 'POST', body: JSON.stringify({ code: form.code.trim(), name: form.name.trim(), sort_no: form.sort_no }) })
      notice.value = '选项已新增'
    }
    showForm.value = false
    await onMainItemSaved()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function toggleStatus(item: any) {
  error.value = ''
  try {
    await api(`/dictionaries/items/${item.id}`, { method: 'PATCH', body: JSON.stringify({ name: item.name, sort_no: item.sort_no, status: item.status === 'active' ? 'disabled' : 'active', version: item.version }) })
    notice.value = item.status === 'active' ? `已停用「${item.name}」` : `已启用「${item.name}」`
    await onMainItemSaved()
  } catch (e) { error.value = (e as Error).message }
}

/* ===== 物料类型 → 物料名称 两级字典维护 ===== */
async function loadMatTypes() {
  if (!isMaterialType.value) { matTypeList.value = []; return }
  try {
    const r = await api<{ items: any[] }>('/dictionaries/material_type/items')
    matTypeList.value = (r.items || []).filter(i => i.status === 'active')
    if (!matTypeList.value.some(i => i.code === currentMatType.value)) {
      currentMatType.value = matTypeList.value[0]?.code || ''
    }
    await loadMatItems()
  } catch (e) { error.value = (e as Error).message }
}
async function loadMatItems() {
  if (!currentMatType.value) { matItems.value = []; matTotal.value = 0; return }
  matLoading.value = true
  try {
    const qs = new URLSearchParams({ material_type: currentMatType.value, keyword: matKeyword.value.trim(), page: String(matPage.value), page_size: String(matPageSize.value), sort: matSort.value, order: matOrder.value })
    const r = await api<{ items: any[]; total: number }>(`/material-receipts/materials?${qs.toString()}`)
    matItems.value = r.items || []; matTotal.value = r.total || 0
  } catch (e) { error.value = (e as Error).message } finally { matLoading.value = false }
}
function searchMat() {
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  matPage.value = 1; void loadMatItems()
}
function sortMatBy(field: string) {
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  if (matSort.value === field) matOrder.value = matOrder.value === 'asc' ? 'desc' : 'asc'
  else { matSort.value = field; matOrder.value = 'asc' }
  matPage.value = 1; void loadMatItems()
}
function sortMark(field: string) { return matSort.value === field ? (matOrder.value === 'asc' ? '▲' : '▼') : '↕' }
function changeMatPageSize() { matPage.value = 1; void loadMatItems() }
function selectMatType(code: string) {
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  currentMatType.value = code; matPage.value = 1; matKeyword.value = ''; void loadMatItems()
}
function addMatType() {
  if (!canWrite.value) return
  showForm.value = true; editing.value = null
  Object.assign(form, { code: '', name: '', sort_no: matTypeList.value.length + 1 })
  error.value = ''
}
function sortMatTypesByName() {
  matTypeNameOrder.value = matTypeNameOrder.value === '' ? 'asc' : matTypeNameOrder.value === 'asc' ? 'desc' : ''
}
function matTypeStoredIndex(item: any) { return matTypeList.value.findIndex(i => i.id === item.id) }
async function moveMatType(item: any, delta: number) {
  if (!canWrite.value || matTypeSaving.value) return
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  const index = matTypeList.value.findIndex(i => i.id === item.id)
  const target = index + delta
  if (index < 0 || target < 0 || target >= matTypeList.value.length) return
  const next = [...matTypeList.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  matTypeSaving.value = true; error.value = ''
  try {
    for (let i = 0; i < next.length; i++) {
      if (next[i].sort_no === i + 1) continue
      await api(`/dictionaries/items/${next[i].id}`, { method: 'PATCH', body: JSON.stringify({ name: next[i].name, sort_no: i + 1, status: next[i].status, version: next[i].version }) })
    }
    notice.value = '物料类型顺序已调整'
    await loadTypes()
  } catch (e) { error.value = (e as Error).message } finally { matTypeSaving.value = false }
}
async function removeMatType(item: any) {
  if (!canWrite.value) return
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  try {
    const r = await api<{ total: number }>(`/material-receipts/materials?material_type=${encodeURIComponent(item.code)}&page=1&page_size=1`)
    if ((r.total || 0) > 0) { error.value = `物料类型「${item.name}」下已有 ${r.total} 项物料，不能删除`; return }
  } catch { /* 检查失败则继续交由后端校验 */ }
  if (!window.confirm(`确定删除物料类型「${item.name}」？`)) return
  await toggleStatus(item)
  await loadMatTypes()
}
function startMatCreate() {
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  matDraft.value = true; matEditing.value = null
  Object.assign(matForm, { code: '', name: '', specification: '', unit: '件', version: 0 })
  error.value = ''
}
function startMatEdit(m: any) {
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  matEditing.value = m; matDraft.value = false
  Object.assign(matForm, { code: m.code || '', name: m.name || '', specification: m.specification || '', unit: m.unit || '件', version: m.version || 0 })
  error.value = ''
}
function cancelMatEdit() { matDraft.value = false; matEditing.value = null; error.value = '' }
async function saveMat() {
  if (!matForm.name.trim()) { error.value = '物料名称必填'; return }
  matLoading.value = true; error.value = ''
  try {
    const body = { code: matForm.code.trim(), name: matForm.name.trim(), specification: matForm.specification.trim(), unit: matForm.unit || '件', material_type: currentMatType.value }
    if (matEditing.value) {
      await api(`/material-receipts/materials/${matEditing.value.id}`, { method: 'PATCH', body: JSON.stringify({ ...body, version: matForm.version }) })
      notice.value = '物料已修改'
    } else {
      await api('/material-receipts/materials', { method: 'POST', body: JSON.stringify(body) })
      notice.value = '物料已新增'
    }
    cancelMatEdit()
    await loadMatItems()
  } catch (e) { error.value = (e as Error).message } finally { matLoading.value = false }
}
async function deleteMat(m: any) {
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  if (!window.confirm(`确定删除物料「${m.name}」？已被业务单据使用的物料不能删除。`)) return
  matLoading.value = true; error.value = ''
  try {
    await api(`/material-receipts/materials/${m.id}`, { method: 'DELETE' })
    notice.value = '物料已删除'; await loadMatItems()
  } catch (e) { error.value = (e as Error).message } finally { matLoading.value = false }
}
function changeMatPage(p: number) {
  if (p < 1 || p > Math.max(1, Math.ceil(matTotal.value / matPageSize.value))) return
  if (matDraft.value || matEditing.value) { error.value = '请先保存或取消当前编辑的物料'; return }
  matPage.value = p; void loadMatItems()
}
function onMainItemSaved() { void loadTypes() }

onMounted(() => { void loadTypes(false) })
</script>

<template>
  <div class="page-header">
    <div><h1>字典维护</h1><p>表单下拉选项的数据源：新增后约 1 分钟内在各下拉框生效（缓存自动失效）</p></div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>

  <div class="dict-layout" :class="{ 'with-material': isMaterialType }">
    <aside class="panel dict-types">
      <h3>字典类型</h3>
      <PageState :loading="!types.length && !currentType" :empty="!types.length" />
      <button v-for="t in types" :key="t.type" class="dict-type" :class="{ active: t.type === currentType }" @click="selectType(t.type)">
        <span>{{ typeName(t.type) }}</span>
        <small>{{ t.type }} · {{ t.active }}/{{ t.total }}</small>
      </button>
    </aside>

    <section class="panel dict-items">
      <div class="dict-items-head">
        <h3>{{ typeName(currentType) }}</h3>
        <div class="dict-items-actions">
          <button v-if="canWrite && !isMaterialType" class="button" :disabled="!currentType" @click="openCreate">+ 新增选项</button>
          <button v-if="canWrite && isMaterialType" class="button small" @click="addMatType">+ 添加</button>
        </div>
      </div>
      <PageState :loading="loading" :empty="!loading && !items.length" />
      <div v-if="items.length" class="table-wrap">
        <table v-if="isMaterialType">
          <thead><tr>
            <th><button type="button" class="sort-head" @click="sortMatTypesByName">名称 <span>{{ matTypeNameMark }}</span></button></th>
            <th style="width:150px">操作</th>
          </tr></thead>
          <tbody>
            <tr v-for="item in displayedMatTypes" :key="item.id" :class="{ selected: item.code === currentMatType }" @click="selectMatType(item.code)">
              <td>{{ item.name }}</td>
              <td>
                <button v-if="canWrite" class="button link" :disabled="matTypeSaving || matTypeStoredIndex(item) <= 0" @click.stop="moveMatType(item, -1)">上移</button>
                <button v-if="canWrite" class="button link" :disabled="matTypeSaving || matTypeStoredIndex(item) < 0 || matTypeStoredIndex(item) === matTypeList.length - 1" @click.stop="moveMatType(item, 1)">下移</button>
                <button v-if="canWrite" class="button link text-danger" :disabled="matTypeSaving" @click.stop="removeMatType(item)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
        <table v-else>
          <thead><tr><th>编码</th><th>名称</th><th>排序</th><th>状态</th><th>版本</th><th>操作</th></tr></thead>
          <tbody>
            <tr v-for="item in items" :key="item.id">
              <td><strong>{{ item.code }}</strong></td>
              <td>{{ item.name }}</td>
              <td>{{ item.sort_no }}</td>
              <td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td>
              <td>v{{ item.version }}</td>
              <td>
                <button v-if="canWrite" class="button link" @click="openEdit(item)">编辑</button>
                <button v-if="canWrite" class="button link text-danger" @click.stop="toggleStatus(item)">{{ item.status === 'active' ? '停用' : '启用' }}</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="isMaterialType" class="panel mat-items">
      <div class="dict-items-head">
        <h3>物料名称</h3>
        <div class="mat-head-actions">
          <div class="mat-search">
            <input v-model.trim="matKeyword" placeholder="物料名称/编码关键字" @keyup.enter="searchMat">
            <button class="button small" :disabled="!currentMatType" @click="searchMat">查找</button>
          </div>
          <button v-if="canWriteMaterial" class="button small" :disabled="!currentMatType" @click="startMatCreate">+ 添加</button>
        </div>
      </div>
      <PageState :loading="matLoading" :empty="!matLoading && !matItems.length && !matDraft" />
      <div class="table-wrap mat-table-wrap">
        <table>
          <thead><tr>
            <th><button type="button" class="sort-head" @click="sortMatBy('code')">物料编码 <span>{{ sortMark('code') }}</span></button></th>
            <th><button type="button" class="sort-head" @click="sortMatBy('name')">物料名称 <span>{{ sortMark('name') }}</span></button></th>
            <th><button type="button" class="sort-head" @click="sortMatBy('specification')">规格 <span>{{ sortMark('specification') }}</span></button></th>
            <th><button type="button" class="sort-head" @click="sortMatBy('unit')">单位 <span>{{ sortMark('unit') }}</span></button></th>
            <th><button type="button" class="sort-head" @click="sortMatBy('material_type')">物料类型 <span>{{ sortMark('material_type') }}</span></button></th>
            <th style="width:150px">操作</th>
          </tr></thead>
          <tbody>
            <tr v-for="m in matItems" :key="m.id" :class="{ 'editing-row': matEditing?.id === m.id }">
              <template v-if="matEditing?.id === m.id">
                <td><input v-model.trim="matForm.code" placeholder="留空自动生成"></td>
                <td><input v-model.trim="matForm.name" placeholder="物料名称"></td>
                <td><input v-model.trim="matForm.specification" placeholder="规格"></td>
                <td><input v-model.trim="matForm.unit" style="width:80px"></td>
                <td>{{ matTypeName(m.material_type) }}</td>
                <td><button class="button small" :disabled="matLoading" @click="saveMat">保存</button><button class="button secondary small" @click="cancelMatEdit">取消</button></td>
              </template>
              <template v-else>
                <td>{{ m.code }}</td><td>{{ m.name }}</td><td>{{ m.specification || '—' }}</td><td>{{ m.unit || '—' }}</td><td>{{ matTypeName(m.material_type) }}</td>
                <td>
                  <button v-if="canWriteMaterial" class="button link" @click="startMatEdit(m)">修改</button>
                  <button v-if="canWriteMaterial" class="button link text-danger" @click="deleteMat(m)">删除</button>
                </td>
              </template>
            </tr>
            <tr v-if="matDraft" class="draft-row">
              <td><input v-model.trim="matForm.code" placeholder="留空自动生成"></td>
              <td><input v-model.trim="matForm.name" placeholder="物料名称"></td>
              <td><input v-model.trim="matForm.specification" placeholder="规格"></td>
              <td><input v-model.trim="matForm.unit" style="width:80px"></td>
              <td>{{ matTypeName(currentMatType) }}</td>
              <td><button class="button small" :disabled="matLoading" @click="saveMat">保存</button><button class="button secondary small" @click="cancelMatEdit">取消</button></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="mat-pager">
        <span>共 {{ matTotal }} 项</span>
        <label>每页 <select v-model.number="matPageSize" @change="changeMatPageSize"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select> 条</label>
        <span>第 {{ matPage }} / {{ Math.max(1, Math.ceil(matTotal / matPageSize)) }} 页</span>
        <button class="button secondary small" :disabled="matPage <= 1 || matLoading" @click="changeMatPage(matPage - 1)">上一页</button>
        <button class="button secondary small" :disabled="matPage * matPageSize >= matTotal || matLoading" @click="changeMatPage(matPage + 1)">下一页</button>
      </div>
    </section>
  </div>

  <ModalDialog v-if="showForm" :title="editing ? `编辑选项 · ${editing.name}` : `新增选项 · ${typeName(currentType)}`" @close="showForm = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field">
        <label :class="{ required: !!editing }">编码</label>
        <input v-model.trim="form.code" :disabled="!!editing" :placeholder="editing ? '' : '留空自动生成（如 ZDX-00001）'">
      </div>
      <div class="field"><label class="required">名称</label><input v-model.trim="form.name" maxlength="60"></div>
      <div class="field"><label>排序号</label><input v-model.number="form.sort_no" inputmode="numeric"></div>
    </div>
    <template #footer>
      <button class="button secondary" @click="showForm = false">取消</button>
      <button class="button" :disabled="saving || !form.name" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.dict-layout{display:grid;grid-template-columns:220px 1fr;gap:16px;align-items:start}
.dict-layout.with-material{grid-template-columns:200px 240px 1fr}
.dict-types{padding:14px 16px}
.dict-types h3{margin:0 0 10px;font-size:15px}
.dict-type{display:flex;flex-direction:column;align-items:flex-start;gap:2px;width:100%;border:1px solid #e3e8f2;background:#fff;
  border-radius:10px;padding:10px 12px;margin-bottom:8px;cursor:pointer;font-family:inherit;text-align:left}
.dict-type small{color:#8b9ac0;font-size:12px}
.dict-type.active{border-color:#2563eb;background:#f0f6ff}
.dict-items{padding:14px 16px;font-size:13px}
.dict-items-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px}
.dict-items-head h3{margin:0;font-size:16px}
.dict-items-actions{display:flex;align-items:center;gap:6px}
.dict-items table{font-size:16px}
.dict-items tbody tr.selected{background:#f0f6ff;cursor:pointer}
.mat-items{padding:14px 16px;min-width:0;font-size:13px}
.mat-head-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.mat-search{display:flex;align-items:center;gap:6px}
.mat-search input{width:300px;max-width:300px;min-width:0}
.mat-search .button{flex:none}
.sort-head{display:inline-flex;align-items:center;gap:4px;border:0;background:none;padding:0;color:inherit;font:inherit;cursor:pointer}
.sort-head span{color:#2563eb;font-size:12px}
.mat-table-wrap{max-height:60vh;overflow:auto}
.mat-table-wrap table{min-width:640px;font-size:16px}
.mat-table-wrap td input{width:100%;min-width:0;box-sizing:border-box}
.mat-table-wrap th{position:sticky;top:0;z-index:2;background:#eaf3ff}
.draft-row{background:#fffbeb}
.editing-row{background:#f5f9ff}
.mat-pager{display:flex;align-items:center;justify-content:flex-end;gap:10px;margin-top:10px;color:#41537e;font-size:13px}
.mat-pager select{padding:4px 6px;border:1px solid #d8e0f0;border-radius:6px;background:#fff;color:#1f2d4d;font-family:inherit}
@media(max-width:1100px){.dict-layout.with-material{grid-template-columns:1fr}.dict-layout{grid-template-columns:1fr}}
</style>
