<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('settings:write'))
const tab = ref<'export' | 'import'>('export')
const exports = ref<any[]>([])
const imports = ref<any[]>([])
const errors = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const uploading = ref(false)
const error = ref('')
const notice = ref('')
const exportForm = reactive({ job_type: 'employees', period: new Date().toISOString().slice(0, 7) })
const importJob = ref<any>(null)

async function load() {
  loading.value = true; error.value = ''
  try {
    const [e, i] = await Promise.all([api<{ items: any[] }>('/export-jobs'), api<{ items: any[] }>('/import-jobs')])
    exports.value = e.items; imports.value = i.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function createExport() {
  saving.value = true; error.value = ''
  try {
    const filter = exportForm.job_type === 'wages' ? { period: exportForm.period } : {}
    await api('/export-jobs', { method: 'POST', body: JSON.stringify({ job_type: exportForm.job_type, format: 'csv', filter_snapshot: filter }) })
    notice.value = '导出已生成'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function download(item: any) {
  try {
    const response = await fetch(`/api/v1/export-jobs/${item.id}/content`, { headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}` } })
    if (!response.ok) throw new Error('下载失败')
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url; link.download = `${item.job_type}-${item.id}.csv`; link.click()
    URL.revokeObjectURL(url)
  } catch (e) { error.value = (e as Error).message }
}
function downloadTemplate() {
  const content = '工号,姓名,岗位,车间,产线,状态\nC001,张三,车工,一车间,1线,active\n'
  const blob = new Blob(['\ufeff' + content], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url; link.download = 'employees-template.csv'; link.click()
  URL.revokeObjectURL(url)
}
async function uploadImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true; error.value = ''
  try {
    const bytes = new Uint8Array(await file.arrayBuffer())
    const digest = await crypto.subtle.digest('SHA-256', bytes)
    const sha256 = Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('')
    const init = await api<{ attachment_id: string; chunk_size: number }>('/attachments/init', { method: 'POST', body: JSON.stringify({ sha256, size_bytes: file.size, mime_type: file.type || 'text/csv', owner_type: 'import' }) })
    const chunkSize = init.chunk_size || 4 * 1024 * 1024
    let index = 0
    for (let offset = 0; offset < file.size; offset += chunkSize) {
      const chunk = file.slice(offset, offset + chunkSize)
      const response = await fetch(`/api/v1/attachments/${init.attachment_id}/chunks/${index++}`, { method: 'PUT', headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}`, 'Content-Type': 'application/octet-stream' }, body: chunk })
      if (!response.ok) throw new Error('分片上传失败')
    }
    await api(`/attachments/${init.attachment_id}/complete`, { method: 'POST', body: JSON.stringify({ total_chunks: index }) })
    const job = await api<{ id: string; total_rows: number; success_rows: number; error_rows: number }>('/import-jobs', { method: 'POST', body: JSON.stringify({ job_type: 'employees', source_attachment_id: init.attachment_id }) })
    notice.value = `校验完成：共 ${job.total_rows} 行，可导入 ${job.success_rows} 行，错误 ${job.error_rows} 行`
    importJob.value = job
    await load(); await showErrors(job)
  } catch (e) { error.value = (e as Error).message } finally { uploading.value = false; if (input) input.value = '' }
}
async function showErrors(job: any) {
  try { errors.value = (await api<{ items: any[] }>(`/import-jobs/${job.id}/errors`)).items } catch { errors.value = [] }
}
async function commit(job: any) {
  if (!window.confirm('确认提交导入？校验通过的员工将写入档案。')) return
  error.value = ''
  try { const result = await api<{ inserted: number }>(`/import-jobs/${job.id}/commit`, { method: 'POST', body: '{}' }); notice.value = `导入完成，新增 ${result.inserted} 名员工`; await load() } catch (e) { error.value = (e as Error).message }
}
function switchTab(value: 'export' | 'import') { tab.value = value }
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>导入导出中心</h1><p>CSV 导出、员工批量导入与错误校验</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel">
    <div class="tabs"><button :class="{ active: tab === 'export' }" @click="switchTab('export')">导出</button><button :class="{ active: tab === 'import' }" @click="switchTab('import')">导入</button></div>
    <PageState :loading="loading" />

    <div v-if="!loading && tab === 'export'" class="panel-body">
      <div class="form-grid">
        <div class="field"><label>导出类型</label><FilterSelect v-model="exportForm.job_type" :options="[{value:'employees',label:'员工档案'},{value:'wages',label:'工资数据'}]"/></div>
        <div class="field" v-if="exportForm.job_type === 'wages'"><label>工资期间</label><input v-model.trim="exportForm.period" placeholder="YYYY-MM"></div>
        <div class="field full"><button v-if="canWrite" class="button" :disabled="saving" @click="createExport">创建导出</button></div>
      </div>
      <div class="table-wrap"><table><thead><tr><th>类型</th><th>状态</th><th>进度</th><th>创建时间</th><th>操作</th></tr></thead><tbody><tr v-for="item in exports" :key="item.id"><td>{{ item.job_type }}</td><td>{{ item.status }}</td><td>{{ item.progress }}%</td><td>{{ new Date(item.created_at / 1000).toLocaleString('zh-CN') }}</td><td><button v-if="item.status === 'succeeded'" class="button link" @click="download(item)">下载 CSV</button></td></tr></tbody></table><PageState :empty="!exports.length"/></div>
    </div>

    <div v-if="!loading && tab === 'import'" class="panel-body">
      <div class="toolbar" style="padding:0 0 14px;border:0"><button class="button secondary" @click="downloadTemplate">下载模板</button><input type="file" accept=".csv,text/csv" :disabled="uploading" @change="uploadImport"><span class="text-muted">模板列：工号、姓名、岗位、车间、产线、状态</span></div>
      <div class="table-wrap"><table><thead><tr><th>类型</th><th>状态</th><th>总行数</th><th>可导入</th><th>错误行</th><th>创建时间</th><th>操作</th></tr></thead><tbody><tr v-for="item in imports" :key="item.id"><td>{{ item.job_type }}</td><td>{{ item.status }}</td><td>{{ item.total_rows }}</td><td>{{ item.success_rows }}</td><td>{{ item.error_rows }}</td><td>{{ new Date(item.created_at / 1000).toLocaleString('zh-CN') }}</td><td><button class="button link" @click="showErrors(item); importJob = item">查看错误</button><button v-if="canWrite && item.status !== 'committed'" class="button link" @click="commit(item)">提交导入</button></td></tr></tbody></table><PageState :empty="!imports.length"/></div>
      <div v-if="errors.length" class="table-wrap" style="max-height:30vh"><table><thead><tr><th>行号</th><th>字段</th><th>错误</th><th>原值</th></tr></thead><tbody><tr v-for="(item, index) in errors" :key="index"><td>{{ item.row_no }}</td><td>{{ item.field }}</td><td class="text-danger">{{ item.message }}</td><td>{{ item.raw_value }}</td></tr></tbody></table></div>
    </div>
  </section>
</template>
