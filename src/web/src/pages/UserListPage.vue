<script setup lang="ts">
/**
 * 人员与组织管理页（设计 6.4）：筛选、列表（手机号默认脱敏）、管理人员/工人共用建档表单、
 * 换线（编辑归属）、重置密码（临时密码仅显示一次）、停用（二次确认）。
 * 级联：车间变化清空并重新加载产线；产线变化清空并重新加载班组。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { toastError, toastSuccess } from '../toast'
import ModalDialog from '../components/ModalDialog.vue'
import FilterSelect from '../components/FilterSelect.vue'

interface Person {
  id: string; employee_no: string; name: string; person_type: string
  phone_masked: string; status: string; position?: string; remark?: string; version: number
  workshop_id?: string | null; line_id?: string | null; team_id?: string | null
  workshop_name?: string | null; line_name?: string | null; team_name?: string | null
  role_names?: string | null; account_status?: string | null
}
interface OrgOption { id: string; code?: string; name: string }
interface RoleOption { id: string; name: string; code: string }
interface ProcessOption { id: string; process_code: string; name: string }

const items = ref<Person[]>([])
const loading = ref(false)
const error = ref('')
const filters = reactive({ workshop_id: '', line_id: '', person_type: '', role_id: '', status: '', keyword: '' })
const workshops = ref<OrgOption[]>([])
const lines = ref<OrgOption[]>([])
const roles = ref<RoleOption[]>([])
const processes = ref<ProcessOption[]>([])

const modalOpen = ref(false)
const saving = ref(false)
const formError = ref('')
const form = reactive({
  person_type: 'worker', employee_no: '', name: '', phone: '', password: '',
  role_ids: [] as string[], workshop_org_id: '', line_org_id: '', team_org_id: '',
  skill_process_ids: [] as string[],
  id_card_no: '', bank_name: '', bank_card: '', emergency_contact: '', emergency_phone: '',
  address: '', position: '', remark: '', as_line_manager: false,
})
const formLines = ref<OrgOption[]>([])
const formTeams = ref<OrgOption[]>([])

const bindOpen = ref(false)
const bindSaving = ref(false)
const bindTarget = ref<Person | null>(null)
const bindForm = reactive({ workshop_org_id: '', line_org_id: '', team_org_id: '', reason: '' })
const bindLines = ref<OrgOption[]>([])
const bindTeams = ref<OrgOption[]>([])

const tempPassword = ref('')
const resetTarget = ref('')

async function load() {
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams()
    for (const [k, v] of Object.entries(filters)) if (v) query.set(k, v)
    const r = await api<{ items: Person[] }>(`/people?${query}`, {}, { silent: true })
    items.value = r.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

async function loadFilters() {
  try {
    const [w, r, p] = await Promise.all([
      api<{ items: OrgOption[] }>('/workshops', {}, { silent: true }),
      api<{ items: RoleOption[] }>('/roles', {}, { silent: true }),
      api<{ items: ProcessOption[] }>('/process-definitions', {}, { silent: true }).catch(() => ({ items: [] as ProcessOption[] })),
    ])
    workshops.value = w.items || []
    roles.value = r.items || []
    processes.value = p.items || []
  } catch { /* 下拉失败不阻断列表 */ }
}

async function loadLines(workshopID: string): Promise<OrgOption[]> {
  if (!workshopID) return []
  const r = await api<{ items: OrgOption[] }>(`/lines?workshop_id=${encodeURIComponent(workshopID)}&status=active`, {}, { silent: true })
  return r.items || []
}

async function loadTeams(lineID: string): Promise<OrgOption[]> {
  if (!lineID) return []
  const r = await api<{ items: OrgOption[] }>(`/lines/${encodeURIComponent(lineID)}/teams`, {}, { silent: true })
  return r.items || []
}

async function onFilterWorkshop() {
  filters.line_id = ''
  lines.value = await loadLines(filters.workshop_id)
  await load()
}

async function onFormWorkshop() {
  form.line_org_id = ''
  form.team_org_id = ''
  formTeams.value = []
  formLines.value = await loadLines(form.workshop_org_id)
}

async function onFormLine() {
  form.team_org_id = ''
  formTeams.value = await loadTeams(form.line_org_id)
}

async function onBindWorkshop() {
  bindForm.line_org_id = ''
  bindForm.team_org_id = ''
  bindTeams.value = []
  bindLines.value = await loadLines(bindForm.workshop_org_id)
}

async function onBindLine() {
  bindForm.team_org_id = ''
  bindTeams.value = await loadTeams(bindForm.line_org_id)
}

const isWorker = computed(() => form.person_type === 'worker')

