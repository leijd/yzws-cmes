<script setup lang="ts">
/**
 * 客户资料（基础资料）：客户名称（租户内唯一）/联系人/电话/地址维护。
 * 客户是制衣厂基础资料的独立模块（订单与项目引用它），不属于员工信息。
 * 权限：查看 masterdata:read，新增 masterdata:write。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'

interface Customer { id: string; name: string; status: string; version: number }

const { has } = usePermissions()
const canWrite = computed(() => has('masterdata:write'))

const items = ref<Customer[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const keyword = ref('')
const form = reactive({ name: '', contact_name: '', phone: '', address: '', remark: '' })
const showForm = ref(false)

const statusText: Record<string, string> = { active: '启用', disabled: '停用' }

async function load() {
  loading.value = true; error.value = ''
  try { items.value = (await api<{ items: Customer[] }>('/customers')).items || [] }
  catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

function openCreate() {
  Object.assign(form, { name: '', contact_name: '', phone: '', address: '', remark: '' })
  error.value = ''
  showForm.value = true
}

async function save() {
  if (!form.name.trim()) { error.value = '客户名称必填'; return }
  saving.value = true; error.value = ''
  try {
    await api('/customers', { method: 'POST', body: JSON.stringify(form) })
    showForm.value = false
    notice.value = '客户已创建'
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  if (!k) return items.value
  return items.value.filter(c => c.name.toLowerCase().includes(k))
})

onMounted(load)
</script>

<template>
  <div class="page-header">
    <div><h1>客户资料</h1><p>客户名称（租户内唯一）与联系方式维护；订单与项目建档时引用这里的数据</p></div>
    <div class="header-actions"><button v-if="canWrite" class="button" @click="openCreate">+ 新增客户</button></div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>

  <section class="panel">
    <div class="toolbar">
      <input v-model.trim="keyword" class="search-input" placeholder="搜索客户名称">
      <span class="spacer"></span>
      <button class="button secondary" @click="load">刷新</button>
    </div>
    <PageState :loading="loading" :empty="!filtered.length" />
    <div v-if="!loading && filtered.length" class="table-wrap">
      <table>
        <thead><tr><th>客户名称</th><th>状态</th><th>版本</th></tr></thead>
        <tbody>
          <tr v-for="item in filtered" :key="item.id">
            <td>{{ item.name }}</td>
            <td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td>
            <td>v{{ item.version }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <ModalDialog v-if="showForm" title="新增客户" @close="showForm = false">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field full"><label class="required">客户名称</label><input v-model.trim="form.name" placeholder="租户内唯一"></div>
      <div class="field"><label>联系人</label><input v-model.trim="form.contact_name"></div>
      <div class="field"><label>联系电话</label><input v-model.trim="form.phone"></div>
      <div class="field full"><label>地址</label><input v-model.trim="form.address"></div>
      <div class="field full"><label>备注</label><textarea v-model="form.remark" rows="2"></textarea></div>
    </div>
    <template #footer>
      <button class="button secondary" @click="showForm = false">取消</button>
      <button class="button" :disabled="saving || !form.name" @click="save">保存</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.toolbar { display: flex; gap: 10px; align-items: center; margin-bottom: 14px; }
</style>
