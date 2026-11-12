<script setup lang="ts">
/**
 * 生产线管理页（设计 6.3）：列表 / 新增 / 编辑 / 停用 / 查看人员。
 * 列表以“车间编码 + 产线编码”消除跨车间重码歧义；停用展示后端拒绝原因。
 */
import { onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { toastError, toastSuccess } from '../toast'
import ModalDialog from '../components/ModalDialog.vue'
import FilterSelect from '../components/FilterSelect.vue'

interface Line {
  id: string; code: string; name: string; status: string
  workshop_id: string; workshop_code: string; workshop_name: string
  manager_employee_id?: string | null; line_nature: string
  default_process_segment?: string | null; version: number; employee_count: number
}
interface WorkshopOption { id: string; code: string; name: string }
interface Member { id: string; employee_no: string; name: string; person_type: string; position: string; status: string }

const route = useRoute()
const items = ref<Line[]>([])
const workshops = ref<WorkshopOption[]>([])
const managers = ref<{ id: string; employee_no: string; name: string }[]>([])
const loading = ref(false)
const error = ref('')
const filterWorkshop = ref(String(route.query.workshop_id || ''))
const filterStatus = ref('')

const modalOpen = ref(false)
const saving = ref(false)
const codeError = ref('')
const editing = ref<Line | null>(null)
const form = reactive({
  workshop_id: '', code: '', name: '', manager_employee_id: '', line_nature: 'fixed',
  default_process_segment: '', status: 'active', remark: '', version: 0,
})

const membersOpen = ref(false)
const membersTitle = ref('')
const members = ref<Member[]>([])
const membersError = ref('')

async function load() {
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams()
    if (filterWorkshop.value) query.set('workshop_id', filterWorkshop.value)
    if (filterStatus.value) query.set('status', filterStatus.value)
    const r = await api<{ items: Line[] }>(`/lines?${query}`, {}, { silent: true })
    items.value = r.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

async function loadWorkshops() {
  try {
    const r = await api<{ items: WorkshopOption[] }>('/workshops?status=active', {}, { silent: true })
    workshops.value = r.items || []
  } catch { workshops.value = [] }
}

async function loadManagers() {
  try {
    const r = await api<{ items: { id: string; employee_no: string; name: string }[] }>('/people?person_type=manager&status=active', {}, { silent: true })
    managers.value = r.items || []
  } catch { managers.value = [] }
}

function openCreate() {
  editing.value = null
  Object.assign(form, {
    workshop_id: filterWorkshop.value, code: '', name: '', manager_employee_id: '',
    line_nature: 'fixed', default_process_segment: '', status: 'active', remark: '', version: 0,
  })
  codeError.value = ''
  modalOpen.value = true
}

function openEdit(l: Line) {
  editing.value = l
  Object.assign(form, {
    workshop_id: l.workshop_id, code: l.code, name: l.name, manager_employee_id: l.manager_employee_id || '',
    line_nature: l.line_nature, default_process_segment: l.default_process_segment || '',
    status: l.status, remark: '', version: l.version,
  })
  codeError.value = ''
  modalOpen.value = true
}

async function save() {
  if (saving.value) return
  codeError.value = ''
  saving.value = true
  try {
    if (editing.value) {
      await api(`/lines/${editing.value.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          name: form.name, manager_employee_id: form.manager_employee_id,
          default_process_segment: form.default_process_segment, status: form.status,
          line_nature: form.line_nature, version: form.version,
        }),
      })
      toastSuccess('产线已保存')
    } else {
      await api('/lines', {
        method: 'POST',
        body: JSON.stringify({
          workshop_id: form.workshop_id, code: form.code, name: form.name,
          manager_employee_id: form.manager_employee_id, line_nature: form.line_nature,
          default_process_segment: form.default_process_segment, status: form.status,
        }),
      })
      toastSuccess('产线已创建')
    }
    modalOpen.value = false
    await load()
  } catch (e) {
    const err = e as Error & { code?: string }
    if (err.code === 'DUPLICATE_LINE_CODE') {
      const w = workshops.value.find(x => x.id === form.workshop_id)
      codeError.value = `车间「${w?.name || ''}」内产线编码已存在，请更换`
    } else if (err.code === 'VERSION_CONFLICT') {
      toastError('产线信息已被他人变更，请刷新后重试')
    } else {
      toastError(err.message || '保存失败，请稍后重试')
    }
  } finally { saving.value = false }
}

async function disable(l: Line) {
  if (!window.confirm(`确定停用产线「${l.workshop_code}/${l.code}」？仍有在职人员时将被拒绝。`)) return
  try {
    await api(`/lines/${l.id}/disable`, { method: 'POST' })
    toastSuccess('产线已停用')
    await load()
  } catch (e) {
    /* 展示后端返回的未迁移人数/占用原因（设计 6.3：不允许前端强行略过） */
    toastError((e as Error).message || '停用失败')
  }
}

async function showMembers(l: Line) {
  membersOpen.value = true
  membersTitle.value = `${l.workshop_code}/${l.code} · 当前人员`
  membersError.value = ''
  members.value = []
  try {
    const r = await api<{ items: Member[] }>(`/lines/${l.id}/members`, {}, { silent: true })
    members.value = r.items || []
  } catch (e) { membersError.value = (e as Error).message || '无权查看' }
}

onMounted(() => { void load(); void loadWorkshops(); void loadManagers() })
</script>

<template>
  <div class="page-header">
    <div><h1>生产线管理</h1><p>产线挂在启用车间下；编码仅在同车间内唯一，跨车间可同码</p></div>
    <div class="header-actions"><button class="button" @click="openCreate">+ 新增产线</button></div>
  </div>

  <div class="toolbar">
    <FilterSelect v-model="filterWorkshop" :options="[
      { value: '', label: '全部车间' },
      ...workshops.map(w => ({ value: w.id, label: `${w.code} ${w.name}` })),
    ]" @update:model-value="load" />
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
        <th>产线编码</th><th>名称</th><th>所属车间</th><th>线长</th><th>人员数</th><th>性质</th><th>状态</th><th>操作</th>
      </tr></thead>
      <tbody>
        <tr v-if="loading"><td colspan="8">加载中…</td></tr>
        <tr v-else-if="!items.length"><td colspan="8">暂无产线</td></tr>
        <tr v-for="l in items" :key="l.id">
          <td><strong>{{ l.code }}</strong></td>
          <td>{{ l.name }}</td>
          <td>{{ l.workshop_code }} · {{ l.workshop_name }}</td>
          <td>{{ managers.find(m => m.id === l.manager_employee_id)?.name || '—' }}</td>
          <td>{{ l.employee_count }}</td>
          <td>{{ l.line_nature === 'dynamic' ? '动态' : '固定' }}</td>
          <td><span class="status" :class="l.status">{{ l.status === 'active' ? '启用' : '停用' }}</span></td>
          <td class="row-actions">
            <button class="button link" @click="showMembers(l)">人员</button>
            <button class="button link" @click="openEdit(l)">编辑</button>
            <button v-if="l.status === 'active'" class="button link danger" @click="disable(l)">停用</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="modalOpen" :title="editing ? `编辑产线 ${editing.code}` : '新增产线'" @close="modalOpen = false">
    <div class="form-grid">
      <div v-if="!editing" class="field full">
        <label class="required">所属车间（须为启用状态）</label>
        <select v-model="form.workshop_id">
          <option value="" disabled>请选择车间</option>
          <option v-for="w in workshops" :key="w.id" :value="w.id">{{ w.code }} · {{ w.name }}</option>
        </select>
      </div>
      <div v-if="!editing" class="field">
        <label>产线编码</label>
        <input v-model.trim="form.code" maxlength="32" placeholder="留空自动生成（如 CX-00001）">
        <small v-if="codeError" class="field-error">{{ codeError }}</small>
      </div>
      <div class="field"><label class="required">产线名称</label><input v-model.trim="form.name" maxlength="60"></div>
      <div class="field">
        <label>线长（本厂管理人员）</label>
        <select v-model="form.manager_employee_id">
          <option value="">不指定（仅停用状态允许）</option>
          <option v-for="m in managers" :key="m.id" :value="m.id">{{ m.name }}（{{ m.employee_no }}）</option>
        </select>
      </div>
      <div v-if="editing" class="field">
        <label>状态</label>
        <select v-model="form.status">
          <option value="active">启用</option>
          <option value="disabled">停用</option>
        </select>
      </div>
      <div class="field">
        <label>产线性质</label>
        <select v-model="form.line_nature">
          <option value="fixed">固定产线</option>
          <option value="dynamic">按工单动态</option>
        </select>
      </div>
      <div class="field"><label>默认工序段</label><input v-model.trim="form.default_process_segment" maxlength="40" placeholder="如 车缝 / 后整"></div>
    </div>
    <div class="form-actions">
      <button class="button secondary" @click="modalOpen = false">取消</button>
      <button class="button" :disabled="saving || !form.name || (!editing && !form.workshop_id)" @click="save">
        {{ saving ? '保存中…' : '保存' }}
      </button>
    </div>
  </ModalDialog>

  <ModalDialog v-if="membersOpen" :title="membersTitle" @close="membersOpen = false">
    <div v-if="membersError" class="notice error">{{ membersError }}</div>
    <div v-else-if="!members.length" class="notice">暂无在职人员归属该产线</div>
    <div v-else class="table-wrap">
      <table>
        <thead><tr><th>工号</th><th>姓名</th><th>类型</th><th>岗位</th><th>状态</th></tr></thead>
        <tbody>
          <tr v-for="m in members" :key="m.id">
            <td>{{ m.employee_no }}</td><td>{{ m.name }}</td>
            <td>{{ m.person_type === 'manager' ? '管理人员' : '工人' }}</td>
            <td>{{ m.position || '—' }}</td>
            <td>{{ m.status === 'active' ? '在职' : m.status }}</td>
          </tr>
        </tbody>
      </table>
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