function openCreate() {
  Object.assign(form, {
    person_type: 'worker', employee_no: '', name: '', phone: '', password: '',
    role_ids: [], workshop_org_id: '', line_org_id: '', team_org_id: '',
    skill_process_ids: [], id_card_no: '', bank_name: '', bank_card: '',
    emergency_contact: '', emergency_phone: '', address: '', position: '', remark: '', as_line_manager: false,
  })
  formLines.value = []
  formTeams.value = []
  formError.value = ''
  modalOpen.value = true
}

async function save() {
  if (saving.value) return
  formError.value = ''
  if (!form.employee_no || !form.name || !form.password) { formError.value = '工号、姓名、初始密码必填'; return }
  if (form.password.length < 12) { formError.value = '初始密码至少 12 位'; return }
  saving.value = true
  try {
    await api('/people', {
      method: 'POST',
      body: JSON.stringify({
        person_type: form.person_type, employee_no: form.employee_no, name: form.name,
        phone: form.phone, password: form.password, role_ids: form.role_ids,
        workshop_org_id: form.workshop_org_id, line_org_id: form.line_org_id, team_org_id: form.team_org_id,
        skill_process_ids: isWorker.value ? form.skill_process_ids : [],
        id_card_no: form.id_card_no, bank_name: form.bank_name, bank_card: form.bank_card,
        emergency_contact: form.emergency_contact, emergency_phone: form.emergency_phone,
        address: form.address, position: form.position, remark: form.remark,
        as_line_manager: form.as_line_manager,
      }),
    })
    toastSuccess(isWorker.value ? '工人已创建（仅允许手机端登录）' : '管理人员已创建')
    modalOpen.value = false
    await load()
  } catch (e) {
    const err = e as Error & { code?: string }
    if (err.code === 'DUPLICATE_EMPLOYEE_NO') formError.value = '工号已存在，请更换'
    else if (err.code === 'DUPLICATE_PHONE') formError.value = '该手机号已被本厂其他人员使用'
    else formError.value = err.message || '创建失败，请稍后重试'
  } finally { saving.value = false }
}

async function openBind(person: Person) {
  bindTarget.value = person
  Object.assign(bindForm, {
    workshop_org_id: person.workshop_id || '', line_org_id: person.line_id || '',
    team_org_id: person.team_id || '', reason: '',
  })
  bindTeams.value = []
  bindLines.value = await loadLines(bindForm.workshop_org_id)
  bindTeams.value = await loadTeams(bindForm.line_org_id)
  bindOpen.value = true
}

async function saveBind() {
  if (bindSaving.value || !bindTarget.value) return
  bindSaving.value = true
  try {
    await api(`/people/${bindTarget.value.id}/org-binding`, {
      method: 'PUT',
      body: JSON.stringify({
        workshop_org_id: bindForm.workshop_org_id, line_org_id: bindForm.line_org_id,
        team_org_id: bindForm.team_org_id, reason: bindForm.reason, version: bindTarget.value.version,
      }),
    })
    toastSuccess('换线已生效')
    bindOpen.value = false
    await load()
  } catch (e) {
    toastError((e as Error).message || '换线失败')
  } finally { bindSaving.value = false }
}

async function resetPassword(person: Person) {
  if (!window.confirm(`确定为「${person.name}」重置密码？其全部会话将被撤销。`)) return
  try {
    const r = await api<{ temporary_password: string }>(`/people/${person.id}/reset-password`, { method: 'POST' })
    tempPassword.value = r.temporary_password
    resetTarget.value = person.name
    toastSuccess('密码已重置')
  } catch (e) {
    toastError((e as Error).message || '重置失败')
  }
}

async function disablePerson(person: Person) {
  if (!window.confirm(`确定停用「${person.name}」？账号会话将被撤销，档案保留。`)) return
  try {
    await api(`/people/${person.id}/disable`, { method: 'POST' })
    toastSuccess('已停用')
    await load()
  } catch (e) {
    toastError((e as Error).message || '停用失败')
  }
}

onMounted(() => { void load(); void loadFilters() })
</script>

