<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import PermissionMatrix from '../components/PermissionMatrix.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('identity:write'))

type Tab = 'roles' | 'permissions'
const tab = ref<Tab>('roles')
const roles = ref<any[]>([])
const permissions = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref(false)
const currentRole = ref<any>(null)
const selectedPermissions = reactive<Record<string, boolean>>({})
/* 角色编码由服务端自动生成（custom_001…），表单只维护名称与说明 */
const roleForm = reactive({ name: '', description: '' })

/* 平台角色库：已存在的角色保持勾选且置灰不可取消，只能勾选本厂缺少的角色 */
const libraryModal = ref(false)
const libraryItems = ref<any[]>([])
const libraryLoading = ref(false)
const librarySaving = ref(false)
const selectedTemplates = reactive<Record<string, boolean>>({})
const missingTemplates = computed(() => libraryItems.value.filter(item => !item.exists))
const selectedMissing = computed(() => missingTemplates.value.filter(item => selectedTemplates[item.id]))

const kindText: Record<string, string> = { read: '查看', write: '维护', action: '操作' }
function clearSelected() { Object.keys(selectedPermissions).forEach(key => delete selectedPermissions[key]) }
async function load() {
  loading.value = true; error.value = ''
  try {
    const [r, p] = await Promise.all([api<{ items: any[] }>('/roles'), api<{ items: any[] }>('/permissions')])
    roles.value = r.items
    permissions.value = p.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function openRole(item: any) {
  currentRole.value = item || null
  Object.assign(roleForm, item ? { name: item.name, description: item.description } : { name: '', description: '' })
  clearSelected()
  modal.value = true
  if (!item) return
  try {
    const detail = await api<{ permission_codes: string[] }>(`/roles/${item.id}/permissions`)
    /* 历史角色可能带有“仅管理员”权限（如 identity:read），界面不展示也不能提交，
     * 加载时只恢复仍可分配的权限点，避免保存触发 INVALID_PERMISSION。 */
    const assignable = new Set(permissions.value.filter((p: any) => !p.admin_only).map((p: any) => p.code))
    detail.permission_codes.forEach((code: string) => { if (assignable.has(code)) selectedPermissions[code] = true })
  } catch (e) { error.value = (e as Error).message }
}
async function saveRole() {
  saving.value = true; error.value = ''
  try {
    let roleID = currentRole.value?.id
    if (!roleID) roleID = (await api<{ id: string }>('/roles', { method: 'POST', body: JSON.stringify(roleForm) })).id
    await api(`/roles/${roleID}/permissions`, { method: 'PUT', body: JSON.stringify({ permission_codes: Object.keys(selectedPermissions).filter(key => selectedPermissions[key]) }) })
    modal.value = false
    notice.value = '角色及权限已保存'
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function deleteRole(item: any) {
  if (!window.confirm(`确认删除角色 ${item.name}？已分配给用户的角色不能删除。`)) return
  error.value = ''
  try { await api(`/roles/${item.id}`, { method: 'DELETE' }); notice.value = `角色 ${item.name} 已删除`; await load() } catch (e) { error.value = (e as Error).message }
}
async function openLibrary() {
  libraryModal.value = true; libraryLoading.value = true; error.value = ''
  try {
    const res = await api<{ items: any[] }>('/role-templates')
    libraryItems.value = res.items
    Object.keys(selectedTemplates).forEach(key => delete selectedTemplates[key])
    /* 缺失的角色默认勾选，已存在的角色固定勾选并置灰 */
    res.items.forEach((item: any) => { selectedTemplates[item.id] = !item.exists })
  } catch (e) { error.value = (e as Error).message } finally { libraryLoading.value = false }
}
async function applyLibrary() {
  const ids = selectedMissing.value.map(item => item.id)
  if (!ids.length) { notice.value = '没有需要补充的角色'; libraryModal.value = false; return }
  librarySaving.value = true; error.value = ''
  try {
    const res = await api<{ created_count: number }>('/role-templates/apply', { method: 'POST', body: JSON.stringify({ template_ids: ids }) })
    libraryModal.value = false
    notice.value = `已补充 ${res.created_count} 个角色，已存在的角色保持原样`
    await load()
  } catch (e) { error.value = (e as Error).message } finally { librarySaving.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>角色与权限</h1><p>用户账号与员工档案在“员工与账号”页面统一维护；此处配置角色权限</p></div><div class="header-actions"><button v-if="canWrite" class="button secondary" @click="openLibrary">从平台库补充角色</button><button v-if="canWrite" class="button" @click="openRole(null)">+ 角色</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><div class="tabs"><button :class="{ active: tab === 'roles' }" @click="tab = 'roles'">角色</button><button :class="{ active: tab === 'permissions' }" @click="tab = 'permissions'">权限点</button></div><PageState :loading="loading" />
    <div v-if="!loading && tab === 'roles'" class="table-wrap"><table><thead><tr><th>角色编码</th><th>名称</th><th>来源</th><th>用户数</th><th>类型</th><th>操作</th></tr></thead><tbody><tr v-for="item in roles" :key="item.id"><td>{{ item.code }}</td><td>{{ item.name }}</td><td><span class="status" :class="item.source === 'platform' ? 'active' : 'pending'">{{ item.source === 'platform' ? '平台提供' : '租户自定义' }}</span></td><td>{{ item.user_count }}</td><td>{{ item.is_system ? '固定管理员' : '可配置' }}</td><td><button v-if="!item.is_system && canWrite" class="button link" @click="openRole(item)">配置权限</button><button v-else class="button link" @click="openRole(item)">查看权限</button><button v-if="!item.is_system && canWrite" class="button link text-danger" @click="deleteRole(item)">删除</button></td></tr></tbody></table><PageState :empty="!roles.length" /></div>
    <div v-if="!loading && tab === 'permissions'" class="table-wrap"><table><thead><tr><th>功能模块</th><th>权限名称</th><th>类型</th><th>说明</th><th>范围</th></tr></thead><tbody><tr v-for="item in permissions" :key="item.code"><td>{{ item.module }}</td><td>{{ item.name }}</td><td>{{ kindText[item.kind] || item.kind }}</td><td>{{ item.description || '—' }}</td><td>{{ item.admin_only ? '仅租户管理员' : '可分配' }}</td></tr></tbody></table><PageState :empty="!permissions.length" /></div>
  </section>
  <ModalDialog v-if="modal" wide :title="currentRole ? `配置角色 · ${currentRole.name}` : '新建角色'" @close="modal = false"><div v-if="error" class="notice error">{{ error }}</div><div class="form-grid"><div class="field"><label>角色编码</label><input :value="currentRole ? currentRole.code : '保存后自动生成（custom_001）'" disabled></div><div class="field"><label>角色名称</label><input v-model.trim="roleForm.name"></div><div class="field full"><label>说明</label><input v-model="roleForm.description"></div></div><div v-if="currentRole?.is_system" class="notice" style="margin-top:16px">固定管理员角色不可修改，仅保留用户、角色与权限管理。</div><h3>{{ currentRole?.is_system ? '已授予数据权限（只读）' : '数据权限' }}</h3><div class="notice" style="margin:8px 0">维护 = 相关业务数据可 CRUD（自动包含查看）；查看 = 相关业务数据允许查询。</div><PermissionMatrix :items="permissions" :selected="selectedPermissions" :readonly="!!currentRole?.is_system" /><template #footer><button class="button secondary" @click="modal = false">取消</button><button v-if="!currentRole?.is_system" class="button" :disabled="saving" @click="saveRole">保存</button></template></ModalDialog>
  <ModalDialog v-if="libraryModal" wide title="从平台库补充角色" @close="libraryModal = false">
    <div class="notice">只需勾选本厂缺少的角色；已存在的角色保持勾选且不可取消，名称与权限不会被覆盖。</div>
    <PageState :loading="libraryLoading" />
    <div v-if="!libraryLoading" class="table-wrap">
      <table>
        <thead><tr><th style="width:64px">选择</th><th>角色名称</th><th>编码</th><th>权限数</th><th>状态</th></tr></thead>
        <tbody>
          <tr v-for="item in libraryItems" :key="item.id" :class="{ 'row-disabled': item.exists }">
            <td><input v-model="selectedTemplates[item.id]" type="checkbox" :disabled="item.exists"></td>
            <td>{{ item.name }}</td>
            <td>{{ item.code }}</td>
            <td>{{ item.permission_count }}</td>
            <td><span class="status" :class="item.exists ? 'active' : 'pending'">{{ item.exists ? '已存在，保持原样' : '可补充' }}</span></td>
          </tr>
        </tbody>
      </table>
      <PageState :empty="!libraryItems.length" />
    </div>
    <template #footer><button class="button secondary" @click="libraryModal = false">取消</button><button class="button" :disabled="librarySaving || !selectedMissing.length" @click="applyLibrary">补充勾选角色（{{ selectedMissing.length }}）</button></template>
  </ModalDialog>
</template>

<style scoped>
/* 角色编辑弹窗：原 wide 1000px + 120px */
:deep(.modal.wide) { width: min(1120px, 100%); }

/* 平台角色库：已存在的角色整行置灰，勾选框保持勾选且不可取消 */
.row-disabled td { color: #94a3b8; background: #f8fafc; }
.row-disabled .status { opacity: .75; }
</style>
