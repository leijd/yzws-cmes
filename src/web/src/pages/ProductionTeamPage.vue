<script setup lang="ts">
/**
 * 生产组管理：把人员配置到车间 → 生产线 → 班组。
 * 原「人员与组织」的建档/账号/验证方式等内容已合并到「员工信息」模块（MasterdataPage 的人员与组织页签）。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import { toastError, toastSuccess } from '../toast'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

interface Person {
  id: string; employee_no: string; name: string; person_type: string; status: string; version: number
  workshop_name?: string | null; line_name?: string | null; team_name?: string | null
}
interface OrgOption { id: string; code?: string; name: string; status?: string }

const { has } = usePermissions()
/* 本页面完全由「组织资源」权限控制：查看=team:read（菜单可见性），配置组员=team:write（维护生产组），
 * 不再依赖「管理用户角色权限」(identity:*)——该权限仅用于「用户角色权限」页。 */
const canWrite = computed(() => has('team:write'))

const items = ref<Person[]>([])
const workshops = ref<OrgOption[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const filters = reactive({ workshop_id: '', line_id: '', status: 'active', keyword: '' })
const lines = ref<OrgOption[]>([])
const selected = ref<string[]>([])

const modalOpen = ref(false)
const modalTargets = ref<Person[]>([])
const form = reactive({ workshop_org_id: '', line_org_id: '', team_org_id: '', reason: '' })
const formLines = ref<OrgOption[]>([])
const formTeams = ref<OrgOption[]>([])

const workshopOptions = computed(() => workshops.value.filter(w => w.status !== 'disabled').map(w => ({ value: w.id, label: w.name })))
const lineOptions = computed(() => lines.value.map(l => ({ value: l.id, label: l.name })))
const formLineOptions = computed(() => formLines.value.map(l => ({ value: l.id, label: l.name })))
const teamOptions = computed(() => formTeams.value.map(t => ({ value: t.id, label: t.name })))
const allSelected = computed(() => items.value.length > 0 && items.value.every(p => selected.value.includes(p.id)))

async function load() {
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams()
    for (const [k, v] of Object.entries(filters)) if (v) query.set(k, v)
    const r = await api<{ items: Person[] }>(`/people?${query}`, {}, { silent: true })
    items.value = r.items || []
    selected.value = selected.value.filter(id => items.value.some(p => p.id === id))
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function loadWorkshops() {
  try { workshops.value = (await api<{ items: OrgOption[] }>('/workshops', {}, { silent: true })).items || [] }
  catch { workshops.value = [] }
}
async function loadLines(workshopID: string): Promise<OrgOption[]> {
  if (!workshopID) return []
  try { return (await api<{ items: OrgOption[] }>(`/lines?workshop_id=${encodeURIComponent(workshopID)}&status=active`, {}, { silent: true })).items || [] }
  catch { return [] }
}
async function loadTeams(lineID: string): Promise<OrgOption[]> {
  if (!lineID) return []
  try { return (await api<{ items: OrgOption[] }>(`/lines/${encodeURIComponent(lineID)}/teams`, {}, { silent: true })).items || [] }
  catch { return [] }
}
async function onFilterWorkshop() {
  filters.line_id = ''
  lines.value = await loadLines(filters.workshop_id)
  await load()
}
function toggleAll(ev: Event) {
  selected.value = (ev.target as HTMLInputElement).checked ? items.value.map(p => p.id) : []
}
function toggleOne(id: string, ev: Event) {
  const checked = (ev.target as HTMLInputElement).checked
  selected.value = checked ? [...new Set([...selected.value, id])] : selected.value.filter(x => x !== id)
}

function openAssign(people: Person[]) {
  if (!people.length) { error.value = '请先勾选人员'; return }
  if (!canWrite.value) { error.value = '需要「维护生产组」权限才能配置组员'; return }
  error.value = ''
  modalTargets.value = people
  Object.assign(form, { workshop_org_id: '', line_org_id: '', team_org_id: '', reason: '' })
  formLines.value = []
  formTeams.value = []
  modalOpen.value = true
}
function openSingle(person: Person) { openAssign([person]) }
function openBatch() {
  openAssign(items.value.filter(p => selected.value.includes(p.id)))
}
async function onFormWorkshop() {
  form.line_org_id = ''; form.team_org_id = ''
  formTeams.value = []
  formLines.value = await loadLines(form.workshop_org_id)
}
async function onFormLine() {
  form.team_org_id = ''
  formTeams.value = await loadTeams(form.line_org_id)
}
async function saveAssign() {
  if (!form.workshop_org_id || !form.line_org_id) { error.value = '请选择车间与生产线'; return }
  saving.value = true; error.value = ''
  const failures: string[] = []
  for (const person of modalTargets.value) {
    try {
      await api(`/people/${person.id}/org-binding`, {
        method: 'PUT',
        body: JSON.stringify({
          workshop_org_id: form.workshop_org_id, line_org_id: form.line_org_id,
          team_org_id: form.team_org_id, reason: form.reason || '生产组员配置', version: person.version,
        }),
      })
    } catch (e) { failures.push(`${person.name}：${(e as Error).message}`) }
  }
  saving.value = false
  const successCount = modalTargets.value.length - failures.length
  if (failures.length) {
    error.value = failures.join('；')
    toastError(`成功 ${successCount} 人，失败 ${failures.length} 人`)
  } else {
    modalOpen.value = false
    selected.value = []
    notice.value = `已配置 ${successCount} 人到生产线`
    toastSuccess(notice.value)
  }
  await load()
}

/* ===== 位置视角（现场调人）：先逐级选定 车间→产线→班组，再从全厂在职人员中挑选加入 =====
 * 与人员视角（勾人→选位置）互补：班组长/主管按"这条线缺谁"的思路操作。 */
const view = ref<'people' | 'position' | 'teams'>('people')
const posWorkshopID = ref('')
const posLines = ref<OrgOption[]>([])
const posLineID = ref('')
const posTeams = ref<OrgOption[]>([])
const posTeamID = ref('')
const candidates = ref<Person[]>([])
const picked = ref<string[]>([])
const posSaving = ref(false)

const posLineOptions = computed(() => posLines.value.map(l => ({ value: l.id, label: l.name })))
const posTeamOptions = computed(() => [{ value: '', label: '不指定班组' }, ...posTeams.value.map(t => ({ value: t.id, label: t.name }))])
const pickedPeople = computed(() => candidates.value.filter(p => picked.value.includes(p.id)))
/* 目标位置当前已有人员（按归属列名匹配，只读参考；班组为空表示整条产线） */
const targetMembers = computed(() => {
  const ln = posLines.value.find(l => l.id === posLineID.value)?.name
  if (!ln) return []
  const tn = posTeamID.value ? posTeams.value.find(t => t.id === posTeamID.value)?.name : null
  return candidates.value.filter(p => p.line_name === ln && (tn === null || p.team_name === tn || (!p.team_name && !tn)))
})

async function loadCandidates() {
  try { candidates.value = (await api<{ items: Person[] }>('/people?status=active', {}, { silent: true })).items || [] }
  catch { candidates.value = [] }
}
async function onPosWorkshop() {
  posLineID.value = ''; posTeamID.value = ''
  posTeams.value = []
  posLines.value = posWorkshopID.value ? await loadLines(posWorkshopID.value) : []
}
async function onPosLine() {
  posTeamID.value = ''
  posTeams.value = posLineID.value ? await loadTeams(posLineID.value) : []
}
function togglePick(id: string) {
  picked.value = picked.value.includes(id) ? picked.value.filter(x => x !== id) : [...picked.value, id]
}
async function assignPicked() {
  if (!posWorkshopID.value || !posLineID.value) { error.value = '请先选择车间与生产线'; return }
  if (!pickedPeople.value.length) { error.value = '请先勾选要调入的人员'; return }
  posSaving.value = true; error.value = ''
  const failures: string[] = []
  for (const person of pickedPeople.value) {
    try {
      await api(`/people/${person.id}/org-binding`, {
        method: 'PUT',
        body: JSON.stringify({
          workshop_org_id: posWorkshopID.value, line_org_id: posLineID.value,
          team_org_id: posTeamID.value, reason: '现场调人', version: person.version,
        }),
      })
    } catch (e) { failures.push(`${person.name}：${(e as Error).message}`) }
  }
  posSaving.value = false
  const okCount = pickedPeople.value.length - failures.length
  if (failures.length) { error.value = failures.join('；'); toastError(`成功 ${okCount} 人，失败 ${failures.length} 人`) }
  else { notice.value = `已把 ${okCount} 人调入目标位置`; toastSuccess(notice.value); picked.value = [] }
  await loadCandidates(); await load()
}

/* ===== 新建班组（位置视角）：选定产线后可就地新建班组（含组长），不含组员 ===== */
const teamModal = ref(false)
const teamForm = reactive({ name: '', code: '', manager_employee_id: '', remark: '' })
const teamSaving = ref(false)
const managerOptions = computed(() => candidates.value.filter(p => p.person_type === 'manager').map(p => ({ value: p.id, label: `${p.name}（${p.employee_no}）` })))

function openTeamModal() {
  if (!posLineID.value) { error.value = '班组需要挂在生产线之下：请先选择车间与生产线'; return }
  if (!canWrite.value) { error.value = '需要「维护生产组」权限才能新建班组'; return }
  error.value = ''
  Object.assign(teamForm, { name: '', code: '', manager_employee_id: '', remark: '' })
  teamModal.value = true
}
async function saveTeam() {
  if (!teamForm.name.trim()) { error.value = '班组名称必填'; return }
  teamSaving.value = true; error.value = ''
  try {
    const r = await api<{ id: string; code: string }>(`/lines/${encodeURIComponent(posLineID.value)}/teams`, {
      method: 'POST',
      body: JSON.stringify({
        name: teamForm.name.trim(), code: teamForm.code.trim() || undefined,
        manager_employee_id: teamForm.manager_employee_id || undefined, remark: teamForm.remark.trim(),
      }),
    })
    teamModal.value = false
    notice.value = `班组「${teamForm.name.trim()}」已创建（${r.code}）`
    toastSuccess(notice.value)
    posTeams.value = await loadTeams(posLineID.value)
    posTeamID.value = r.id
  } catch (e) { error.value = (e as Error).message } finally { teamSaving.value = false }
}
onMounted(async () => { await loadWorkshops(); await load(); await loadCandidates() })

/* ===== 班组管理（多级维护）：车间 → 产线 → 班组列表，编辑/停用/启用/删除/组员 ===== */
interface TeamRow {
  id: string; code: string; name: string; status: string; version: number
  remark: string; manager_employee_id: string; manager_name?: string | null; member_count: number
}
const tmWorkshopID = ref('')
const tmLines = ref<OrgOption[]>([])
const tmLineID = ref('')
const tmTeams = ref<TeamRow[]>([])
const tmLoading = ref(false)
/* 编辑班组弹窗 */
const editTeamOpen = ref(false)
const editTeamTarget = ref<TeamRow | null>(null)
const editTeamForm = reactive({ name: '', manager_employee_id: '', remark: '' })
/* 组员弹窗 */
const membersOpen = ref(false)
const membersTeam = ref<TeamRow | null>(null)
const members = ref<Person[]>([])
const membersBusy = ref(false)

async function onTmWorkshop() {
  tmLineID.value = ''; tmTeams.value = []
  tmLines.value = tmWorkshopID.value ? await loadLines(tmWorkshopID.value) : []
}
async function onTmLine() {
  tmTeams.value = []
  if (!tmLineID.value) return
  tmLoading.value = true
  try { tmTeams.value = (await api<{ items: TeamRow[] }>(`/lines/${encodeURIComponent(tmLineID.value)}/teams`, {}, { silent: true })).items || [] }
  catch (e) { error.value = (e as Error).message } finally { tmLoading.value = false }
}
function openEditTeam(t: TeamRow) {
  if (!canWrite.value) { error.value = '需要「维护生产组」权限'; return }
  editTeamTarget.value = t
  Object.assign(editTeamForm, { name: t.name, manager_employee_id: t.manager_employee_id || '', remark: t.remark || '' })
  error.value = ''
  editTeamOpen.value = true
}
async function saveEditTeam() {
  const t = editTeamTarget.value
  if (!t) return
  if (!editTeamForm.name.trim()) { error.value = '班组名称必填'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/teams/${encodeURIComponent(t.id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ name: editTeamForm.name.trim(), manager_employee_id: editTeamForm.manager_employee_id, remark: editTeamForm.remark.trim(), version: t.version }),
    })
    editTeamOpen.value = false
    notice.value = `班组「${editTeamForm.name.trim()}」已保存`
    toastSuccess(notice.value)
    await onTmLine()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function toggleTeamStatus(t: TeamRow) {
  if (!canWrite.value) { error.value = '需要「维护生产组」权限'; return }
  const action = t.status === 'active' ? 'disable' : 'enable'
  try {
    await api(`/teams/${encodeURIComponent(t.id)}/${action}`, { method: 'POST' })
    toastSuccess(t.status === 'active' ? `已停用「${t.name}」` : `已启用「${t.name}」`)
    await onTmLine()
  } catch (e) { error.value = (e as Error).message }
}
async function deleteTeam(t: TeamRow) {
  if (!canWrite.value) { error.value = '需要「维护生产组」权限'; return }
  if (!window.confirm(`确定删除班组「${t.name}」？仅无组员且无历史归属时可删。`)) return
  try {
    await api(`/teams/${encodeURIComponent(t.id)}`, { method: 'DELETE' })
    toastSuccess(`已删除「${t.name}」`)
    await onTmLine()
  } catch (e) { error.value = (e as Error).message }
}
async function openMembers(t: TeamRow) {
  membersTeam.value = t
  membersBusy.value = true; error.value = ''
  try { members.value = (await api<{ items: Person[] }>(`/teams/${encodeURIComponent(t.id)}/members`, {}, { silent: true })).items || [] }
  catch (e) { error.value = (e as Error).message } finally { membersBusy.value = false }
  membersOpen.value = true
}
/* 移出组员（取消分配）：org-binding 全空 = 结束当前主归属、清空员工归属 */
async function removeMember(m: Person) {
  const t = membersTeam.value
  if (!t) return
  if (!window.confirm(`确定把「${m.name}」调出「${t.name}」？`)) return
  try {
    await api(`/people/${encodeURIComponent(m.id)}/org-binding`, {
      method: 'PUT',
      body: JSON.stringify({ workshop_org_id: '', line_org_id: '', team_org_id: '', reason: `调出班组「${t.name}」`, version: m.version }),
    })
    toastSuccess(`已调出 ${m.name}`)
    await openMembers(t)
    await onTmLine()
    await loadCandidates(); await load()
  } catch (e) { error.value = (e as Error).message }
}
/* 跳到位置视角并预选好该班组，继续调入人员 */
function gotoAssign(t: TeamRow) {
  membersOpen.value = false
  view.value = 'position'
  posWorkshopID.value = tmWorkshopID.value
  void onPosWorkshop().then(async () => {
    posLineID.value = tmLineID.value
    await onPosLine()
    posTeamID.value = t.id
  })
}
</script>

<template>
  <div class="page-header"><div><h1>生产组管理</h1><p>把人员配置到车间、生产线与班组；人员建档、账号与验证方式请在「员工信息」维护</p></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <div class="view-tabs">
    <button type="button" :class="{ active: view === 'people' }" @click="view = 'people'">人员视角（勾人 → 选位置）</button>
    <button type="button" :class="{ active: view === 'position' }" @click="view = 'position'">按位置调人（现场）</button>
    <button type="button" :class="{ active: view === 'teams' }" @click="view = 'teams'">班组管理（CRUD + 组员）</button>
  </div>

  <template v-if="view === 'teams'">
    <section class="panel">
      <div class="toolbar" style="margin-top:0">
        <FilterSelect v-model="tmWorkshopID" :options="[{ value: '', label: '选择车间' }, ...workshopOptions]" @update:model-value="onTmWorkshop" />
        <FilterSelect v-model="tmLineID" :options="[{ value: '', label: '选择生产线' }, ...tmLines.map(l => ({ value: l.id, label: l.name }))]" placeholder="选择生产线" @update:model-value="onTmLine" />
        <span class="spacer"></span>
        <button v-if="tmLineID && canWrite" class="button secondary" @click="openTeamModal">＋ 新建班组</button>
      </div>
      <PageState :loading="tmLoading" />
      <div v-if="!tmLoading && tmLineID && !tmTeams.length" class="text-muted" style="padding:24px 0;text-align:center">该产线下暂无班组，点右上角「新建班组」创建</div>
      <div v-if="!tmLoading && !tmLineID" class="text-muted" style="padding:24px 0;text-align:center">请先选择车间与生产线</div>
      <div v-if="!tmLoading && tmTeams.length" class="table-wrap">
        <table>
          <thead><tr>
            <th>编码</th><th>班组名称</th><th>组长</th><th>组员数</th><th>状态</th><th>备注</th><th style="width:280px">操作</th>
          </tr></thead>
          <tbody>
            <tr v-for="t in tmTeams" :key="t.id">
              <td><strong>{{ t.code }}</strong></td>
              <td>{{ t.name }}</td>
              <td>{{ t.manager_name || '—' }}</td>
              <td>{{ t.member_count }}</td>
              <td><span class="status" :class="t.status">{{ t.status === 'active' ? '启用' : '停用' }}</span></td>
              <td class="text-muted">{{ t.remark || '—' }}</td>
              <td>
                <button class="button link" @click="openMembers(t)">组员</button>
                <button class="button link" :disabled="!canWrite" @click="openEditTeam(t)">编辑</button>
                <button class="button link" :disabled="!canWrite" @click="toggleTeamStatus(t)">{{ t.status === 'active' ? '停用' : '启用' }}</button>
                <button class="button link" :disabled="!canWrite" style="color:#d92d20" @click="deleteTeam(t)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </template>

  <template v-if="view === 'position'">
    <section class="panel">
      <div class="toolbar" style="margin-top:0">
        <FilterSelect v-model="posWorkshopID" :options="[{ value: '', label: '选择车间' }, ...workshopOptions]" @update:model-value="onPosWorkshop" />
        <FilterSelect v-model="posLineID" :options="[{ value: '', label: '选择生产线' }, ...posLineOptions]" placeholder="选择生产线" @update:model-value="onPosLine" />
        <FilterSelect v-model="posTeamID" :options="posTeamOptions" placeholder="班组（可选）" />
        <button class="button secondary" type="button" :disabled="!canWrite" title="班组挂在生产线下：先选择车间与生产线" @click="openTeamModal">＋ 新建班组</button>
        <span class="spacer"></span>
        <button class="button" :disabled="!canWrite || !picked.length || posSaving || !posLineID" @click="assignPicked">
          {{ posSaving ? '调入中…' : `把选中 ${picked.length} 人调入此处` }}
        </button>
      </div>
      <p class="text-muted" style="margin:6px 0 12px">
        <template v-if="posLineID">目标位置已有 {{ targetMembers.length }} 人{{ posTeamID ? '（本班组）' : '（整条产线）' }}：{{ targetMembers.slice(0, 8).map(m => m.name).join('、') }}{{ targetMembers.length > 8 ? '…' : '' }}</template>
        <template v-else>请先逐级选择车间与生产线，再从下方勾选要调入的人员（需「维护生产组」权限）。</template>
      </p>
      <div class="table-wrap">
        <table>
          <thead><tr>
            <th style="width:44px"></th><th>工号</th><th>姓名</th><th>当前车间</th><th>当前产线</th><th>当前班组</th>
          </tr></thead>
          <tbody>
            <tr v-if="!candidates.length"><td colspan="6">暂无在职人员</td></tr>
            <tr v-for="c in candidates" :key="c.id">
              <td><input type="checkbox" :checked="picked.includes(c.id)" :disabled="!canWrite" @change="togglePick(c.id)"></td>
              <td><strong>{{ c.employee_no }}</strong></td>
              <td>{{ c.name }}</td>
              <td>{{ c.workshop_name || '—' }}</td>
              <td>{{ c.line_name || '—' }}</td>
              <td>{{ c.team_name || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </template>
  <template v-else>
  <div class="toolbar">
    <FilterSelect v-model="filters.workshop_id" :options="[{ value: '', label: '全部车间' }, ...workshopOptions]" @update:model-value="onFilterWorkshop" />
    <FilterSelect v-model="filters.line_id" :options="[{ value: '', label: '全部产线' }, ...lineOptions]" @update:model-value="load" />
    <FilterSelect v-model="filters.status" :options="[{ value: 'active', label: '在职' }, { value: '', label: '全部' }, { value: 'terminated', label: '离职' }]" @update:model-value="load" />
    <input v-model.trim="filters.keyword" class="search-input" placeholder="工号/姓名（回车）" @keyup.enter="load">
    <button class="button secondary" @click="load">查询</button>
    <span class="spacer"></span>
    <button class="button" :disabled="!selected.length || !canWrite" @click="openBatch">配置到生产线（{{ selected.length }} 人）</button>
  </div>
  <section class="panel">
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap">
      <table>
        <thead><tr>
          <th style="width:44px"><input type="checkbox" :checked="allSelected" @change="toggleAll"></th>
          <th>工号</th><th>姓名</th><th>类型</th><th>车间</th><th>生产线</th><th>班组</th><th>在职</th><th>操作</th>
        </tr></thead>
        <tbody>
          <tr v-if="!items.length"><td colspan="9">暂无人员</td></tr>
          <tr v-for="p in items" :key="p.id">
            <td><input type="checkbox" :checked="selected.includes(p.id)" @change="toggleOne(p.id, $event)"></td>
            <td><strong>{{ p.employee_no }}</strong></td>
            <td>{{ p.name }}</td>
            <td>{{ p.person_type === 'manager' ? '管理' : '工人' }}</td>
            <td>{{ p.workshop_name || '—' }}</td>
            <td>{{ p.line_name || '—' }}</td>
            <td>{{ p.team_name || '—' }}</td>
            <td>{{ p.status === 'active' ? '在职' : '离职' }}</td>
            <td><button class="button link" :disabled="!canWrite || p.status !== 'active'" @click="openSingle(p)">配置</button></td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
  </template>

  <ModalDialog v-if="editTeamOpen" :title="`编辑班组 · ${editTeamTarget?.name || ''}`" @close="editTeamOpen = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label class="required">班组名称</label><input v-model.trim="editTeamForm.name"></div>
      <div class="field"><label>组长（可选，须为管理人员）</label><FilterSelect v-model="editTeamForm.manager_employee_id" :options="[{ value: '', label: '暂不指定' }, ...managerOptions]" placeholder="选择组长" /></div>
      <div class="field full"><label>备注</label><textarea v-model="editTeamForm.remark" rows="2"></textarea></div>
    </div>
    <template #footer><button class="button secondary" @click="editTeamOpen = false">取消</button><button class="button" :disabled="saving" @click="saveEditTeam">保存</button></template>
  </ModalDialog>

  <ModalDialog v-if="membersOpen" :title="`组员 · ${membersTeam?.name || ''}（${members.length} 人）`" @close="membersOpen = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div v-if="membersBusy" class="text-muted">加载中…</div>
    <div v-else-if="!members.length" class="text-muted">暂无组员</div>
    <div v-else class="table-wrap">
      <table>
        <thead><tr><th>工号</th><th>姓名</th><th>类型</th><th style="width:140px">操作</th></tr></thead>
        <tbody>
          <tr v-for="m in members" :key="m.id">
            <td><strong>{{ m.employee_no }}</strong></td>
            <td>{{ m.name }}</td>
            <td>{{ m.person_type === 'manager' ? '管理' : '工人' }}</td>
            <td>
              <button class="button link" :disabled="!canWrite" style="color:#d92d20" @click="removeMember(m)">调出</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <template #footer>
      <button v-if="canWrite" class="button secondary" @click="gotoAssign(membersTeam!)">调入人员 →</button>
      <button class="button" @click="membersOpen = false">关闭</button>
    </template>
  </ModalDialog>

  <ModalDialog v-if="teamModal" title="新建班组" @close="teamModal = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label class="required">班组名称</label><input v-model.trim="teamForm.name" placeholder="如 一组 / 缝纫A班"></div>
      <div class="field"><label>编码（留空自动生成）</label><input v-model.trim="teamForm.code" placeholder="如 BZ-00001"></div>
      <div class="field"><label>组长（可选，须为管理人员）</label><FilterSelect v-model="teamForm.manager_employee_id" :options="[{ value: '', label: '暂不指定' }, ...managerOptions]" placeholder="选择组长" /></div>
      <div class="field"><label>备注</label><input v-model.trim="teamForm.remark" placeholder="可选"></div>
    </div>
    <div class="notice">班组挂在当前选定的生产线之下；组员请创建后回到下方勾选调入。</div>
    <template #footer><button class="button secondary" @click="teamModal = false">取消</button><button class="button" :disabled="teamSaving" @click="saveTeam">{{ teamSaving ? '创建中…' : '创建班组' }}</button></template>
  </ModalDialog>

  <ModalDialog v-if="modalOpen" title="配置到生产线" @close="modalOpen = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <p class="text-muted">已选 {{ modalTargets.length }} 人：{{ modalTargets.slice(0, 5).map(p => p.name).join('、') }}{{ modalTargets.length > 5 ? '…' : '' }}</p>
    <div class="form-grid">
      <div class="field"><label class="required">车间</label><FilterSelect v-model="form.workshop_org_id" :options="workshopOptions" placeholder="选择车间" @update:model-value="onFormWorkshop" /></div>
      <div class="field"><label class="required">生产线</label><FilterSelect v-model="form.line_org_id" :options="formLineOptions" placeholder="选择生产线" @update:model-value="onFormLine" /></div>
      <div class="field"><label>班组</label><FilterSelect v-model="form.team_org_id" :options="[{ value: '', label: '不指定班组' }, ...teamOptions]" placeholder="选择班组" /></div>
      <div class="field"><label>原因</label><input v-model.trim="form.reason" placeholder="如：新产线组员分配"></div>
    </div>
    <div class="notice">车间与生产线须同时选择；班组可选。配置会记录换线历史并更新人员当前归属。</div>
    <template #footer><button class="button secondary" @click="modalOpen = false">取消</button><button class="button" :disabled="saving" @click="saveAssign">保存配置</button></template>
  </ModalDialog>
</template>

<style scoped>
.toolbar { display: flex; gap: 10px; align-items: center; margin: 14px 0; flex-wrap: wrap; }
.view-tabs { display: flex; gap: 8px; margin: 4px 0 0; }
.view-tabs button { padding: 7px 16px; border: 1px solid var(--border, #d0d7e2); border-radius: 8px 8px 0 0; background: transparent; color: inherit; cursor: pointer; font-size: 13px; }
.view-tabs button.active { background: var(--panel, #fff); border-bottom-color: transparent; font-weight: 600; }
</style>