<template>
  <div class="page-header">
    <div><h1>人员与组织</h1><p>管理人员与工人建档、组织归属与换线；手机号默认脱敏展示</p></div>
    <div class="header-actions"><button class="button" @click="openCreate">+ 新增人员</button></div>
  </div>

  <div class="toolbar">
    <FilterSelect v-model="filters.workshop_id" :options="[
      { value: '', label: '全部车间' }, ...workshops.map(w => ({ value: w.id, label: w.name })),
    ]" @update:model-value="onFilterWorkshop" />
    <FilterSelect v-model="filters.line_id" :options="[
      { value: '', label: '全部产线' }, ...lines.map(l => ({ value: l.id, label: l.name })),
    ]" @update:model-value="load" />
    <FilterSelect v-model="filters.person_type" :options="[
      { value: '', label: '全部类型' }, { value: 'manager', label: '管理人员' }, { value: 'worker', label: '工人' },
    ]" @update:model-value="load" />
    <FilterSelect v-model="filters.role_id" :options="[
      { value: '', label: '全部角色' }, ...roles.map(r => ({ value: r.id, label: r.name })),
    ]" @update:model-value="load" />
    <FilterSelect v-model="filters.status" :options="[
      { value: '', label: '全部状态' }, { value: 'active', label: '在职' }, { value: 'terminated', label: '离职' },
    ]" @update:model-value="load" />
    <input v-model.trim="filters.keyword" class="search-input" placeholder="工号/姓名（回车）" @keyup.enter="load">
    <button class="button secondary" @click="load">查询</button>
  </div>

  <div v-if="error" class="notice error">{{ error }}</div>
  <div class="table-wrap">
    <table>
      <thead><tr>
        <th>工号</th><th>姓名</th><th>手机号</th><th>类型</th><th>角色</th>
        <th>车间</th><th>产线</th><th>班组</th><th>在职</th><th>账号</th><th>操作</th>
      </tr></thead>
      <tbody>
        <tr v-if="loading"><td colspan="11">加载中…</td></tr>
        <tr v-else-if="!items.length"><td colspan="11">暂无人员</td></tr>
        <tr v-for="p in items" :key="p.id">
          <td><strong>{{ p.employee_no }}</strong></td>
          <td>{{ p.name }}</td>
          <td>{{ p.phone_masked || '—' }}</td>
          <td><span class="status" :class="p.person_type === 'manager' ? 'active' : 'pending'">{{ p.person_type === 'manager' ? '管理' : '工人' }}</span></td>
          <td>{{ p.role_names || '—' }}</td>
          <td>{{ p.workshop_name || '—' }}</td>
          <td>{{ p.line_name || '—' }}</td>
          <td>{{ p.team_name || '—' }}</td>
          <td>{{ p.status === 'active' ? '在职' : '离职' }}</td>
          <td><span class="status" :class="p.account_status === 'active' ? 'active' : 'pending'">{{ p.account_status === 'active' ? '正常' : (p.account_status || '无账号') }}</span></td>
          <td class="row-actions">
            <button class="button link" @click="openBind(p)">换线</button>
            <button class="button link" @click="resetPassword(p)">重置密码</button>
            <button v-if="p.status === 'active'" class="button link danger" @click="disablePerson(p)">停用</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <!-- 建档弹窗：管理人员与工人共用（设计 6.4） -->
  <ModalDialog v-if="modalOpen" wide title="新增人员" @close="modalOpen = false">
    <div class="form-grid">
      <div class="field">
        <label class="required">人员类型</label>
        <select v-model="form.person_type">
          <option value="worker">工人（仅手机端）</option>
          <option value="manager">管理人员</option>
        </select>
      </div>
      <div class="field"><label class="required">工号</label><input v-model.trim="form.employee_no" maxlength="32"></div>
      <div class="field"><label class="required">姓名</label><input v-model.trim="form.name" maxlength="30"></div>
      <div class="field"><label>手机号</label><input v-model.trim="form.phone" maxlength="11" type="tel"></div>
      <div class="field"><label class="required">初始密码（至少 12 位，仅显示一次）</label><input v-model="form.password" maxlength="64"></div>
      <div class="field"><label>岗位</label><input v-model.trim="form.position" maxlength="40"></div>

      <div class="field">
        <label class="required">所属车间</label>
        <select v-model="form.workshop_org_id" @change="onFormWorkshop">
          <option value="" disabled>请选择</option>
          <option v-for="w in workshops" :key="w.id" :value="w.id">{{ w.name }}</option>
        </select>
      </div>
      <div class="field">
        <label class="required">所属产线</label>
        <select v-model="form.line_org_id" :disabled="!formLines.length" @change="onFormLine">
          <option value="" disabled>{{ formLines.length ? '请选择' : '请先选择车间' }}</option>
          <option v-for="l in formLines" :key="l.id" :value="l.id">{{ l.name }}</option>
        </select>
      </div>
      <div class="field">
        <label>所属班组</label>
        <select v-model="form.team_org_id" :disabled="!formTeams.length">
          <option value="">{{ formTeams.length ? '不分配' : '（该产线无班组）' }}</option>
          <option v-for="t in formTeams" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </div>
      <div v-if="!isWorker" class="field">
        <label class="required">角色（可多选）</label>
        <select multiple v-model="form.role_ids" size="4">
          <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
      </div>
      <div v-if="isWorker" class="field">
        <label>角色（固定工人模板）</label>
        <select v-model="form.role_ids" multiple size="4">
          <option v-for="r in roles.filter(x => ['sewer', 'worker'].includes(x.code))" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
      </div>
      <div v-if="isWorker" class="field full">
        <label>工序专长（多选）</label>
        <div class="check-grid">
          <label v-for="p in processes" :key="p.id" class="check-item">
            <input v-model="form.skill_process_ids" type="checkbox" :value="p.id"> {{ p.name }}
          </label>
          <span v-if="!processes.length" class="text-muted">暂无工序数据</span>
        </div>
      </div>
      <div v-if="isWorker" class="field full">
        <label class="check-item"><input v-model="form.as_line_manager" type="checkbox"> 同时登记为该产线线长</label>
      </div>

      <div class="field"><label>身份证号</label><input v-model.trim="form.id_card_no" maxlength="18"></div>
      <div class="field"><label>开户行</label><input v-model.trim="form.bank_name" maxlength="60"></div>
      <div class="field"><label>银行卡号</label><input v-model.trim="form.bank_card" maxlength="32"></div>
      <div class="field"><label>紧急联系人</label><input v-model.trim="form.emergency_contact" maxlength="30"></div>
      <div class="field"><label>紧急联系电话</label><input v-model.trim="form.emergency_phone" maxlength="20"></div>
      <div class="field full"><label>现住址</label><input v-model.trim="form.address" maxlength="120"></div>
      <div class="field full"><label>备注</label><input v-model.trim="form.remark" maxlength="200"></div>
    </div>
    <div v-if="formError" class="notice error">{{ formError }}</div>
    <div class="form-actions">
      <button class="button secondary" @click="modalOpen = false">取消</button>
      <button class="button" :disabled="saving" @click="save">{{ saving ? '创建中…' : '创建' }}</button>
    </div>
  </ModalDialog>

  <!-- 换线弹窗 -->
  <ModalDialog v-if="bindOpen" :title="bindTarget ? `换线 · ${bindTarget.name}` : '换线'" @close="bindOpen = false">
    <div class="form-grid">
      <div class="field">
        <label class="required">新车间</label>
        <select v-model="bindForm.workshop_org_id" @change="onBindWorkshop">
          <option value="" disabled>请选择</option>
          <option v-for="w in workshops" :key="w.id" :value="w.id">{{ w.name }}</option>
        </select>
      </div>
      <div class="field">
        <label class="required">新产线</label>
        <select v-model="bindForm.line_org_id" :disabled="!bindLines.length" @change="onBindLine">
          <option value="" disabled>{{ bindLines.length ? '请选择' : '请先选择车间' }}</option>
          <option v-for="l in bindLines" :key="l.id" :value="l.id">{{ l.name }}</option>
        </select>
      </div>
      <div class="field">
        <label>新班组</label>
        <select v-model="bindForm.team_org_id" :disabled="!bindTeams.length">
          <option value="">不分配</option>
          <option v-for="t in bindTeams" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </div>
      <div class="field full"><label>换线原因</label><input v-model.trim="bindForm.reason" maxlength="120"></div>
    </div>
    <div class="form-actions">
      <button class="button secondary" @click="bindOpen = false">取消</button>
      <button class="button" :disabled="bindSaving || !bindForm.workshop_org_id || !bindForm.line_org_id" @click="saveBind">
        {{ bindSaving ? '提交中…' : '确认换线' }}
      </button>
    </div>
  </ModalDialog>

  <!-- 临时密码一次性展示 -->
  <ModalDialog v-if="tempPassword" :title="`临时密码 · ${resetTarget}`" @close="tempPassword = ''">
    <p>请立即通过安全渠道告知本人；关闭后不再显示：</p>
    <div class="temp-password">{{ tempPassword }}</div>
    <div class="form-actions">
      <button class="button" @click="tempPassword = ''">我已保存</button>
    </div>
  </ModalDialog>
</template>

<style scoped>
.toolbar{display:flex;gap:10px;align-items:center;margin:14px 0;flex-wrap:wrap}
.row-actions{white-space:nowrap}
.row-actions .button.link{padding:2px 8px}
.danger{color:#d92d20}
.check-grid{display:flex;flex-wrap:wrap;gap:8px 16px;max-height:120px;overflow:auto}
.check-item{display:flex;align-items:center;gap:6px;font-size:13px}
.temp-password{font-size:22px;font-weight:700;letter-spacing:2px;text-align:center;padding:14px;background:#f1f5f9;border-radius:10px;margin:10px 0}
</style>
