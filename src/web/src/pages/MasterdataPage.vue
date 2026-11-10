<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import DatePicker from '../components/DatePicker.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has, ready } = usePermissions()
const canManage = computed(() => has('masterdata:read'))
const canWrite = computed(() => has('masterdata:write'))
const canAccountRead = computed(() => has('identity:read'))
const canAccountWrite = computed(() => has('identity:write'))
const canCreateEmployee = computed(() => canWrite.value || canAccountWrite.value)

const tab = ref<'employees' | 'customers'>('employees')
const employees = ref<any[]>([])
const customers = ref<any[]>([])
const roles = ref<any[]>([])
const processes = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const myEmployeeId = ref('')

const genderOptions = [{ value: 'male', label: '男' }, { value: 'female', label: '女' }]
const positionOptions = ['车工', '裁床师傅', '组长', '质检', '仓管', '整烫工', '生产工艺员'].map(v => ({ value: v, label: v }))
const workshopOptions = ['一车间', '二车间', '三车间', '裁床房'].map(v => ({ value: v, label: v }))
const lineOptions = ['1线', '2线', '3线', '4线', '裁床房'].map(v => ({ value: v, label: v }))
const gradeOptions = [{ value: 'A', label: 'A 熟练' }, { value: 'B', label: 'B 合格' }, { value: 'C', label: 'C 学徒' }]
const nationOptions = ['汉', '回', '壮', '苗', '其他'].map(v => ({ value: v, label: v }))
const rateOptions = [{ value: 'high', label: '高档（系数 1.25）' }, { value: 'mid', label: '中档（系数 1.0）' }, { value: 'low', label: '低档（系数 0.85）' }]
const modeOptions = [{ value: 'piecework', label: '计件' }, { value: 'monthly', label: '月薪' }]
const statusOptions = [{ value: 'active', label: '在职' }, { value: 'disabled', label: '离职' }]
const accountStatusOptions = [{ value: 'active', label: '启用' }, { value: 'disabled', label: '停用' }]

const blank = () => ({ code: '', name: '', gender: 'male', birth_date: '', phone: '', id_card_no: '', nation: '汉', native_place: '', address: '', hire_date: '', position: '', workshop: '', production_line: '', skill_grade: 'B', pay_mode: 'piecework', base_salary: '0', rate_level: 'mid', bank_card: '', bank_name: '', emergency_contact: '', emergency_phone: '', status: 'active', remark: '', skill_process_ids: [] as string[] })
const form = reactive(blank())
const fail = () => { error.value = '当前账号没有修改该字段的权限'; saving.value = false }

// 本人自助视图：表单展示全部信息，仅以下 6 项可修改。
const isSelfView = computed(() => ready.value && !canManage.value && !canAccountRead.value && !canAccountWrite.value && !canWrite.value)
const selfProfile = ref<any>(null)
const selfForm = reactive({ phone: '', address: '', emergency_contact: '', emergency_phone: '', native_place: '', nation: '汉' })
const selfAge = computed(() => {
  const birthDate = selfProfile.value?.birth_date
  if (!birthDate) return '—'
  const birth = new Date(birthDate)
  if (Number.isNaN(birth.getTime())) return '—'
  const now = new Date()
  let a = now.getFullYear() - birth.getFullYear()
  if (now.getMonth() < birth.getMonth() || (now.getMonth() === birth.getMonth() && now.getDate() < birth.getDate())) a--
  return a + ' 岁'
})
const selfGrade = computed(() => gradeOptions.find(o => o.value === selfProfile.value?.skill_grade)?.label || selfProfile.value?.skill_grade || '—')
const selfRate = computed(() => rateOptions.find(o => o.value === selfProfile.value?.rate_level)?.label || selfProfile.value?.rate_level || '—')

const age = computed(() => {
  if (!form.birth_date) return '—'
  const birth = new Date(form.birth_date)
  if (Number.isNaN(birth.getTime())) return '—'
  const now = new Date()
  let a = now.getFullYear() - birth.getFullYear()
  if (now.getMonth() < birth.getMonth() || (now.getMonth() === birth.getMonth() && now.getDate() < birth.getDate())) a--
  return a + ' 岁'
})

