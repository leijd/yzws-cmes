<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'

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
const roleForm = reactive({ code: '', name: '', description: '' })

const kindText: Record<string, string> = { read: '读', write: '写', action: '操作' }
const assignablePermissions = computed(() => permissions.value.filter(item => !item.admin_only))
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
  Object.assign(roleForm, item ? { code: item.code, name: item.name, description: item.description } : { code: '', name: '', description: '' })
  clearSelected()
  modal.value = true
  if (!item) return
  try {
    const detail = await api<{ permission_codes: string[] }>(`/roles/${item.id}/permissions`)
    detail.permission_codes.forEach((code: string) => selectedPermissions[code] = true)
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
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>角色与权限</h1><p>用户账号与员工档案在“员工与账号”页面统一维护；此处配置角色权限</p></div><div class="header-actions"><button v-if="canWrite" class="button" @click="openRole(null)">+ 角色</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><div class="tabs"><button :class="{ active: tab === 'roles' }" @click="tab = 'roles'">角色</button><button :class="{ active: tab === 'permissions' }" @click="tab = 'permissions'">权限点</button></div><PageState :loading="loading" />
    <div v-if="!loading && tab === 'roles'" class="table-wrap"><table><thead><tr><th>角色编码</th><th>名称</th><th>来源</th><th>用户数</th><th>类型</th><th>操作</th></tr></thead><tbody><tr v-for="item in roles" :key="item.id"><td>{{ item.code }}</td><td>{{ item.name }}</td><td><span class="status" :class="item.source === 'platform' ? 'active' : 'pending'">{{ item.source === 'platform' ? '平台提供' : '租户自定义' }}</span></td><td>{{ item.user_count }}</td><td>{{ item.is_system ? '固定管理员' : '可配置' }}</td><td><button v-if="!item.is_system && canWrite" class="button link" @click="openRole(item)">配置权限</button><button v-else class="button link" @click="openRole(item)">查看权限</button><button v-if="!item.is_system && canWrite" class="button link text-danger" @click="deleteRole(item)">删除</button></td></tr></tbody></table><PageState :empty="!roles.length" /></div>
    <div v-if="!loading && tab === 'permissions'" class="table-wrap"><table><thead><tr><th>功能模块</th><th>权限名称</th><th>类型</th><th>说明</th><th>范围</th></tr></thead><tbody><tr v-for="item in permissions" :key="item.code"><td>{{ item.module }}</td><td>{{ item.name }}</td><td>{{ kindText[item.kind] || item.kind }}</td><td>{{ item.description || '—' }}</td><td>{{ item.admin_only ? '仅租户管理员' : '可分配' }}</td></tr></tbody></table><PageState :empty="!permissions.length" /></div>
  </section>
  <ModalDialog v-if="modal" wide :title="currentRole ? `配置角色 · ${currentRole.name}` : '新建租户自定义角色'" @close="modal = false"><div v-if="error" class="notice error">{{ error }}</div><div class="form-grid"><div class="field"><label>角色编码</label><input v-model.trim="roleForm.code" :disabled="!!currentRole" placeholder="custom_role"></div><div class="field"><label>角色名称</label><input v-model.trim="roleForm.name"></div><div class="field full"><label>说明</label><input v-model="roleForm.description"></div></div><template v-if="currentRole?.is_system"><div class="notice" style="margin-top:16px">固定管理员角色不可修改，仅保留用户、角色与权限管理。</div><h3>已授予权限（只读）</h3><div class="table-wrap" style="max-height:40vh;overflow:auto"><table><thead><tr><th>功能模块</th><th>权限名称</th><th>说明</th></tr></thead><tbody><tr v-for="item in permissions.filter(p => selectedPermissions[p.code])" :key="item.code"><td>{{ item.module }}</td><td>{{ item.name }}</td><td class="text-muted">{{ item.description }}</td></tr></tbody></table></div></template><template v-else><h3>功能权限（数据权限）</h3><div class="table-wrap" style="max-height:46vh;overflow:auto"><table><thead><tr><th style="width:130px">功能模块</th><th>权限名称</th><th style="width:60px">类型</th><th>说明</th><th style="width:52px">允许</th></tr></thead><tbody><tr v-for="item in assignablePermissions" :key="item.code"><td>{{ item.module }}</td><td>{{ item.name }}</td><td>{{ kindText[item.kind] || item.kind }}</td><td class="text-muted">{{ item.description }}</td><td><input v-model="selectedPermissions[item.code]" type="checkbox"></td></tr></tbody></table></div></template><template #footer><button class="button secondary" @click="modal = false">取消</button><button v-if="!currentRole?.is_system" class="button" :disabled="saving" @click="saveRole">保存</button></template></ModalDialog>
</template>
