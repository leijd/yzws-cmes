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
/* 权限表检索：功能模块 / 权限名称 / 类型 / 说明 四列内容过滤 */
const permFilter = ref('')

/* 多级分组树：功能模块 -> 权限名称 -> 类型（类型与允许勾选框一一对应）；
 * 同名模块 / 同名权限名称合并到同一分组行显示；检索时仅保留有匹配子孙的分组链。 */
interface PermLeaf { code: string; module: string; name: string; kind: string; description: string }
interface PermNameGroup { name: string; items: PermLeaf[] }
interface ModuleGroup { name: string; perms: PermNameGroup[] }
const filteredGrouped = computed<ModuleGroup[]>(() => {
  const q = permFilter.value.trim().toLowerCase()
  const leaves = assignablePermissions.value.filter(item => {
    if (!q) return true
    return String(item.module || '').toLowerCase().includes(q) ||
      String(item.name || '').toLowerCase().includes(q) ||
      String(kindText[item.kind] || item.kind || '').toLowerCase().includes(q) ||
      String(item.description || '').toLowerCase().includes(q)
  })
  const modMap = new Map<string, ModuleGroup & { permMap: Map<string, PermNameGroup> }>()
  const modules: (ModuleGroup & { permMap: Map<string, PermNameGroup> })[] = []
  for (const item of leaves) {
    let m = modMap.get(item.module)
    if (!m) {
      m = { name: item.module, perms: [], permMap: new Map() }
      modMap.set(item.module, m)
      modules.push(m)
    }
    let pn = m.permMap.get(item.name)
    if (!pn) {
      pn = { name: item.name, items: [] }
      m.permMap.set(item.name, pn)
      m.perms.push(pn)
    }
    pn.items.push(item)
  }
  return modules
})

/* 分组勾选状态：全选 / 全不选 / 部分（indeterminate） */
function groupStates(codes: string[]) {
  const checked = codes.filter(code => selectedPermissions[code]).length
  return { all: codes.length > 0 && checked === codes.length, none: checked === 0 }
}
const permStates = (pn: PermNameGroup) => groupStates(pn.items.map(it => it.code))
const moduleStates = (m: ModuleGroup) => groupStates(m.perms.flatMap(pn => pn.items.map(it => it.code)))
const permIndeterminate = (pn: PermNameGroup) => { const s = permStates(pn); return !s.all && !s.none }
const moduleIndeterminate = (m: ModuleGroup) => { const s = moduleStates(m); return !s.all && !s.none }

/* 点击分组勾选框：选中 = 该分组及全部子孙打勾；取消 = 全部子孙取消 */
function toggleModule(m: ModuleGroup, ev: Event) {
  const checked = (ev.target as HTMLInputElement).checked
  for (const pn of m.perms) for (const it of pn.items) selectedPermissions[it.code] = checked
}
function togglePermName(pn: PermNameGroup, ev: Event) {
  const checked = (ev.target as HTMLInputElement).checked
  for (const it of pn.items) selectedPermissions[it.code] = checked
}
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
  permFilter.value = ''
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
  <ModalDialog v-if="modal" wide :title="currentRole ? `配置角色 · ${currentRole.name}` : '新建角色'" @close="modal = false"><div v-if="error" class="notice error">{{ error }}</div><div class="form-grid"><div class="field"><label>角色编码</label><input v-model.trim="roleForm.code" :disabled="!!currentRole" placeholder="custom_role"></div><div class="field"><label>角色名称</label><input v-model.trim="roleForm.name"></div><div class="field full"><label>说明</label><input v-model="roleForm.description"></div></div><template v-if="currentRole?.is_system"><div class="notice" style="margin-top:16px">固定管理员角色不可修改，仅保留用户、角色与权限管理。</div><h3>已授予权限（只读）</h3><div class="table-wrap perm-table"><table><thead><tr><th>功能模块</th><th>权限名称</th><th>说明</th></tr></thead><tbody><tr v-for="item in permissions.filter(p => selectedPermissions[p.code])" :key="item.code"><td>{{ item.module }}</td><td>{{ item.name }}</td><td class="text-muted">{{ item.description }}</td></tr></tbody></table></div></template><template v-else><h3>功能权限（数据权限）</h3><div class="field" style="margin:8px 0"><input v-model="permFilter" placeholder="检索功能模块 / 权限名称 / 类型 / 说明"></div><div class="table-wrap perm-table"><table class="perm-tree"><thead><tr><th style="width:140px">功能模块</th><th style="width:180px">权限名称</th><th style="width:64px">类型</th><th style="width:56px">允许</th><th>说明</th></tr></thead><tbody><template v-for="m in filteredGrouped" :key="'m-' + m.name"><tr class="grp grp-module"><td colspan="5"><label class="grp-label"><input type="checkbox" :checked="moduleStates(m).all" :indeterminate="moduleIndeterminate(m)" @change="toggleModule(m, $event)"><strong>{{ m.name }}</strong></label></td></tr><template v-for="pn in m.perms" :key="m.name + '/' + pn.name"><tr class="grp grp-perm"><td class="col-spacer"></td><td colspan="4"><label class="grp-label"><input type="checkbox" :checked="permStates(pn).all" :indeterminate="permIndeterminate(pn)" @change="togglePermName(pn, $event)">{{ pn.name }}</label></td></tr><tr v-for="it in pn.items" :key="it.code" class="leaf-row"><td class="col-spacer"></td><td class="col-spacer"></td><td>{{ kindText[it.kind] || it.kind }}</td><td><input v-model="selectedPermissions[it.code]" type="checkbox"></td><td class="desc">{{ it.description || '—' }}</td></tr></template></template><tr v-if="!filteredGrouped.length"><td colspan="5" class="text-muted" style="text-align:center;padding:14px">无匹配的权限点</td></tr></tbody></table></div></template><template #footer><button class="button secondary" @click="modal = false">取消</button><button v-if="!currentRole?.is_system" class="button" :disabled="saving" @click="saveRole">保存</button></template></ModalDialog>
</template>

<style scoped>
/* 角色编辑弹窗：原 wide 1000px + 120px */
:deep(.modal.wide) { width: min(1120px, 100%); }

/* 权限分配表：表头与分组行固定（滚动时，子孙仍有可见项时分组行保持在视口顶部） */
.perm-table { --row-h: 37px; }
.perm-table thead th { position: sticky; top: 0; z-index: 3; background: #fff; box-shadow: 0 1px 0 var(--border, #e2e8f0); }
.perm-tree .grp-module > td { position: sticky; top: var(--row-h); z-index: 2; background: #e3ecfb; }
.perm-tree .grp-perm > td { position: sticky; top: calc(var(--row-h) * 2); z-index: 1; background: #f2f6fe; }
.perm-tree .grp-label { display: flex; align-items: center; gap: 8px; font-weight: 600; color: #17255f; }
.perm-tree .grp-perm .grp-label { font-weight: 500; }
.perm-tree .col-spacer { padding: 0; border-top: none; }
.perm-tree .leaf-row td { vertical-align: top; }
.perm-tree .desc { white-space: normal; word-break: break-word; }
</style>
