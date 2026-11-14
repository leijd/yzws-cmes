<template>
  <BrandNavBar title="项目进度" />
  <view class="page">
    <view class="search">
      <input
        v-model.trim="keyword"
        placeholder="搜索项目号 / 款号"
        confirm-type="search"
        @confirm="load"
      />
      <button size="mini" @click="load">搜索</button>
    </view>

    <view v-if="!loading && !items.length" class="empty">暂无项目进度数据</view>

    <view
      v-for="item in items"
      :key="item.project_id"
      class="card"
      @click="openDetail(item)"
    >
      <view class="row between">
        <text class="code">{{ item.project_code }}</text>
        <text v-if="item.warning_level" class="warning" :class="item.warning_level">
          {{ warningText[item.warning_level] || item.warning_level }}
        </text>
      </view>
      <view class="row">
        <text class="pname">{{ item.product_name }}（{{ item.product_code }}）</text>
      </view>
      <view class="row meta">
        <text>{{ item.customer_name || "—" }}</text>
        <text v-if="item.workshop_name">{{ item.workshop_name }} {{ item.line_name }}</text>
      </view>
      <view class="progress-wrap">
        <view class="progress-bar">
          <view class="progress-fill" :style="{ width: ratioPercent(item.progress_ratio) }" />
        </view>
        <text class="ratio">{{ ratioPercent(item.progress_ratio) }}</text>
      </view>
      <view class="row meta">
        <text>订单 {{ item.order_qty }} / 入库 {{ item.received_qty }} / 出货 {{ item.shipped_qty }}</text>
        <text v-if="item.remaining_days !== null" :class="{ overdue: Number(item.remaining_days) < 0 }">
          {{ Number(item.remaining_days) < 0 ? `逾期 ${Math.abs(Number(item.remaining_days))} 天` : `剩 ${item.remaining_days} 天` }}
        </text>
      </view>
      <view v-if="item.current_node" class="node">当前节点：{{ item.current_node }}</view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface StyleProgress {
  project_id: string;
  project_code: string;
  product_code: string;
  product_name: string;
  customer_name: string;
  order_qty: number;
  cut_qty: number;
  wip_qty: number;
  received_qty: number;
  shipped_qty: number;
  progress_ratio: string | null;
  current_node: string;
  workshop_name: string;
  line_name: string;
  planned_delivery_date: string;
  remaining_days: number | null;
  warning_level: string;
  business_status: string;
}

const warningText: Record<string, string> = {
  normal: "正常",
  warning: "预警",
  danger: "延期风险",
};

const keyword = ref("");
const items = ref<StyleProgress[]>([]);
const loading = ref(false);

function ratioPercent(ratio: string | null): string {
  const n = Number(ratio || 0);
  return `${Math.min(100, Math.max(0, n * 100)).toFixed(1)}%`;
}

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: StyleProgress[] }>("/progress/styles", {
      data: keyword.value ? { project_code: keyword.value } : {},
      silent: true,
    });
    items.value = res.items || [];
  } catch {
    items.value = [];
  } finally {
    loading.value = false;
  }
}

function openDetail(item: StyleProgress) {
  uni.navigateTo({
    url: `/pages/progress/detail?id=${item.project_id}&code=${encodeURIComponent(item.project_code)}`,
  });
}

onShow(() => {
  load();
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 24rpx 32rpx;
  box-sizing: border-box;
}
.search {
  display: flex;
  gap: 16rpx;
  align-items: center;
  margin-bottom: 24rpx;
}
.search input {
  flex: 1;
  background: #fff;
  border-radius: 12rpx;
  padding: 16rpx 24rpx;
  font-size: 28rpx;
}
.empty {
  text-align: center;
  color: #a0aec0;
  font-size: 28rpx;
  padding: 120rpx 0;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 24rpx 32rpx;
  margin-bottom: 20rpx;
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
  font-size: 30rpx;
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
.pname {
  font-size: 28rpx;
  color: #2d3748;
}
.meta {
  justify-content: space-between;
  font-size: 24rpx;
  color: #718096;
}
.meta .overdue {
  color: #e53e3e;
  font-weight: 600;
}
.progress-wrap {
  display: flex;
  align-items: center;
  gap: 16rpx;
  margin: 12rpx 0 8rpx;
}
.progress-bar {
  flex: 1;
  height: 14rpx;
  background: #edf2f7;
  border-radius: 7rpx;
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, #4299e1, #2b6cb0);
  border-radius: 7rpx;
}
.ratio {
  font-size: 24rpx;
  font-weight: 600;
  color: #2b6cb0;
  width: 100rpx;
  text-align: right;
}
.node {
  font-size: 24rpx;
  color: #4a5568;
  background: #f5f7fa;
  border-radius: 8rpx;
  padding: 10rpx 16rpx;
  margin-top: 8rpx;
}
</style>
