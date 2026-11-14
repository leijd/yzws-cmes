<template>
  <BrandNavBar title="项目详情" />
  <view class="page">
    <view v-if="detail" class="card head">
      <view class="row between">
        <text class="code">{{ projectCode }}</text>
        <text v-if="detail.warning_level" class="warning" :class="detail.warning_level">
          {{ warningText[detail.warning_level] || detail.warning_level }}
        </text>
      </view>
      <view class="big-ratio">{{ ratioPercent(detail.progress_ratio) }}</view>
      <view class="progress-bar">
        <view class="progress-fill" :style="{ width: ratioPercent(detail.progress_ratio) }" />
      </view>
      <view v-if="detail.current_node" class="node">当前节点：{{ detail.current_node }}</view>
    </view>

    <view v-if="detail" class="card">
      <view class="row between item">
        <text class="label">订单数量</text>
        <text class="value">{{ detail.order_qty }}</text>
      </view>
      <view class="row between item">
        <text class="label">裁剪数量</text>
        <text class="value">{{ detail.cut_qty }}</text>
      </view>
      <view class="row between item">
        <text class="label">在制数量</text>
        <text class="value">{{ detail.wip_qty }}</text>
      </view>
      <view class="row between item">
        <text class="label">入库数量</text>
        <text class="value">{{ detail.received_qty }}</text>
      </view>
      <view class="row between item">
        <text class="label">出货数量</text>
        <text class="value">{{ detail.shipped_qty }}</text>
      </view>
      <view class="row between item">
        <text class="label">计划交期</text>
        <text class="value">{{ detail.planned_delivery_date || "—" }}</text>
      </view>
      <view class="row between item">
        <text class="label">剩余天数</text>
        <text class="value" :class="{ overdue: Number(detail.remaining_days) < 0 }">
          <template v-if="detail.remaining_days !== null">
            {{ Number(detail.remaining_days) < 0 ? `逾期 ${Math.abs(Number(detail.remaining_days))} 天` : `${detail.remaining_days} 天` }}
          </template>
          <template v-else>—</template>
        </text>
      </view>
      <view v-if="detail.estimated_days" class="row between item">
        <text class="label">预计还需</text>
        <text class="value">{{ detail.estimated_days }} 天</text>
      </view>
    </view>

    <view v-if="detail" class="card">
      <text class="card-title">关联工单（{{ workOrders.length }}）</text>
      <view v-if="!workOrders.length" class="none">暂无工单</view>
      <view v-for="wo in workOrders" :key="wo.id" class="row between item">
        <text class="wo-code">{{ wo.work_order_code }}</text>
        <text class="wo-meta">
          {{ woStatusText[wo.status] || wo.status }} · 计划 {{ wo.planned_product_qty }}
        </text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface ProgressDetail {
  order_qty: number;
  cut_qty: number;
  wip_qty: number;
  received_qty: number;
  shipped_qty: number;
  progress_ratio: string | null;
  current_node: string;
  planned_delivery_date: string;
  remaining_days: number | null;
  estimated_days?: string | null;
  warning_level: string;
  business_status: string;
  work_orders: WorkOrder[];
}

interface WorkOrder {
  id: string;
  work_order_code: string;
  status: string;
  planned_product_qty: number;
  review_status: string;
}

const warningText: Record<string, string> = {
  normal: "正常",
  warning: "预警",
  danger: "延期风险",
};

const woStatusText: Record<string, string> = {
  draft: "草稿",
  active: "已下发",
  in_production: "生产中",
  paused: "暂停",
  completed: "已完工",
  cancelled: "已取消",
};

const detail = ref<ProgressDetail | null>(null);
const workOrders = ref<WorkOrder[]>([]);
const projectCode = ref("");

function ratioPercent(ratio: string | null): string {
  const n = Number(ratio || 0);
  return `${Math.min(100, Math.max(0, n * 100)).toFixed(1)}%`;
}

onLoad((options) => {
  const id = options?.id || "";
  projectCode.value = options?.code ? decodeURIComponent(options.code) : "";
  if (!id) return;
  request<ProgressDetail>(`/progress/styles/${id}`, { silent: true })
    .then((res) => {
      detail.value = res;
      workOrders.value = res.work_orders || [];
    })
    .catch(() => {
      detail.value = null;
    });
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 24rpx 32rpx;
  box-sizing: border-box;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 24rpx 32rpx;
  margin-bottom: 20rpx;
}
.head {
  background: linear-gradient(135deg, #2b6cb0, #2c5282);
}
.head .code {
  color: #fff;
}
.row {
  display: flex;
  padding: 4rpx 0;
}
.between {
  justify-content: space-between;
  align-items: center;
}
.code {
  font-size: 32rpx;
  font-weight: 700;
  color: #2b6cb0;
}
.warning {
  font-size: 22rpx;
  border-radius: 8rpx;
  padding: 2rpx 14rpx;
}
.warning.normal {
  background: #c6f6d5;
  color: #276749;
}
.warning.warning {
  background: #feebc8;
  color: #c05621;
}
.warning.danger {
  background: #fed7d7;
  color: #c53030;
}
.big-ratio {
  text-align: center;
  font-size: 64rpx;
  font-weight: 700;
  color: #fff;
  margin: 24rpx 0 12rpx;
}
.progress-bar {
  height: 16rpx;
  background: rgba(255, 255, 255, 0.25);
  border-radius: 8rpx;
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: #fff;
  border-radius: 8rpx;
}
.node {
  font-size: 24rpx;
  color: rgba(255, 255, 255, 0.85);
  margin-top: 16rpx;
}
.card-title {
  font-size: 28rpx;
  font-weight: 600;
  color: #2d3748;
  display: block;
  margin-bottom: 8rpx;
}
.item {
  border-bottom: 1px solid #edf2f7;
  padding: 14rpx 0;
  font-size: 27rpx;
}
.item:last-child {
  border-bottom: none;
}
.label {
  color: #718096;
}
.value {
  color: #2d3748;
  font-weight: 600;
}
.value.overdue {
  color: #e53e3e;
}
.none {
  color: #a0aec0;
  font-size: 26rpx;
  padding: 16rpx 0;
}
.wo-code {
  font-size: 28rpx;
  color: #2b6cb0;
  font-weight: 600;
}
.wo-meta {
  font-size: 24rpx;
  color: #718096;
}
</style>
