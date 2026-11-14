<template>
  <BrandNavBar title="敏感信息授权申请" />
  <view class="page">
    <view v-if="!canReview" class="empty">只有租户管理员可以审批授权申请</view>

    <template v-else>
      <!-- 状态筛选 -->
      <view class="tabs">
        <view
          v-for="f in filters"
          :key="f.value"
          class="tab"
          :class="{ active: status === f.value }"
          @click="switchTab(f.value)"
        >
          <text>{{ f.label }}</text>
          <text v-if="f.value === 'pending' && pendingCount > 0" class="badge">{{ pendingCount }}</text>
        </view>
      </view>

      <view v-if="error" class="card error-card">{{ error }}</view>

      <view v-if="loading" class="empty">加载中…</view>
      <view v-else-if="!items.length" class="empty">暂无{{ statusLabel }}申请</view>

      <view v-for="item in items" :key="item.id" class="card" @click="openDetail(item)">
        <view class="row between">
          <text class="who">{{ item.requester_name || item.requester_user_id }}</text>
          <text class="status" :class="item.status">{{ statusLabels[item.status] || item.status }}</text>
        </view>
        <view class="row"><text class="label">目标员工</text><text class="value">{{ item.employee_name || item.employee_id }}</text></view>
        <view class="row"><text class="label">申请字段</text><text class="value">{{ item.field_label || "员工全部敏感信息" }}</text></view>
        <view class="row"><text class="label">接收手机</text><text class="value">{{ item.requester_phone || "未登记" }}</text></view>
        <view class="row"><text class="label">申请事由</text><text class="value">{{ item.reason || "—" }}</text></view>
        <view class="row"><text class="label">申请时间</text><text class="value">{{ formatDateTime(item.created_at) }}</text></view>
        <view class="row action-row">
          <text class="hint">{{ item.status === "pending" ? "点击查看详情并审批" : "点击查看审批结果" }}</text>
          <text class="arrow">›</text>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { onPullDownRefresh, onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { formatDateTime } from "../../utils/format";
import { auth } from "../../store/auth";

/* 租户管理员的「敏感信息授权申请」列表：待审批/已允许/已拒绝/全部，
 * 点击进入 pages/notify/detail 查看请求信息并审批（允许后签发授权码下发）。 */
interface ViewRequestItem {
  id: string;
  requester_user_id: string;
  requester_name?: string;
  requester_phone?: string;
  employee_id: string;
  employee_name?: string;
  field_key?: string;
  field_label?: string;
  reason?: string;
  status: string;
  created_at: number;
}

const canReview = computed(() => auth.hasPerm("identity:write"));
const items = ref<ViewRequestItem[]>([]);
const pendingCount = ref(0);
const loading = ref(false);
const error = ref("");
const status = ref("pending");

const filters = [
  { value: "pending", label: "待审批" },
  { value: "approved", label: "已允许" },
  { value: "rejected", label: "已拒绝" },
  { value: "all", label: "全部" },
];
const statusLabels: Record<string, string> = { pending: "待审批", approved: "已允许", rejected: "已拒绝" };
const statusLabel = computed(() => filters.find((f) => f.value === status.value)?.label || "");

async function load() {
  if (!canReview.value) return;
  loading.value = true;
  error.value = "";
  try {
    const res = await request<{ items: ViewRequestItem[]; pending_count: number }>(
      `/sensitive-view-requests?status=${status.value}`,
      { silent: true }
    );
    items.value = res.items || [];
    pendingCount.value = res.pending_count || 0;
  } catch (e) {
    items.value = [];
    error.value = (e as Error).message || "加载失败";
  } finally {
    loading.value = false;
  }
}

function switchTab(value: string) {
  if (status.value === value) return;
  status.value = value;
  items.value = [];
  load();
}

function openDetail(item: ViewRequestItem) {
  const body = `${item.requester_name || item.requester_user_id} 申请查看员工「${item.employee_name || item.employee_id}」的${item.field_label || "敏感信息"}`;
  uni.navigateTo({
    url:
      `/pages/notify/detail?type=sensitive_view_request` +
      `&owner_id=${encodeURIComponent(item.id)}` +
      `&title=${encodeURIComponent("敏感信息授权申请")}` +
      `&body=${encodeURIComponent(body)}` +
      `&time=${item.created_at || 0}`,
  });
}

onShow(() => {
  load();
});

onPullDownRefresh(async () => {
  await load();
  uni.stopPullDownRefresh();
});
</script>

<style scoped>
.page { min-height: 100vh; background: #f5f7fa; padding: 24rpx 32rpx; box-sizing: border-box; }
.tabs { display: flex; gap: 16rpx; margin-bottom: 20rpx; }
.tab {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8rpx;
  height: 72rpx;
  border-radius: 12rpx;
  background: #fff;
  color: #64748b;
  font-size: 26rpx;
}
.tab.active { background: #2563eb; color: #fff; }
.badge {
  min-width: 32rpx;
  height: 32rpx;
  line-height: 32rpx;
  padding: 0 8rpx;
  border-radius: 16rpx;
  background: #ef4444;
  color: #fff;
  font-size: 22rpx;
  text-align: center;
}
.card { background: #fff; border-radius: 16rpx; padding: 24rpx 32rpx; margin-bottom: 20rpx; }
.row { display: flex; padding: 6rpx 0; font-size: 26rpx; }
.between { justify-content: space-between; border-bottom: 1px solid #edf2f7; padding-bottom: 12rpx; margin-bottom: 10rpx; }
.who { font-size: 30rpx; font-weight: 600; color: #2d3748; }
.status { font-size: 24rpx; padding: 2rpx 14rpx; border-radius: 20rpx; }
.status.pending { background: #fff7ed; color: #c2410c; }
.status.approved { background: #ecfdf5; color: #047857; }
.status.rejected { background: #fef2f2; color: #b91c1c; }
.label { color: #718096; width: 150rpx; }
.value { flex: 1; color: #2d3748; word-break: break-all; }
.action-row { justify-content: space-between; align-items: center; margin-top: 12rpx; padding-top: 14rpx; border-top: 1px solid #edf2f7; }
.hint { font-size: 24rpx; color: #2563eb; }
.arrow { font-size: 32rpx; color: #cbd5e1; }
.empty { text-align: center; color: #a0aec0; font-size: 28rpx; padding: 120rpx 0; }
.error-card { background: #fef2f2; color: #b91c1c; font-size: 26rpx; line-height: 1.7; }
</style>
