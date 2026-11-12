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

const TYPE_NAMES: Record<string, string> = {
  nation: '民族', position: '岗位', skill_grade: '技能评级', rate_level: '单价档位',
}
const typeName = (t: string) => TYPE_NAMES[t] || t

const types = ref<{ type: string; total: number; active: number }[]>([])
const currentType = ref('')
const items = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')

async function loadTypes(keepSelection = true) {
  try {
    const r = await api<{ items: { type: string; total: number; active: number }[] }>('/dictionaries/types')
    types.value = r.items || []
    if (!keepSelection || !types.value.some(t => t.type === currentType.value)) {
      currentType.value = types.value[0]?.type || ''
    }
    if (currentType.value) await loadItems()
  } catch (e) { error.value = (e as Error).message }
}
async function loadItems() {
  if (!currentType.value) { items.value = []; return }
  loading.value = true; error.value = ''
  try {
    const r = await api<{ items: any[] }>(`/dictionaries/${currentType.value}/items`)
    items.value = r.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function selectType(t: string) { currentType.value = t; void loadItems() }

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
    await loadTypes()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function toggleStatus(item: any) {
  error.value = ''
  try {
    await api(`/dictionaries/items/${item.id}`, { method: 'PATCH', body: JSON.stringify({ name: item.name, sort_no: item.sort_no, status: item.status === 'active' ? 'disabled' : 'active', version: item.version }) })
    notice.value = item.status === 'active' ? `已停用「${item.name}」` : `已启用「${item.name}」`
    await loadTypes()
  } catch (e) { error.value = (e as Error).message }
}

onMounted(() => { void loadTypes(false) })
</script>

<template>
  <div class="page-header">
    <div><h1>字典维护</h1><p>表单下拉选项的数据源：新增后约 1 分钟内在各下拉框生效（缓存自动失效）</p></div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>

  <div class="dict-layout">
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
        <h3>{{ typeName(currentType) }} <small class="text-muted">{{ currentType }}</small></h3>
        <button v-if="canWrite" class="button" :disabled="!currentType" @click="openCreate">+ 新增选项</button>
      </div>
      <PageState :loading="loading" :empty="!loading && !items.length" />
      <div v-if="items.length" class="table-wrap">
        <table>
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
                <button v-if="canWrite" class="button link text-danger" @click="toggleStatus(item)">{{ item.status === 'active' ? '停用' : '启用' }}</button>
              </td>
            </tr>
          </tbody>
        </table>
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
.dict-layout{display:grid;grid-template-columns:240px 1fr;gap:16px;align-items:start}
.dict-types{padding:14px 16px}
.dict-types h3{margin:0 0 10px;font-size:15px}
.dict-type{display:flex;flex-direction:column;align-items:flex-start;gap:2px;width:100%;border:1px solid #e3e8f2;background:#fff;
  border-radius:10px;padding:10px 12px;margin-bottom:8px;cursor:pointer;font-family:inherit;text-align:left}
.dict-type small{color:#8b9ac0;font-size:11px}
.dict-type.active{border-color:#2563eb;background:#f0f6ff}
.dict-items{padding:14px 16px}
.dict-items-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px}
.dict-items-head h3{margin:0;font-size:15px}
@media(max-width:860px){.dict-layout{grid-template-columns:1fr}}
</style>
