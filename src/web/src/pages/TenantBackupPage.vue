<script setup lang="ts">
/**
 * 明文离线备份页（设计 3.9 / 6.1）：backup:export 权限。
 * 从密文库解密导出单租户 JSONL + manifest（SHA-256）；一次性短期下载，取包即作废。
 * 备份包不再受字段级加密保护，必须按高敏感文件保管。
 */
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { toastError, toastSuccess } from '../toast'

interface BackupJob {
  id: string; status: string; manifest_sha256?: string | null
  total_rows: number; table_count: number
  snapshot_at?: number | null; finished_at?: number | null
  downloaded: boolean; error?: string | null
}

const items = ref<BackupJob[]>([])
const loading = ref(false)
const error = ref('')
const exporting = ref(false)
const verifyResults = ref<Record<string, { ok: boolean; checked_tables: number; mismatch: string[] }>>({})
const verifying = ref<Record<string, boolean>>({})

async function load() {
  loading.value = true; error.value = ''
  try {
    const r = await api<{ items: BackupJob[] }>('/tenant/plain-backups', {}, { silent: true })
    items.value = r.items || []
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

async function startExport() {
  if (!window.confirm('明文备份包含解密后的全部业务数据（含敏感字段），不再受字段加密保护。\n\n'
    + '确认已获授权并能够按高敏感文件安全保管？')) return
  exporting.value = true
  try {
    const r = await api<{ job_id: string; tables: number; total_rows: number }>('/tenant/plain-backups', {
      method: 'POST', body: JSON.stringify({ reason: '用户主动申请的明文离线备份' }),
    })
    toastSuccess(`导出完成：${r.tables} 张表 / ${r.total_rows} 行`)
    await load()
  } catch (e) {
    toastError((e as Error).message || '导出失败')
  } finally { exporting.value = false }
}

async function verify(job: BackupJob) {
  verifying.value[job.id] = true
  try {
    const r = await api<{ ok: boolean; checked_tables: number; mismatch: string[] }>(`/tenant/plain-backups/${job.id}/verify`, { method: 'POST' })
    verifyResults.value[job.id] = r
    if (r.ok) toastSuccess(`校验通过：${r.checked_tables} 张表`)
    else toastError(`校验未通过：${r.mismatch.length} 项不一致`)
  } catch (e) {
    toastError((e as Error).message || '校验失败')
  } finally { verifying.value[job.id] = false }
}

async function download(job: BackupJob) {
  try {
    const t = await api<{ token: string }>(`/tenant/plain-backups/${job.id}/download-ticket`, { method: 'POST' })
    /* 取包即作废：直接触发浏览器下载 */
    const link = document.createElement('a')
    link.href = `/api/v1/tenant/plain-backups/${job.id}/download?token=${encodeURIComponent(t.token)}`
    link.download = `plaintext-backup-${job.id}.zip`
    document.body.appendChild(link)
    link.click()
    link.remove()
    toastSuccess('下载已开始；该备份包的一次性凭证已作废')
    setTimeout(() => { void load() }, 1500)
  } catch (e) {
    toastError((e as Error).message || '下载失败')
  }
}

function fmtTime(micros?: number | null) {
  if (!micros) return '—'
  return new Date(micros / 1000).toLocaleString('zh-CN')
}

onMounted(load)
</script>

<template>
  <div class="page-header">
    <div><h1>明文离线备份</h1><p>从密文库解密导出本厂完整明文备份（JSONL + 校验清单）；一次性下载，取包即作废</p></div>
    <div class="header-actions"><button class="button" :disabled="exporting" @click="startExport">{{ exporting ? '导出中…' : '发起明文导出' }}</button></div>
  </div>

  <div class="notice">
    备份包含解密后的身份证号、银行卡、手机号等敏感数据，<strong>不再受字段级加密保护</strong>（设计 3.9 受控例外）。
    请显式授权后导出、完成校验、一次性下载并离线安全保管；下载凭证 10 分钟内有效且取包即作废。
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>

  <div class="table-wrap">
    <table>
      <thead><tr>
        <th>任务 ID</th><th>表数</th><th>行数</th><th>快照时间</th><th>状态</th><th>下载</th><th>操作</th>
      </tr></thead>
      <tbody>
        <tr v-if="loading"><td colspan="7">加载中…</td></tr>
        <tr v-else-if="!items.length"><td colspan="7">暂无导出记录</td></tr>
        <tr v-for="j in items" :key="j.id">
          <td class="mono">{{ j.id.slice(0, 13) }}…</td>
          <td>{{ j.table_count }}</td>
          <td>{{ j.total_rows }}</td>
          <td>{{ fmtTime(j.snapshot_at) }}</td>
          <td>
            <span class="status" :class="j.status === 'succeeded' ? 'active' : 'pending'">
              {{ j.status === 'succeeded' ? '成功' : j.status }}{{ j.error ? '：' + j.error : '' }}
            </span>
          </td>
          <td><span class="status" :class="j.downloaded ? '' : 'active'">{{ j.downloaded ? '已取包' : '未下载' }}</span></td>
          <td class="row-actions">
            <button class="button link" :disabled="verifying[j.id]" @click="verify(j)">{{ verifying[j.id] ? '校验中…' : '校验' }}</button>
            <button v-if="j.status === 'succeeded' && !j.downloaded" class="button link" @click="download(j)">下载</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <div v-for="(v, id) in verifyResults" :key="id" class="notice" :class="v.ok ? 'success' : 'error'">
    {{ id.slice(0, 13) }}… 校验{{ v.ok ? `通过（${v.checked_tables} 张表）` : '未通过' }}
    <ul v-if="!v.ok && v.mismatch.length"><li v-for="m in v.mismatch" :key="m">{{ m }}</li></ul>
  </div>
</template>

<style scoped>
.mono{font-family:ui-monospace,Consolas,monospace;font-size:12px}
.row-actions{white-space:nowrap}
.row-actions .button.link{padding:2px 8px}
.notice ul{margin:6px 0 0;padding-left:18px}
</style>
