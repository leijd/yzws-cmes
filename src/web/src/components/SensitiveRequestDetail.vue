<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, formatTime } from '../api'
import ModalDialog from './ModalDialog.vue'

/* 敏感信息查看授权申请的「提醒详情」弹窗：展示请求信息，租户管理员可允许/拒绝。
 * 允许时后端签发授权码并短信下发；未接真实短信环境时授权码回显在申请人页面。 */
const props = defineProps<{ requestId: string; canReview?: boolean }>()
const emit = defineEmits<{ close: []; reviewed: [] }>()

const detail = ref<any>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const statusLabels: Record<string, string> = { pending: '待审批', approved: '已允许', rejected: '已拒绝' }

async function load() {
  loading.value = true; error.value = ''
  try {
    detail.value = await api(`/sensitive-view-requests/${encodeURIComponent(props.requestId)}`)
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function review(action: 'approve' | 'reject') {
  busy.value = true; error.value = ''; notice.value = ''
  try {
    const res = await api<any>(`/sensitive-view-requests/${encodeURIComponent(props.requestId)}/${action}`, {
      method: 'POST', body: JSON.stringify({}),
    })
    notice.value = res.message || (action === 'approve' ? '已允许申请并签发授权码' : '已拒绝申请')
    await load()
    emit('reviewed')
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <ModalDialog title="敏感信息查看授权码申请 · 详情" @close="emit('close')">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div v-if="notice" class="notice success">{{ notice }}</div>
    <div v-if="loading" class="text-muted">正在加载申请信息…</div>
    <template v-else-if="detail">
      <div class="notice">有人向租户管理员发出查看敏感信息授权码申请，请核对以下请求信息后决定是否允许。</div>
      <dl style="display:grid;grid-template-columns:112px 1fr;gap:10px 14px;margin:14px 0;font-size:13px">
        <dt class="text-muted" style="margin:0">申请人</dt><dd style="margin:0">{{ detail.requester_name || detail.requester_user_id }}</dd>
        <dt class="text-muted" style="margin:0">接收手机号</dt><dd style="margin:0">{{ detail.requester_phone || '未登记' }}</dd>
        <dt class="text-muted" style="margin:0">目标员工</dt><dd style="margin:0">{{ detail.employee_name || detail.employee_id }}</dd>
        <dt class="text-muted" style="margin:0">申请字段</dt><dd style="margin:0">{{ detail.field_label || '员工全部敏感信息' }}</dd>
        <dt class="text-muted" style="margin:0">申请事由</dt><dd style="margin:0">{{ detail.reason || '—' }}</dd>
        <dt class="text-muted" style="margin:0">申请时间</dt><dd style="margin:0">{{ formatTime(detail.created_at) }}</dd>
        <dt class="text-muted" style="margin:0">当前状态</dt><dd style="margin:0"><span class="status" :class="detail.status === 'approved' ? 'approved' : detail.status === 'rejected' ? 'cancelled' : 'pending'">{{ statusLabels[detail.status] || detail.status }}</span></dd>
      </dl>
      <div v-if="detail.sms_sent" class="notice success">授权码已通过短信发送至申请人手机（30 分钟内有效）。</div>
      <div v-else-if="detail.demo_code_visible" class="notice success" style="display:flex;flex-wrap:wrap;align-items:center;gap:8px">
        <span>当前未连接真实短信环境，演示模式授权码：</span>
        <strong style="font-size:18px;letter-spacing:2px;user-select:all">{{ detail.demo_code }}</strong>
        <span class="text-muted" style="font-size:12px">已在申请人页面显示，30 分钟内有效</span>
      </div>
      <div v-else-if="detail.code_expired" class="notice">该申请签发的授权码已过期，可让申请人重新发起申请。</div>
    </template>
    <template #footer>
      <button class="button secondary" @click="emit('close')">关闭</button>
      <template v-if="canReview && detail && detail.status === 'pending'">
        <button class="button secondary" :disabled="busy" @click="review('reject')">拒绝</button>
        <button class="button" :disabled="busy" @click="review('approve')">{{ busy ? '处理中…' : '允许并签发授权码' }}</button>
      </template>
    </template>
  </ModalDialog>
</template>
