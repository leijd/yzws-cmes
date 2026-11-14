<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, formatTime } from '../api'
import PageState from '../components/PageState.vue'
import SensitiveRequestDetail from '../components/SensitiveRequestDetail.vue'
import { usePermissions } from '../permissions'

/* 租户管理员的「敏感信息查看授权申请」页面：列表 + 详情弹窗审批。
 * 申请人发起申请时也会写入提醒列表，可在「消息与会话」里点详情直接处理。 */
const { has } = usePermissions()
const canReview = computed(() => has('identity:write'))
const items = ref<any[]>([])
const pendingCount = ref(0)
const loading = ref(false)
const error = ref('')
const status = ref('pending')
const detailId = ref('')
const statusLabels: Record<string, string> = { pending: '待审批', approved: '已允许', rejected: '已拒绝' }
const filters = [
  { value: 'pending', label: '待审批' },
  { value: 'approved', label: '已允许' },
  { value: 'rejected', label: '已拒绝' },
  { value: 'all', label: '全部' },
]

async function load() {
  loading.value = true; error.value = ''
  try {
    const res = await api<{ items: any[]; pending_count: number }>(`/sensitive-view-requests?status=${status.value}`)
    items.value = res.items || []
    pendingCount.value = res.pending_count || 0
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openDetail(item: any) { detailId.value = item.id }
function onReviewed() { detailId.value = ''; void load() }
onMounted(load)
</script>

<template>
  <div class="page-header">
    <div><h1>敏感信息授权申请</h1><p>审批员工敏感信息查看授权码申请，允许后向申请人手机下发授权码</p></div>
    <div class="header-actions"><span v-if="pendingCount > 0" class="text-muted">待审批 {{ pendingCount }} 条</span><button class="button secondary" @click="load">刷新</button></div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="!canReview" class="notice">只有租户管理员可以审批授权申请。</div>
  <section class="panel">
    <div class="toolbar">
      <button v-for="f in filters" :key="f.value" class="button secondary small" :class="{ 'text-danger': status === f.value }" @click="status = f.value; load()">{{ f.label }}</button>
    </div>
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap">
      <table>
        <thead><tr><th>申请人</th><th>接收手机号</th><th>目标员工</th><th>申请字段</th><th>申请事由</th><th>申请时间</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td>{{ item.requester_name || item.requester_user_id }}</td>
            <td class="text-muted">{{ item.requester_phone || '未登记' }}</td>
            <td><strong>{{ item.employee_name }}</strong></td>
            <td>{{ item.field_label || '员工全部敏感信息' }}</td>
            <td class="text-muted">{{ item.reason || '—' }}</td>
            <td>{{ formatTime(item.created_at) }}</td>
            <td><span class="status" :class="item.status === 'approved' ? 'approved' : item.status === 'rejected' ? 'cancelled' : 'pending'">{{ statusLabels[item.status] || item.status }}</span></td>
            <td><button class="button link" @click="openDetail(item)">查看详情</button></td>
          </tr>
        </tbody>
      </table>
      <PageState :empty="!items.length" />
    </div>
  </section>
  <SensitiveRequestDetail v-if="detailId" :request-id="detailId" :can-review="canReview" @close="detailId = ''" @reviewed="onReviewed" />
</template>
