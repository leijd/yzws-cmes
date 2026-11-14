<template>
  <BrandNavBar title="提醒详情" />
  <view class="page">
    <!-- 提醒信息 -->
    <view class="card">
      <view class="card-title">{{ title || "提醒详情" }}</view>
      <text class="muted">{{ typeLabel }} · {{ formatDate(time) }}</text>
      <text class="body">{{ body || "暂无内容" }}</text>
    </view>

    <!-- 敏感信息查看授权申请：请求信息 + 允许/拒绝 -->
    <template v-if="isGrantRequest">
      <view v-if="loading" class="empty">正在加载请求信息…</view>
      <view v-else-if="loadError" class="empty error">{{ loadError }}</view>
      <template v-else-if="detail">
        <view class="card">
          <view class="card-title">请求信息</view>
          <view class="kv"><text class="k">申请人</text><text class="v">{{ detail.requester_name || detail.requester_user_id }}</text></view>
          <view class="kv"><text class="k">接收手机号</text><text class="v">{{ detail.requester_phone || "未登记" }}</text></view>
          <view class="kv"><text class="k">目标员工</text><text class="v">{{ detail.employee_name || detail.employee_id }}</text></view>
          <view class="kv"><text class="k">申请字段</text><text class="v">{{ detail.field_label || "员工全部敏感信息" }}</text></view>
          <view class="kv"><text class="k">申请事由</text><text class="v">{{ detail.reason || "—" }}</text></view>
          <view class="kv"><text class="k">申请时间</text><text class="v">{{ formatDate(detail.created_at) }}</text></view>
          <view class="kv"><text class="k">当前状态</text><text class="v">{{ statusLabels[detail.status] || detail.status }}</text></view>
        </view>

        <view v-if="notice" class="card notice-card">{{ notice }}</view>
        <view v-if="detail.demo_code_visible" class="card notice-card">
          <text class="muted">当前未连接真实短信环境，演示模式授权码（已在申请人页面显示）：</text>
          <text class="code" selectable>{{ detail.demo_code }}</text>
        </view>

        <view v-if="canReview && detail.status === 'pending'" class="actions">
          <button class="btn btn-outline" :disabled="busy" @click="review('reject')">拒绝</button>
          <button class="btn btn-solid" :disabled="busy" @click="review('approve')">{{ busy ? "处理中…" : "允许并签发授权码" }}</button>
        </view>
        <view v-else-if="!canReview" class="card"><text class="muted">只有租户管理员可以审批该申请。</text></view>
      </template>
    </template>

    <view v-if="!isGrantRequest" class="card"><text class="muted">该提醒无需审批，查看详情后可直接返回。</text></view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { formatDate } from "../../utils/format";
import { auth } from "../../store/auth";

const id = ref("");
const title = ref("");
const body = ref("");
const type = ref("");
const time = ref(0);
const ownerId = ref("");
const detail = ref<any>(null);
const loading = ref(false);
const loadError = ref("");
const notice = ref("");
const busy = ref(false);

const isGrantRequest = computed(() => type.value === "sensitive_view_request");
const canReview = computed(() => auth.hasPerm("identity:write"));
const typeLabels: Record<string, string> = {
  sensitive_view_request: "敏感信息授权申请",
  sensitive_view_request_result: "授权申请结果",
};
const statusLabels: Record<string, string> = {
  pending: "待审批",
  approved: "已允许",
  rejected: "已拒绝",
};
const typeLabel = computed(() => typeLabels[type.value] || type.value || "提醒");

async function loadDetail() {
  if (!isGrantRequest.value || !ownerId.value) return;
  loading.value = true;
  loadError.value = "";
  try {
    detail.value = await request<any>(`/sensitive-view-requests/${encodeURIComponent(ownerId.value)}`, { silent: true });
  } catch (e) {
    loadError.value = (e as Error).message || "请求信息加载失败";
  } finally {
    loading.value = false;
  }
}

function review(action: "approve" | "reject") {
  const content = action === "approve"
    ? "允许后将签发敏感信息查看授权码并发送给申请人，确定允许吗？"
    : "确定拒绝该敏感信息查看授权码申请吗？";
  uni.showModal({
    title: action === "approve" ? "允许申请" : "拒绝申请",
    content,
    success: async (res) => {
      if (!res.confirm) return;
      busy.value = true;
      try {
        const result = await request<any>(`/sensitive-view-requests/${encodeURIComponent(ownerId.value)}/${action}`, {
          method: "POST",
          data: {},
        });
        notice.value = result.message || (action === "approve" ? "已允许申请" : "已拒绝申请");
        await loadDetail();
        uni.showToast({ title: action === "approve" ? "已允许" : "已拒绝", icon: "none" });
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "操作失败", icon: "none" });
      } finally {
        busy.value = false;
      }
    },
  });
}

onLoad((options: any) => {
  id.value = options?.id || "";
  title.value = decodeURIComponent(options?.title || "");
  body.value = decodeURIComponent(options?.body || "");
  type.value = decodeURIComponent(options?.type || "");
  time.value = Number(options?.time || 0);
  ownerId.value = decodeURIComponent(options?.owner_id || "");
  if (isGrantRequest.value) void loadDetail();
});
</script>

<style scoped>
.page { min-height: 100vh; background: #f5f7fa; padding: 24rpx 32rpx; box-sizing: border-box; }
.card { background: #fff; border-radius: 16rpx; padding: 24rpx 32rpx; margin-bottom: 20rpx; }
.card-title { font-size: 30rpx; font-weight: 600; color: #2d3748; margin-bottom: 12rpx; }
.body { display: block; font-size: 26rpx; color: #475569; margin-top: 16rpx; line-height: 1.7; }
.muted { font-size: 24rpx; color: #94a3b8; }
.kv { display: flex; padding: 12rpx 0; border-bottom: 1px solid #f1f5f9; font-size: 26rpx; }
.k { width: 180rpx; color: #94a3b8; }
.v { flex: 1; color: #2d3748; }
.empty { text-align: center; color: #a0aec0; font-size: 28rpx; padding: 80rpx 0; }
.error { color: #b91c1c; }
.notice-card { background: #ecfdf5; color: #047857; font-size: 26rpx; line-height: 1.7; }
.code { display: block; font-size: 40rpx; font-weight: 700; letter-spacing: 4rpx; margin-top: 12rpx; color: #065f46; }
.actions { display: flex; gap: 20rpx; margin-top: 12rpx; }
.actions .btn { flex: 1; height: 80rpx; line-height: 80rpx; border-radius: 12rpx; font-size: 28rpx; }
.btn-solid { background: #2563eb; color: #fff; }
.btn-outline { background: #fff; color: #475569; border: 1px solid #e2e8f0; }
</style>
