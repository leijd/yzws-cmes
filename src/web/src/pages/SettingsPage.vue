<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import PageState from '../components/PageState.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('settings:write'))

const settings = ref<any[]>([])
const sync = ref<any>({})
const loading = ref(false)
const error = ref('')
const notice = ref('')
function value(domain: string, key: string) { return settings.value.find(s => s.setting_domain === domain && s.setting_key === key)?.setting_value }
async function load() {
  loading.value = true; error.value = ''
  try { const [settingData, syncData] = await Promise.all([api<{ items: any[] }>('/feature-settings'), api<any>('/sync/status')]); settings.value = settingData.items; sync.value = syncData } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function update(key: string, newValue: boolean) {
  const current = settings.value.find(s => s.setting_domain === 'report' && s.setting_key === key)
  if (!current) return
  error.value = ''
  try { await api(`/feature-settings/report/${key}`, { method: 'PUT', body: JSON.stringify({ value: newValue, version: current.version, reason: '管理页面调整报工规则' }) }); notice.value = '设置已保存为新版本，不追溯修改历史任务或报工'; await load() } catch (e) { error.value = (e as Error).message; await load() }
}
/* 门户内容：本厂 logo 与欢迎标题/欢迎语（留空则使用平台默认内容） */
const portal = ref<any>(null)
const portalForm = reactive({ logo_url: '', welcome_title: '', welcome_text: '' })
const portalSaving = ref(false)
const portalNotice = ref('')
async function loadPortal() {
  try {
    const r = await api<any>('/tenant-portal/content', {}, { silent: true })
    portal.value = r
    portalForm.logo_url = r.logo_url || ''
    portalForm.welcome_title = r.welcome_title || ''
    portalForm.welcome_text = r.welcome_text || ''
  } catch { /* 无权限时隐藏表单 */ }
}
async function savePortal(reset = false) {
  portalSaving.value = true; portalNotice.value = ''
  try {
    const body = reset
      ? { logo_url: '', welcome_title: '', welcome_text: '', reason: '恢复平台默认门户内容' }
      : { ...portalForm, reason: '管理页面调整门户内容' }
    await api('/tenant-portal/content', { method: 'PUT', body: JSON.stringify(body) })
    portalNotice.value = reset ? '已恢复平台默认内容' : '门户内容已保存，门户页下次打开即生效'
    await loadPortal()
  } catch (e) { error.value = (e as Error).message } finally { portalSaving.value = false }
}
onMounted(() => { void load(); void loadPortal() })
</script>
<template>
  <div class="page-header"><div><h1>功能设置</h1><p>配置按版本生效并同步，历史任务与报工快照保持不变</p></div><button class="button secondary" @click="load">刷新</button></div><div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <div class="dashboard-grid"><section class="panel" style="grid-column:1 / -1"><div class="panel-header"><h2>制衣厂门户内容</h2><span class="status active">{{ portal?.edition === 'trial' ? '免费体验版' : '正式版' }}</span></div><div class="panel-body"><div v-if="portalNotice" class="notice success">{{ portalNotice }}</div><p class="text-muted" style="margin-top:0">门户页（登录后进入的欢迎页）顶部显示本厂名称与 logo；没有 logo 时使用平台默认 logo。欢迎标题与欢迎语留空即使用平台默认内容（当前默认标题：{{ portal?.default_title || '—' }}）。</p><div class="form-grid"><div class="field"><label>logo 网址</label><input v-model.trim="portalForm.logo_url" :disabled="!canWrite" placeholder="https://… 或 /uploads/logo.png（留空用平台默认）"></div><div class="field"><label>欢迎标题</label><input v-model.trim="portalForm.welcome_title" :disabled="!canWrite" maxlength="60" :placeholder="portal?.default_title || ''"></div><div class="field full"><label>欢迎语</label><textarea v-model.trim="portalForm.welcome_text" :disabled="!canWrite" rows="3" maxlength="400" :placeholder="portal?.default_text || ''"></textarea></div></div><div style="display:flex;gap:10px;margin-top:14px"><button class="button" :disabled="!canWrite||portalSaving" @click="savePortal(false)">{{ portalSaving ? '保存中…' : '保存门户内容' }}</button><button class="button secondary" :disabled="!canWrite||portalSaving" @click="savePortal(true)">恢复平台默认</button></div></div></section><section class="panel"><div class="panel-header"><h2>报工规则</h2><span class="status active">版本化配置</span></div><div class="panel-body"><PageState :loading="loading" /><div v-if="!loading" class="progress-list"><div style="display:flex;align-items:center;justify-content:space-between;gap:20px"><div><strong>上道工序数量控制</strong><p class="text-muted">累计报工不得超过上道合格可用量</p></div><input type="checkbox" :disabled="!canWrite" :checked="value('report','check_previous_process')" @change="update('check_previous_process', ($event.target as HTMLInputElement).checked)"></div><div style="display:flex;align-items:center;justify-content:space-between;gap:20px"><div><strong>正常报工自动审批</strong><p class="text-muted">仅无不良的正常报工由系统规则自动通过</p></div><input type="checkbox" :disabled="!canWrite" :checked="value('report','auto_approve')" @change="update('auto_approve', ($event.target as HTMLInputElement).checked)"></div><div class="notice">超量、越序和任务状态校验始终在服务端事务中执行，不能通过修改前端绕过。</div></div></div></section>
    <section class="panel"><div class="panel-header"><h2>节点与同步</h2><span class="status active">在线</span></div><div class="panel-body"><div class="quantity-summary"><span>待同步<strong>{{ sync.outbox_pending ?? 0 }}</strong></span><span>冲突<strong>{{ sync.conflicts ?? 0 }}</strong></span><span>附件待同步<strong>{{ sync.attachment_pending ?? 0 }}</strong></span><span>节点<strong style="font-size:12px">{{ sync.node_id || '—' }}</strong></span></div><p class="text-muted">业务写入与 outbox 在同一事务提交。同步中的数据不会显示为“已云端同步”。</p></div></section></div>
</template>
