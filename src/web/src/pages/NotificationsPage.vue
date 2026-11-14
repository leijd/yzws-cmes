<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, formatTime } from '../api'
import PageState from '../components/PageState.vue'
import ModalDialog from '../components/ModalDialog.vue'
import SensitiveRequestDetail from '../components/SensitiveRequestDetail.vue'
import { usePermissions } from '../permissions'

const tab = ref<'notifications' | 'sessions'>('notifications')
const notifications = ref<any[]>([])
const sessions = ref<any[]>([])
const unread = ref(0)
const loading = ref(false)
const error = ref('')
const notice = ref('')
const readFilter = ref<'all' | 'unread'>('all')
/* 提醒详情：点消息标题弹窗显示提醒信息；敏感信息授权申请再点「详细」展示请求信息并审批 */
const { has } = usePermissions()
const canReview = computed(() => has('identity:write'))
const reminderItem = ref<any>(null)
const detailRequestId = ref('')
const typeLabels: Record<string, string> = {
  sensitive_view_request: '敏感信息授权申请',
  sensitive_view_request_result: '授权申请结果',
}
async function openReminder(item: any) {
  reminderItem.value = item
  if (!item.read) await markRead(item)
}
async function openDetail(item: any) {
  reminderItem.value = null
  if (!item.read) await markRead(item)
  if (item.type === 'sensitive_view_request') detailRequestId.value = item.owner_id || ''
  else reminderItem.value = item
}
function onReviewed() { detailRequestId.value = ''; void loadNotifications() }

async function loadNotifications() {
  loading.value = true; error.value = ''
  try {
    const query = readFilter.value === 'unread' ? '?read=false' : ''
    const data = await api<{ items: any[]; unread_count: number }>('/notifications' + query)
    notifications.value = data.items
    unread.value = data.unread_count
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function loadSessions() {
  loading.value = true; error.value = ''
  try { sessions.value = (await api<{ items: any[] }>('/sessions')).items } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function load() { if (tab.value === 'notifications') await loadNotifications(); else await loadSessions() }
async function markRead(item: any) {
  try { await api('/notifications/read', { method: 'POST', body: JSON.stringify({ ids: [item.id] }) }); await loadNotifications() } catch (e) { error.value = (e as Error).message }
}
async function markAllRead() {
  try { await api('/notifications/read', { method: 'POST', body: JSON.stringify({ all_before: Date.now() * 1000 }) }); notice.value = '已全部标记为已读'; await loadNotifications() } catch (e) { error.value = (e as Error).message }
}
async function revoke(item: any) {
  if (item.current && !window.confirm('这是当前会话，撤销后将立即退出，继续？')) return
  if (!item.current && !window.confirm('确认撤销该登录会话？')) return
  error.value = ''
  try { await api(`/sessions/${item.id}`, { method: 'DELETE' }); notice.value = '会话已撤销'; await loadSessions() } catch (e) { error.value = (e as Error).message }
}
function changeTab(value: 'notifications' | 'sessions') { tab.value = value; load() }
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>消息与会话</h1><p>待办通知与登录设备管理</p></div><div class="header-actions"><button class="button secondary" @click="load">刷新</button><button v-if="tab === 'notifications' && unread > 0" class="button" @click="markAllRead">全部已读</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel">
    <div class="tabs"><button :class="{ active: tab === 'notifications' }" @click="changeTab('notifications')">消息待办{{ unread > 0 ? `（${unread}）` : '' }}</button><button :class="{ active: tab === 'sessions' }" @click="changeTab('sessions')">登录会话</button></div>
    <div v-if="tab === 'notifications'" class="toolbar"><button class="button secondary small" :class="{ 'text-danger': readFilter === 'unread' }" @click="readFilter = readFilter === 'all' ? 'unread' : 'all'; loadNotifications()">{{ readFilter === 'all' ? '只看未读' : '查看全部' }}</button><span class="text-muted">未读 {{ unread }} 条</span></div>
    <PageState :loading="loading" />
    <div v-if="!loading && tab === 'notifications'" class="table-wrap"><table><thead><tr><th>类型</th><th>标题</th><th>内容</th><th>时间</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in notifications" :key="item.id"><td>{{ typeLabels[item.type] || item.type }}</td><td><button class="button link" style="font-weight:600;text-align:left" @click="openReminder(item)">{{ item.title }}</button></td><td class="text-muted">{{ item.body }}</td><td>{{ formatTime(item.created_at) }}</td><td><span class="status" :class="item.read ? 'approved' : 'pending'">{{ item.read ? '已读' : '未读' }}</span></td><td><button class="button link" @click="openDetail(item)">详细</button><button v-if="!item.read" class="button link" @click="markRead(item)">标记已读</button></td></tr></tbody></table><PageState :empty="!notifications.length" /></div>
    <div v-if="!loading && tab === 'sessions'" class="table-wrap"><table><thead><tr><th>节点</th><th>设备</th><th>创建时间</th><th>过期时间</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in sessions" :key="item.id"><td>{{ item.node_id }}</td><td class="text-muted">{{ item.device_info }}</td><td>{{ formatTime(item.created_at) }}</td><td>{{ formatTime(item.expires_at) }}</td><td><span class="status" :class="item.revoked_at ? 'cancelled' : 'active'">{{ item.revoked_at ? '已撤销' : (item.current ? '当前会话' : '有效') }}</span></td><td><button v-if="!item.revoked_at" class="button link text-danger" @click="revoke(item)">撤销</button></td></tr></tbody></table><PageState :empty="!sessions.length" /></div>
  </section>

  <!-- 网页版：点击提醒弹窗显示提醒信息，敏感信息授权申请可继续查看请求详情 -->
  <ModalDialog v-if="reminderItem" :title="reminderItem.title" @close="reminderItem = null">
    <div class="notice">{{ typeLabels[reminderItem.type] || reminderItem.type }} · {{ formatTime(reminderItem.created_at) }}</div>
    <p style="font-size:14px;line-height:1.8;white-space:pre-wrap">{{ reminderItem.body }}</p>
    <template #footer>
      <button v-if="reminderItem.type === 'sensitive_view_request'" class="button secondary" @click="openDetail(reminderItem)">查看请求详细</button>
      <button class="button" @click="reminderItem = null">知道了</button>
    </template>
  </ModalDialog>

  <SensitiveRequestDetail v-if="detailRequestId" :request-id="detailRequestId" :can-review="canReview" @close="detailRequestId = ''" @reviewed="onReviewed" />
</template>
