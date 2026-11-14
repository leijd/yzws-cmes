<template>
  <BrandNavBar title="我的报工" />
  <view class="page">
    <view class="tabs">
      <view
        v-for="t in tabs"
        :key="t.value"
        class="tab"
        :class="{ active: status === t.value }"
        @click="switchTab(t.value)"
      >
        {{ t.label }}
      </view>
    </view>

    <view v-if="!loading && !items.length" class="empty">暂无报工记录</view>

    <view v-for="item in items" :key="item.id" class="card">
      <view class="row between">
        <text class="process">{{ item.process_name }}</text>
        <text class="status" :class="item.review_status">{{
          reviewStatusText[item.review_status] || item.review_status
        }}</text>
      </view>
      <view class="row">
        <text class="label">数量</text>
        <text class="value"
          >合格 {{ item.good_qty }} / 次品 {{ item.defect_qty }}</text
        >
      </view>
      <view class="row">
        <text class="label">时间</text>
        <text class="value">{{ formatDateTime(item.occurred_at) }}</text>
      </view>
      <view v-if="item.piecework_amount" class="row">
        <text class="label">计件金额</text>
        <text class="value amount">¥ {{ item.piecework_amount }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { auth } from "../../store/auth";
import { formatDateTime, reviewStatusText } from "../../utils/format";

interface ReportItem {
  id: string;
  process_name: string;
  good_qty: number;
  defect_qty: number;
  review_status: string;
  occurred_at: number;
  piecework_amount?: string | null;
}

const tabs = [
  { label: "全部", value: "" },
  { label: "待审批", value: "pending" },
  { label: "已通过", value: "approved" },
  { label: "已驳回", value: "rejected" },
];

const status = ref("");
const items = ref<ReportItem[]>([]);
const loading = ref(false);

async function load() {
  if (!auth.user?.employee_id) {
    items.value = [];
    return;
  }
  loading.value = true;
  try {
    const res = await request<{ items: ReportItem[] }>("/work-reports", {
      data: { employee_id: auth.user.employee_id, status: status.value },
      silent: true,
    });
    items.value = res.items || [];
  } catch {
    items.value = [];
  } finally {
    loading.value = false;
  }
}

function switchTab(value: string) {
  status.value = value;
  load();
}

onShow(() => {
  if (!auth.user) {
    auth.fetchMe().then(load);
  } else {
    load();
  }
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 24rpx 32rpx;
  box-sizing: border-box;
}
.tabs {
  display: flex;
  background: #fff;
  border-radius: 12rpx;
  padding: 8rpx;
  margin-bottom: 24rpx;
}
.tab {
  flex: 1;
  text-align: center;
  font-size: 26rpx;
  color: #718096;
  padding: 14rpx 0;
  border-radius: 8rpx;
}
.tab.active {
  background: #2b6cb0;
  color: #fff;
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
  padding: 6rpx 0;
  font-size: 26rpx;
}
.between {
  justify-content: space-between;
  border-bottom: 1px solid #edf2f7;
  padding-bottom: 12rpx;
  margin-bottom: 10rpx;
}
.process {
  font-size: 30rpx;
  font-weight: 600;
  color: #2d3748;
}
.status {
  font-size: 24rpx;
}
.status.pending {
  color: #d69e2e;
}
.status.approved {
  color: #38a169;
}
.status.rejected {
  color: #e53e3e;
}
.label {
  color: #718096;
  width: 150rpx;
}
.value {
  color: #2d3748;
}
.amount {
  color: #38a169;
  font-weight: 600;
}
</style>
