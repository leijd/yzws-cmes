<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('settings:write'))
const tab = ref<'backup' | 'conflicts'>('backup')
const backups = ref<any[]>([])
const conflicts = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const current = ref<any>(null)
const form = reactive({ backup_type: 'manifest', include_attachments: false })
const resolveForm = reactive({ resolution_type: 'keep_current', reason: '' })

const backupStatusText: Record<string, string> = { running: '进行中', succeeded: '已完成', verified: '校验通过', failed: '失败' }
const conflictStatusText: Record<string, string> = { open: '待处理', resolved: '已处理', ignored: '已忽略' }

async function load() {
  loading.value = true; error.value = ''
  try {
    const [b, c] = await Promise.all([api<{ items: any[] }>('/backup-jobs'), api<{ items: any[] }>('/sync/conflicts')])
    backups.value = b.items; conflicts.value = c.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function createBackup() {
  saving.value = true; error.value = ''
  try {
    const result = await api<{ id: string; tables: number }>('/backup-jobs', { method: 'POST', body: JSON.stringify(form) })
    notice.value = `备份已生成（${result.tables} 张表）`; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function verify(item: any) {
  error.value = ''
  try {
    const result = await api<{ status: string; match: boolean }>(`/backup-jobs/${item.id}/verify`, { method: 'POST', body: '{}' })
    notice.value = result.match ? '备份校验通过' : '备份校验失败：指纹不一致'
    await load()
  } catch (e) { error.value = (e as Error).message }
}
function openResolve(item: any) {
  current.value = item
  resolveForm.resolution_type = 'keep_current'; resolveForm.reason = ''
  error.value = ''; modal.value = 'resolve'
}
async function resolveConflict() {
  if (!current.value || !resolveForm.reason.trim()) { error.value = '请选择处理方式并填写原因'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/sync/conflicts/${current.value.id}/resolve`, { method: 'POST', body: JSON.stringify(resolveForm) })
    modal.value = ''; current.value = null; notice.value = '冲突已处理'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function changeTab(value: 'backup' | 'conflicts') { tab.value = value }
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>备份与同步</h1><p>备份指纹校验、同步冲突处理</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel">
    <div class="tabs"><button :class="{ active: tab === 'backup' }" @click="changeTab('backup')">备份</button><button :class="{ active: tab === 'conflicts' }" @click="changeTab('conflicts')">同步冲突</button></div>
    <PageState :loading="loading" />
    <div v-if="!loading && tab === 'backup'" class="panel-body">
      <div class="form-grid"><div class="field"><label>备份类型</label><FilterSelect v-model="form.backup_type" :options="[{value:'manifest',label:'清单快照'},{value:'database',label:'数据库'}]"/></div><div class="field"><label>包含附件</label><div style="display:flex;align-items:center;height:38px"><input v-model="form.include_attachments" type="checkbox"> 记录附件清单</div></div><div class="field full"><button v-if="canWrite" class="button" :disabled="saving" @click="createBackup">创建备份</button></div></div>
      <div class="table-wrap"><table><thead><tr><th>类型</th><th>节点</th><th>状态</th><th>附件</th><th>指纹</th><th>完成时间</th><th>操作</th></tr></thead><tbody><tr v-for="item in backups" :key="item.id"><td>{{ item.backup_type }}</td><td>{{ item.node_id }}</td><td><span class="status" :class="item.status === 'failed' ? 'rejected' : 'approved'">{{ backupStatusText[item.status] || item.status }}</span></td><td>{{ item.includes_attachments ? '是' : '否' }}</td><td class="text-muted">{{ (item.manifest_sha256 || '').slice(0, 12) }}</td><td>{{ item.finished_at ? new Date(item.finished_at / 1000).toLocaleString('zh-CN') : '—' }}</td><td><button v-if="canWrite" class="button link" @click="verify(item)">校验</button></td></tr></tbody></table><PageState :empty="!backups.length"/></div>
      <div class="notice">在线恢复已禁用；恢复请使用 pg_restore 在隔离环境执行，并先完成校验。</div>
    </div>
    <div v-if="!loading && tab === 'conflicts'" class="table-wrap"><table><thead><tr><th>聚合</th><th>冲突码</th><th>状态</th><th>当前值</th><th>传入值</th><th>处理方式</th><th>操作</th></tr></thead><tbody><tr v-for="item in conflicts" :key="item.id"><td>{{ item.aggregate_type }}<small class="text-muted" style="display:block">{{ item.aggregate_id }}</small></td><td>{{ item.conflict_code }}</td><td><span class="status" :class="item.status === 'resolved' ? 'approved' : 'pending'">{{ conflictStatusText[item.status] || item.status }}</span></td><td class="text-muted">{{ item.current_value }}</td><td class="text-muted">{{ item.incoming_value }}</td><td>{{ item.resolution_type || '—' }}</td><td><button v-if="canWrite && item.status !== 'resolved'" class="button link" @click="openResolve(item)">处理</button></td></tr></tbody></table><PageState :empty="!conflicts.length"/></div>
  </section>

  <ModalDialog v-if="modal === 'resolve'" title="处理同步冲突" @close="modal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid"><div class="field full"><label>处理方式</label><FilterSelect v-model="resolveForm.resolution_type" :options="[{value:'keep_current',label:'保留当前值'},{value:'keep_incoming',label:'采用传入值'},{value:'merged',label:'合并'}]"/></div><div class="field full"><label class="required">原因</label><input v-model.trim="resolveForm.reason"></div></div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !resolveForm.reason" @click="resolveConflict">确认处理</button></template>
  </ModalDialog>
</template>