const showForm = ref(false)
const editing = ref<any>(null)
const detail = ref<any>(null)
const accountNotice = ref('')
const accountPassword = ref('')
const accountStatus = ref('active')
const selectedRoles = reactive<Record<string, boolean>>({})
const isSelf = computed(() => !!editing.value && editing.value.id === myEmployeeId.value)
const canEditEmployee = computed(() => canWrite.value || canAccountWrite.value)
const account = computed(() => detail.value?.account || null)

const showPassword = ref(false)
const passwordForm = reactive({ old_password: '', new_password: '', confirm: '' })

async function load() {
  loading.value = true; error.value = ''
  try {
    if (tab.value === 'employees') employees.value = (await api<{ items: any[] }>('/employees')).items
    else customers.value = (await api<{ items: any[] }>('/customers')).items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function loadSelfProfile() {
  loading.value = true; error.value = ''
  try {
    const list = (await api<{ items: any[] }>('/employees')).items
    if (!list.length) { selfProfile.value = null; return }
    const data = await api<any>(`/employees/${list[0].id}`)
    selfProfile.value = data
    Object.assign(selfForm, {
      phone: data.phone || '',
      address: data.address || '',
      emergency_contact: data.emergency_contact || '',
      emergency_phone: data.emergency_phone || '',
      native_place: data.native_place || '',
      nation: data.nation || '汉',
    })
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function saveSelf() {
  if (!selfProfile.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    await api(`/employees/${selfProfile.value.id}`, { method: 'PATCH', body: JSON.stringify({ ...selfForm, version: selfProfile.value.version }) })
    notice.value = '我的资料已保存'
    await loadSelfProfile()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function loadRoles() {
  if (!canAccountRead.value) { roles.value = []; return }
  try { roles.value = (await api<{ items: any[] }>('/roles')).items } catch { roles.value = [] }
}
async function loadProcesses() {
  if (!has('process:read')) { processes.value = []; return }
  try { processes.value = (await api<{ items: any[] }>('/process-definitions')).items } catch { processes.value = [] }
}
function changeTab(value: typeof tab.value) { tab.value = value; load() }
function clearSelected() { Object.keys(selectedRoles).forEach(key => delete selectedRoles[key]) }
function resetAccountState() {
  accountNotice.value = ''; accountPassword.value = ''; accountStatus.value = 'active'; clearSelected()
}
async function openCreate() {
  editing.value = null; detail.value = null
  Object.assign(form, blank())
  resetAccountState()
  error.value = ''
  showForm.value = true
  loadProcesses()
  try {
    const next = await api<{ employee_no: string }>('/employees/next-no', {}, { silent: true })
    if (!form.code) form.code = next.employee_no
  } catch { /* 由服务端兜底生成 */ }
}
async function openEdit(item: any) {
  editing.value = item; detail.value = null
  resetAccountState()
  error.value = ''
  const self = item.id === myEmployeeId.value
  try {
    const data = await api<any>(`/employees/${item.id}`)
    detail.value = data
    const next: Record<string, any> = blank()
    const map: Record<string, string> = { code: 'employee_no', name: 'name', gender: 'gender', birth_date: 'birth_date', nation: 'nation', native_place: 'native_place', address: 'address', hire_date: 'hire_date', position: 'position', workshop: 'workshop', production_line: 'production_line', skill_grade: 'skill_grade', pay_mode: 'pay_mode', base_salary: 'base_salary', rate_level: 'rate_level', emergency_contact: 'emergency_contact', emergency_phone: 'emergency_phone', remark: 'remark' }
    for (const [key, col] of Object.entries(map)) {
      const v = data[col]
      if (v !== null && v !== undefined) next[key] = String(v)
    }
    if (self) {
      for (const key of ['phone', 'id_card_no', 'bank_card', 'bank_name']) {
        const v = data[key]
        if (v !== null && v !== undefined) next[key] = String(v)
      }
    }
    next.skill_process_ids = data.skill_process_ids || []
    Object.assign(form, next)
    if (data.account) {
      accountStatus.value = data.account.status
      const codes: string[] = data.account.roles || []
      for (const role of roles.value) if (codes.includes(role.code)) selectedRoles[role.id] = true
    }
    showForm.value = true
    if (canEditEmployee.value) loadProcesses()
  } catch (e) { error.value = (e as Error).message }
}
function buildPayload() {
  const payload: Record<string, any> = { ...form }
  payload.employee_no = form.code
  delete payload.code
  payload.base_salary = Number(form.base_salary) || 0
  payload.skill_process_ids = form.skill_process_ids
  if (!isSelf.value) {
    for (const key of ['phone', 'id_card_no', 'bank_card', 'bank_name']) if (!payload[key]) delete payload[key]
  }
  return payload
}
async function save() {
  if (!canEditEmployee.value) return fail()
  if (!form.name.trim() || (editing.value && !form.code.trim())) { error.value = '姓名为必填项，编辑时工号不能为空'; return }
  saving.value = true; error.value = ''; notice.value = ''
  try {
    if (editing.value) {
      await api(`/employees/${editing.value.id}`, { method: 'PATCH', body: JSON.stringify({ ...buildPayload(), version: editing.value.version }) })
      notice.value = '员工资料已保存'
      showForm.value = false
      await load()
    } else {
      const created = await api<{ id: string }>('/employees', { method: 'POST', body: JSON.stringify(buildPayload()) })
      editing.value = { id: created.id, version: 1 }
      await load()
      if (canAccountWrite.value) {
        await createAccount(created.id)
      } else {
        notice.value = '员工已创建；账号需由租户管理员开通'
        showForm.value = false
      }
    }
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function createAccount(employeeID?: string) {
  const id = employeeID || editing.value?.id
  if (!id) return
  saving.value = true; error.value = ''
  try {
    const roles = Object.keys(selectedRoles).filter(key => selectedRoles[key])
    const result = await api<{ initial_password?: string; login_name: string }>(`/employees/${id}/account`, { method: 'POST', body: JSON.stringify({ password: accountPassword.value, role_ids: roles }) })
    accountPassword.value = ''
    accountNotice.value = result.initial_password ? `账号已开通（登录名 ${result.login_name}），初始密码：${result.initial_password}` : `账号已开通（登录名 ${result.login_name}）`
    detail.value = await api(`/employees/${id}`)
    accountStatus.value = detail.value.account?.status || 'active'
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function resetPassword() {
  if (!editing.value || !account.value) return
  saving.value = true; error.value = ''
  try {
    const result = await api<{ initial_password?: string }>(`/employees/${editing.value.id}/account/reset-password`, { method: 'POST', body: JSON.stringify({ password: accountPassword.value }) })
    accountPassword.value = ''
    accountNotice.value = result.initial_password ? `密码已重置，新密码：${result.initial_password}` : '密码已重置'
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function saveAccountSettings() {
  if (!account.value) return
  saving.value = true; error.value = ''
  try {
    const roleIDs = Object.keys(selectedRoles).filter(key => selectedRoles[key])
    await api(`/users/${account.value.id}/roles`, { method: 'PUT', body: JSON.stringify({ role_ids: roleIDs, status: accountStatus.value }) })
    accountNotice.value = '账号角色与状态已保存'
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function copyPassword() {
  const password = accountNotice.value.split('：').pop() || ''
  try { await navigator.clipboard.writeText(password) } catch { /* ignore */ }
}
function openPassword() { passwordForm.old_password = ''; passwordForm.new_password = ''; passwordForm.confirm = ''; error.value = ''; showPassword.value = true }
async function changePassword() {
  if (passwordForm.new_password.length < 12) { error.value = '新密码至少 12 位'; return }
  if (passwordForm.new_password !== passwordForm.confirm) { error.value = '两次输入的新密码不一致'; return }
  saving.value = true; error.value = ''
  try {
    await api('/auth/password/change', { method: 'POST', body: JSON.stringify({ old_password: passwordForm.old_password, new_password: passwordForm.new_password }) })
    showPassword.value = false
    notice.value = '密码已修改'
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

const customerForm = reactive({ code: '', name: '', contact_name: '', phone: '', address: '', remark: '' })
const showCustomer = ref(false)
function openCustomer() { Object.assign(customerForm, { code: '', name: '', contact_name: '', phone: '', address: '', remark: '' }); error.value = ''; showCustomer.value = true }
async function saveCustomer() {
  if (!customerForm.code.trim() || !customerForm.name.trim()) { error.value = '客户编码和名称必填'; return }
  saving.value = true; error.value = ''
  try {
    await api('/customers', { method: 'POST', body: JSON.stringify(customerForm) })
    showCustomer.value = false
    notice.value = '客户已创建'
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

onMounted(async () => {
  try {
    const me = await api<{ user: { employee_id?: string } }>('/me')
    myEmployeeId.value = me.user.employee_id || ''
  } catch { /* ignore */ }
})
watch(ready, loaded => {
  if (!loaded) return
  loadRoles()
  if (isSelfView.value) loadSelfProfile()
  else load()
}, { immediate: true })
</script>

<template>
  <div class="page-header"><div><h1>员工与账号</h1><p>员工档案与登录账号一体化维护</p></div><div class="header-actions"><button class="button secondary" @click="openPassword">修改密码</button><button v-if="!isSelfView && (tab === 'employees' ? canCreateEmployee : canWrite)" class="button" @click="tab === 'employees' ? openCreate() : openCustomer()">+ 新增{{ tab === 'employees' ? '员工' : '客户' }}</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section v-if="!isSelfView" class="panel"><div class="tabs"><button v-if="canManage" :class="{ active: tab === 'employees' }" @click="changeTab('employees')">员工与账号</button><button v-if="canManage" :class="{ active: tab === 'customers' }" @click="changeTab('customers')">客户资料</button></div>
    <div class="toolbar"><input class="search-input" placeholder="搜索编码或名称"><span class="spacer"></span><button class="button secondary" @click="load">刷新</button></div>
    <PageState :loading="loading" />
    <div v-if="!loading && tab === 'employees' && employees.length" class="table-wrap"><table><thead><tr><th>工号</th><th>姓名</th><th>岗位</th><th>车间/产线</th><th>账号</th><th v-if="canAccountRead">角色</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in employees" :key="item.id"><td><strong>{{ item.code }}</strong></td><td>{{ item.name }}</td><td>{{ item.position || '—' }}</td><td>{{ [item.workshop, item.production_line].filter(Boolean).join(' / ') || '—' }}</td><td><span v-if="item.account">{{ item.account.login_name }}（{{ item.account.status === 'active' ? '启用' : '停用' }}）</span><span v-else class="status pending">未开通</span></td><td v-if="canAccountRead">{{ item.account?.roles?.join('、') || '—' }}</td><td><span class="status" :class="item.status">{{ item.status === 'active' ? '在职' : '离职' }}</span></td><td><button class="button link" @click="openEdit(item)">{{ item.id === myEmployeeId ? '编辑我的资料' : (canEditEmployee ? '编辑' : '查看') }}</button></td></tr></tbody></table></div>
    <PageState v-if="!loading && tab === 'employees'" :empty="!employees.length" />
    <div v-if="!loading && tab === 'customers' && customers.length" class="table-wrap"><table><thead><tr><th>客户编码</th><th>名称</th><th>状态</th><th>版本</th></tr></thead><tbody><tr v-for="item in customers" :key="item.id"><td>{{ item.code }}</td><td>{{ item.name }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td>v{{ item.version }}</td></tr></tbody></table></div>
    <PageState v-if="!loading && tab === 'customers'" :empty="!customers.length" />
  </section>

  <section v-if="isSelfView" class="panel">
    <div class="panel-header"><h2>我的员工资料</h2><span class="text-muted">联系电话、现住址、紧急联系人、紧急联系电话、籍贯、民族可自行修改</span></div>
    <div class="panel-body">
      <PageState :loading="loading" />
      <div v-if="!loading && !selfProfile" class="notice">当前账号未关联员工档案，请联系租户管理员。</div>
      <div v-if="selfProfile" class="form-grid">
        <div class="field"><label>工号</label><input :value="selfProfile.employee_no" disabled></div>
        <div class="field"><label>姓名</label><input :value="selfProfile.name" disabled></div>
        <div class="field"><label>性别</label><input :value="selfProfile.gender === 'male' ? '男' : selfProfile.gender === 'female' ? '女' : '—'" disabled></div>
        <div class="field"><label>出生日期</label><input :value="selfProfile.birth_date || '—'" disabled></div>
        <div class="field"><label>年龄</label><input :value="selfAge" disabled></div>
        <div class="field"><label>联系电话</label><input v-model.trim="selfForm.phone" placeholder="11 位手机号"></div>
        <div class="field"><label>身份证号</label><input :value="selfProfile.id_card_no || '—'" disabled></div>
        <div class="field"><label>民族</label><FilterSelect v-model="selfForm.nation" :options="nationOptions"/></div>
        <div class="field"><label>籍贯</label><input v-model.trim="selfForm.native_place"></div>
        <div class="field"><label>入职日期</label><input :value="selfProfile.hire_date || '—'" disabled></div>
        <div class="field"><label>岗位</label><input :value="selfProfile.position || '—'" disabled></div>
        <div class="field"><label>责任车间</label><input :value="selfProfile.workshop || '—'" disabled></div>
        <div class="field"><label>生产线</label><input :value="selfProfile.production_line || '—'" disabled></div>
        <div class="field"><label>技能评级</label><input :value="selfGrade" disabled></div>
        <div class="field"><label>工资模式</label><input :value="selfProfile.pay_mode === 'monthly' ? '月薪' : selfProfile.pay_mode === 'piecework' ? '计件' : '—'" disabled></div>
        <div class="field"><label>基础底薪(元)</label><input :value="selfProfile.base_salary ?? '—'" disabled></div>
        <div class="field"><label>单价档位</label><input :value="selfRate" disabled></div>
        <div class="field"><label>银行卡号</label><input :value="selfProfile.bank_card || '—'" disabled></div>
        <div class="field"><label>开户行</label><input :value="selfProfile.bank_name || '—'" disabled></div>
        <div class="field"><label>紧急联系人</label><input v-model.trim="selfForm.emergency_contact"></div>
        <div class="field"><label>紧急联系电话</label><input v-model.trim="selfForm.emergency_phone"></div>
        <div class="field"><label>在职状态</label><input :value="selfProfile.status === 'active' ? '在职' : '离职'" disabled></div>
        <div class="field"><label>登录账号</label><input :value="selfProfile.account?.login_name || '未开通'" disabled></div>
        <div class="field"><label>账号状态</label><input :value="selfProfile.account ? (selfProfile.account.status === 'active' ? '启用' : '停用') : '—'" disabled></div>
        <div class="field full"><label>现住址</label><textarea v-model="selfForm.address" rows="2"></textarea></div>
        <div class="field full"><label>备注</label><textarea :value="selfProfile.remark || ''" rows="2" disabled></textarea></div>
      </div>
      <div v-if="selfProfile" class="form-actions"><button class="button" :disabled="saving" @click="saveSelf">保存我的资料</button></div>
    </div>
  </section>

  <ModalDialog v-if="showForm" wide :title="editing ? `员工 · ${form.name || form.code}` : '新增员工'" @close="showForm = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div v-if="accountNotice" class="notice success" style="display:flex;align-items:center;justify-content:space-between;gap:10px"><span>{{ accountNotice }}（关闭后不再显示）</span><button class="button secondary small" @click="copyPassword">复制</button></div>
    <div class="form-grid">
      <template v-if="canEditEmployee">
        <div class="field"><label class="required">工号</label><input v-model.trim="form.code" placeholder="如 C001" :disabled="!!editing && !canAccountWrite"></div>
        <div class="field"><label class="required">姓名</label><input v-model.trim="form.name" :disabled="!!editing && !canAccountWrite"></div>
        <div class="field"><label>性别</label><div style="display:flex;gap:18px;align-items:center;height:38px"><label v-for="o in genderOptions" :key="o.value"><input v-model="form.gender" type="radio" :value="o.value"> {{ o.label }}</label></div></div>
        <div class="field"><label>出生日期</label><DatePicker v-model="form.birth_date"/></div>
        <div class="field"><label>年龄（自动计算）</label><input :value="age" disabled></div>
        <div class="field"><label>联系电话</label><input v-model.trim="form.phone" :placeholder="editing && !isSelf ? '已设置，留空则不修改' : '11 位手机号'"></div>
        <div class="field"><label>身份证号</label><input v-model.trim="form.id_card_no" :placeholder="editing && !isSelf ? '已设置，留空则不修改' : '18 位'"></div>
        <div class="field"><label>民族</label><FilterSelect v-model="form.nation" :options="nationOptions"/></div>
        <div class="field"><label>籍贯</label><input v-model.trim="form.native_place" placeholder="如 广西贵港"></div>
        <div class="field"><label>入职日期</label><DatePicker v-model="form.hire_date"/></div>
        <div class="field"><label>岗位</label><FilterSelect v-model="form.position" :options="positionOptions" placeholder="请选择"/></div>
        <div class="field"><label>责任车间</label><FilterSelect v-model="form.workshop" :options="workshopOptions" placeholder="请选择"/></div>
        <div class="field"><label>生产线</label><FilterSelect v-model="form.production_line" :options="lineOptions" placeholder="请选择"/></div>
        <div class="field"><label>技能评级</label><FilterSelect v-model="form.skill_grade" :options="gradeOptions"/></div>
        <div class="field"><label>工资模式</label><div style="display:flex;gap:18px;align-items:center;height:38px"><label v-for="o in modeOptions" :key="o.value"><input v-model="form.pay_mode" type="radio" :value="o.value"> {{ o.label }}</label></div></div>
        <div class="field"><label>基础底薪(元)</label><input v-model="form.base_salary" inputmode="decimal" placeholder="计件员工填 0"></div>
        <div class="field"><label>单价档位</label><FilterSelect v-model="form.rate_level" :options="rateOptions"/></div>
        <div class="field"><label>银行卡号</label><input v-model.trim="form.bank_card" :placeholder="editing && !isSelf ? '已设置，留空则不修改' : '发薪使用'"></div>
        <div class="field"><label>开户行</label><input v-model.trim="form.bank_name" :placeholder="editing && !isSelf ? '已设置，留空则不修改' : '如 工行'"></div>
        <div class="field"><label>紧急联系人</label><input v-model.trim="form.emergency_contact" placeholder="姓名及关系"></div>
        <div class="field"><label>紧急联系电话</label><input v-model.trim="form.emergency_phone"></div>
        <div class="field"><label>在职状态</label><FilterSelect v-model="form.status" :options="statusOptions"/></div>
        <div class="field full"><label>现住址</label><textarea v-model="form.address" rows="2"></textarea></div>
        <div class="field full"><label>工序专长（多选）</label><div v-if="processes.length" style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;max-height:160px;overflow:auto;border:1px solid var(--border);border-radius:6px;padding:8px"><label v-for="p in processes" :key="p.id" style="font-size:13px"><input type="checkbox" :value="p.id" :checked="form.skill_process_ids.includes(p.id)" @change="form.skill_process_ids = form.skill_process_ids.includes(p.id) ? form.skill_process_ids.filter((x: string) => x !== p.id) : [...form.skill_process_ids, p.id]"> {{ p.name }}</label></div><small v-else class="text-muted">工序库为空或无读取权限，可后补</small></div>
        <div class="field full"><label>备注</label><textarea v-model="form.remark" rows="2" placeholder="如 熟练工，6年夹克经验"></textarea></div>
      </template>
      <template v-else>
        <div class="field"><label>工号</label><input :value="form.code" disabled></div>
        <div class="field"><label>姓名</label><input :value="form.name" disabled></div>
        <div class="field full"><div class="notice" style="margin:0">当前角色只能查看该员工，修改请联系厂长或租户管理员。</div></div>
      </template>
    </div>

    <template v-if="canAccountRead">
      <h3 style="margin-top:22px">登录账号</h3>
      <div v-if="!account && editing" class="form-grid">
        <div v-if="canAccountWrite" class="field"><label>初始密码</label><input v-model="accountPassword" type="text" autocomplete="off" placeholder="留空自动生成 12 位"></div>
        <div v-else class="field full"><div class="notice" style="margin:0">该员工尚未开通账号，需由租户管理员开通。</div></div>
      </div>
      <div v-else-if="!account && !editing && canAccountWrite" class="form-grid">
        <div class="field"><label>初始密码</label><input v-model="accountPassword" type="text" autocomplete="off" placeholder="留空自动生成 12 位"></div>
      </div>
      <div v-else-if="account" class="form-grid">
        <div class="field"><label>登录名</label><input :value="account.login_name" disabled></div>
        <div v-if="canAccountWrite" class="field"><label>账号状态</label><FilterSelect v-model="accountStatus" :options="accountStatusOptions"/></div>
        <div v-if="canAccountWrite" class="field"><label>重置密码</label><input v-model="accountPassword" type="text" autocomplete="off" placeholder="留空自动生成 12 位"></div>
      </div>
      <div v-if="canAccountWrite && (!account || canAccountRead)" style="margin-top:12px">
        <label v-for="role in roles" :key="role.id" style="display:inline-flex;align-items:center;gap:6px;margin:0 16px 8px 0"><input v-model="selectedRoles[role.id]" type="checkbox" :disabled="role.is_system"> {{ role.name }}</label>
      </div>
      <div v-if="canAccountWrite" class="form-actions" style="justify-content:flex-start;margin-top:6px">
        <button v-if="editing && !account" class="button secondary" :disabled="saving" @click="createAccount()">开通账号</button>
        <template v-if="account">
          <button class="button secondary" :disabled="saving" @click="saveAccountSettings">保存角色与状态</button>
          <button class="button secondary" :disabled="saving" @click="resetPassword">重置密码</button>
        </template>
      </div>
      <div class="notice" style="margin-top:12px">账号登录名默认等于工号；初始密码仅在生成时显示一次，请转告本人并提醒尽快修改。</div>
    </template>

    <template #footer><button class="button secondary" @click="showForm = false">取消</button><button v-if="canEditEmployee" class="button" :disabled="saving || (!!editing && !form.code) || !form.name" @click="save">{{ editing ? '保存' : (canAccountWrite ? '创建员工并开通账号' : '创建员工') }}</button></template>
  </ModalDialog>

  <ModalDialog v-if="showCustomer" :title="'新增客户'" @close="showCustomer = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label class="required">客户编码</label><input v-model.trim="customerForm.code"></div>
      <div class="field"><label class="required">客户名称</label><input v-model.trim="customerForm.name"></div>
      <div class="field"><label>联系人</label><input v-model.trim="customerForm.contact_name"></div>
      <div class="field"><label>联系电话</label><input v-model.trim="customerForm.phone"></div>
      <div class="field full"><label>地址</label><input v-model.trim="customerForm.address"></div>
      <div class="field full"><label>备注</label><textarea v-model="customerForm.remark" rows="2"></textarea></div>
    </div>
    <template #footer><button class="button secondary" @click="showCustomer = false">取消</button><button class="button" :disabled="saving || !customerForm.code || !customerForm.name" @click="saveCustomer">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="showPassword" :title="'修改密码'" @close="showPassword = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field full"><label class="required">当前密码</label><input v-model="passwordForm.old_password" type="password" autocomplete="current-password"></div>
      <div class="field"><label class="required">新密码</label><input v-model="passwordForm.new_password" type="password" autocomplete="new-password" placeholder="至少 12 位"></div>
      <div class="field"><label class="required">确认新密码</label><input v-model="passwordForm.confirm" type="password" autocomplete="new-password"></div>
    </div>
    <template #footer><button class="button secondary" @click="showPassword = false">取消</button><button class="button" :disabled="saving || passwordForm.new_password.length < 12" @click="changePassword">保存</button></template>
  </ModalDialog>
</template>
