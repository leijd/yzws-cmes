<script setup lang="ts">
/**
 * 车间管理页（设计 6.2）：列表 / 新增 / 编辑 / 停用 / 查看生产线。
 * 编码冲突 409 在编码字段下提示；统计数字只读。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { toastError, toastSuccess } from '../toast'
import ModalDialog from '../components/ModalDialog.vue'
import FilterSelect from '../components/FilterSelect.vue'

interface Workshop {
  id: string; code: string; name: string; status: string
  manager_employee_id?: string | null; workshop_nature: string; outsourced_partner?: string | null
  capacity_note?: string | null; remark?: string | null; version: number
  line_count: number; employee_count: number
}
interface ManagerOption { id: string; employee_no: string; name: string }

const router = useRouter()
const items = ref<Workshop[]>([])
const loading = ref(false)
const error = ref('')
const filterName = ref('')
const filterStatus = ref('')
const managers = ref<ManagerOption[]>([])

const editing = ref<Workshop | null>(null)
const modalOpen = ref(false)
const saving = ref(false)
const codeError = ref('')
const form = reactive({
  code: '', name: '', manager_employee_id: '', workshop_nature: 'own', outsourced_partner: '',
  capacity_note: '', remark: '', version: 0,
})

const statusText: Record<string, string> = { active: '启用', disabled: '停用' }

async function load() {
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams()
    if (filterName.value.trim()) query.set('name', filterName.value.trim())
    if (filterStatus.value) query.set('status', filterStatus.value)
    const r = await api<{ items: Workshop[] }>(`/workshops?${query}`, {}, { silent: true })
    items.value = r.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

async function loadManagers() {
  try {
    const r = await api<{ items: { id: string; employee_no: string; name: string }[] }>('/people?person_type=manager&status=active', {}, { silent: true })
    managers.value = r.items || []
  } catch { managers.value = [] }
}

function openCreate() {
  editing.value = null
  Object.assign(form, { code: '', name: '', manager_employee_id: '', workshop_nature: 'own', outsourced_partner: '', capacity_note: '', remark: '', version: 0 })
  codeError.value = ''
  modalOpen.value = true
}

function openEdit(w: Workshop) {
  editing.value = w
  Object.assign(form, {
    code: w.code, name: w.name, manager_employee_id: w.manager_employee_id || '',
    workshop_nature: w.workshop_nature, outsourced_partner: w.outsourced_partner || '',
    capacity_note: w.capacity_note || '', remark: w.remark || '', version: w.version,
  })
  codeError.value = ''
  modalOpen.value = true
}

const modalTitle = computed(() => editing.value ? `编辑车间 ${editing.value.code}` : '新增车间')

async function save() {
  if (saving.value) return
  codeError.value = ''
  saving.value = true
  try {
    if (editing.value) {
      await api(`/workshops/${editing.value.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          name: form.name, manager_employee_id: form.manager_employee_id,
          workshop_nature: form.workshop_nature, outsourced_partner: form.outsourced_partner,
          capacity_note: form.capacity_note, remark: form.remark, version: form.version,
        }),
      })
      toastSuccess('车间已保存')
    } else {
      await api('/workshops', {
        method: 'POST',
        body: JSON.stringify({
          code: form.code, name: form.name, manager_employee_id: form.manager_employee_id,
          workshop_nature: form.workshop_nature, outsourced_partner: form.outsourced_partner,
          capacity_note: form.capacity_note, remark: form.remark,
        }),
      })
      toastSuccess('车间已创建')
    }
    modalOpen.value = false
    await load()
  } catch (e) {
    const err = e as Error & { code?: string }
    if (err.code === 'DUPLICATE_WORKSHOP_CODE') {
      codeError.value = '车间编码已存在，请更换'
    } else if (err.code === 'VERSION_CONFLICT') {
      toastError('车间信息已被他人变更，请刷新后重试')
    } else {
      toastError(err.message || '保存失败，请稍后重试')
    }
  } finally { saving.value = false }
}

async function disable(w: Workshop) {
  if (!window.confirm(`确定停用车间「${w.name}」？有启用产线时将被拒绝。`)) return
  try {
    await api(`/workshops/${w.id}/disable`, { method: 'POST' })
    toastSuccess('车间已停用')
    await load()
  } catch (e) {
    toastError((e as Error).message || '停用失败')
  }
}

function goLines(w: Workshop) {
  void router.push({ path: '/settings/lines', query: { workshop_id: w.id } })
}

onMounted(() => { void load(); void loadManagers() })
</script>

<template>
  <div class="page-header">
    <div><h1>车间管理</h1><p>车间建档、负责人与启停；产线必须挂在启用车间下</p></div>
    <div class="header-actions"><button class="button" @click="openCreate">+ 新增车间</button></div>
  </div>

  <div class="toolbar">
    <input v-model.trim="filterName" class="search-input" placeholder="搜索车间名称（回车）" @keyup.enter="load">
    <FilterSelect v-model="filterStatus" :options="[
      { value: '', label: '全部状态' }, { value: 'active', label: '启用' }, { value: 'disabled', label: '停用' },
    ]" @update:model-value="load" />
    <span class="spacer"></span>
    <button class="button secondary" @click="load">刷新</button>
  </div>

  <div v-if="error" class="notice error">{{ error }}</div>
  <div class="table-wrap">
    <table>
      <thead><tr>
        <th>编码</th><th>名称</th><th>负责人</th><th>产线数</th><th>人员数</th><th>性质</th><th>状态</th><th>操作</th>
      </tr></thead>
      <tbody>
        <tr v-if="loading"><td colspan="8">加载中…</td></tr>
        <tr v-else-if="!items.length"><td colspan="8">暂无车间，点击右上角新增</td></tr>
        <tr v-for="w in items" :key="w.id">
          <td><strong>{{ w.code }}</strong></td>
          <td>{{ w.name }}</td>
          <td>{{ managers.find(m => m.id === w.manager_employee_id)?.name || '—' }}</td>
          <td>{{ w.line_count }}</td>
          <td>{{ w.employee_count }}</td>
          <td>{{ w.workshop_nature === 'outsourced' ? '外包' : '自营' }}</td>
          <td><span class="status" :class="w.status">{{ statusText[w.status] || w.status }}</span></td>
          <td class="row-actions">
            <button class="button link" @click="goLines(w)">生产线</button>
            <button class="button link" @click="openEdit(w)">编辑</button>
            <button v-if="w.status === 'active'" class="button link danger" @click="disable(w)">停用</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="modalOpen" :title="modalTitle" @close="modalOpen = false">
    <div class="form-grid">
      <div v-if="!editing" class="field">
        <label class="required">车间编码</label>
        <input v-model.trim="form.code" maxlength="32" placeholder="如 WS01">
        <small v-if="codeError" class="field-error">{{ codeError }}</small>
      </div>
      <div class="field"><label class="required">车间名称</label><input v-model.trim="form.name" maxlength="60"></div>
      <div class="field">
        <label>负责人（本厂管理人员）</label>
        <select v-model="form.manager_employee_id">
          <option value="">不指定</option>
          <option v-for="m in managers" :key="m.id" :value="m.id">{{ m.name }}（{{ m.employee_no }}）</option>
        </select>
      </div>
      <div class="field">
        <label>性质</label>
        <select v-model="form.workshop_nature">
          <option value="own">自营</option>
          <option value="outsourced">外包</option>
        </select>
      </div>
      <div v-if="form.workshop_nature === 'outsourced'" class="field full">
        <label class="required">外包方名称</label><input v-model.trim="form.outsourced_partner" maxlength="60">
      </div>
      <div class="field full"><label>备注</label><input v-model.trim="form.remark" maxlength="200"></div>
    </div>
    <div class="form-actions">
      <button class="button secondary" @click="modalOpen = false">取消</button>
      <button class="button" :disabled="saving || !form.name || (!editing && !form.code)" @click="save">
        {{ saving ? '保存中…' : '保存' }}
      </button>
    </div>
  </ModalDialog>
</template>

<style scoped>
.toolbar{display:flex;gap:10px;align-items:center;margin:14px 0;flex-wrap:wrap}
.spacer{flex:1}
.row-actions{white-space:nowrap}
.row-actions .button.link{padding:2px 8px}
.field-error{color:#d92d20;font-size:12px}
.danger{color:#d92d20}
</style>
