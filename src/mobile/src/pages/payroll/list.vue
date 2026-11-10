<template>
  <view class="page">
    <view class="filter-bar">
      <view class="period">
        <button size="mini" @click="shiftMonth(-1)">‹</button>
        <input
          v-model.trim="period"
          class="period-input"
          maxlength="7"
          @blur="normalizePeriod"
          @confirm="load"
        />
        <button size="mini" @click="shiftMonth(1)">›</button>
      </view>
      <picker :range="statusOptions" range-key="label" @change="onStatusChange">
        <view class="status-picker">
          {{ currentStatusLabel }} ▾
        </view>
      </picker>
    </view>

    <view class="summary card">
      <view class="summary-item">
        <text class="num">{{ items.length }}</text>
        <text class="cap">工资单</text>
      </view>
      <view class="summary-item">
        <text class="num">¥ {{ totalNet }}</text>
        <text class="cap">实发合计</text>
      </view>
      <view class="summary-item">
        <text class="num unpaid">¥ {{ totalOutstanding }}</text>
        <text class="cap">未发合计</text>
      </view>
    </view>

    <view v-if="!loading && !items.length" class="empty">该月没有工资单</view>

    <view
      v-for="item in items"
      :key="item.id"
      class="card"
      @click="openDetail(item)"
    >
      <view class="row between">
        <text class="name">{{ item.employee_name }}</text>
        <text class="status" :class="item.status">{{
          sheetStatusText[item.status] || item.status
        }}</text>
      </view>
      <view class="row">
        <text class="label">车间/小组</text>
        <text class="value">{{ item.workshop || "—" }} {{ item.production_line }}</text>
      </view>
      <view class="row">
        <text class="label">计件</text>
        <text class="value">¥ {{ item.piecework_amount }}（{{ item.piecework_qty }} 件）</text>
      </view>
      <view class="row between amounts">
        <text>实发 ¥{{ item.net_amount }}</text>
        <text v-if="Number(item.outstanding_amount) > 0" class="unpaid">
          未发 ¥{{ item.outstanding_amount }}
        </text>
        <text v-else class="paid">已结清</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface SheetItem {
  id: string;
  employee_no: string;
  employee_name: string;
  workshop: string;
  production_line: string;
  piecework_qty: string;
  piecework_amount: string;
  net_amount: string;
  paid_amount: string;
  outstanding_amount: string;
  status: string;
  signature_status: string;
}

const sheetStatusText: Record<string, string> = {
  draft: "待确认",
  confirmed: "已确认",
  partially_paid: "部分发放",
  paid: "已发放",
  locked: "已锁定",
};

const statusOptions = [
  { label: "全部状态", value: "" },
  { label: "待确认", value: "draft" },
  { label: "已确认", value: "confirmed" },
  { label: "部分发放", value: "partially_paid" },
  { label: "已发放", value: "paid" },
];

const period = ref(currentMonth());
const status = ref("");
const items = ref<SheetItem[]>([]);
const loading = ref(false);

const currentStatusLabel = computed(
  () => statusOptions.find((s) => s.value === status.value)?.label || "全部状态"
);
const totalNet = computed(() =>
  items.value.reduce((s, i) => s + (Number(i.net_amount) || 0), 0).toFixed(2)
);
const totalOutstanding = computed(() =>
  items.value
    .reduce((s, i) => s + (Number(i.outstanding_amount) || 0), 0)
    .toFixed(2)
);

function currentMonth(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

function normalizePeriod() {
  if (!/^\d{4}-\d{2}$/.test(period.value)) period.value = currentMonth();
  load();
}

function shiftMonth(delta: number) {
  const [y, m] = period.value.split("-").map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  period.value = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
  load();
}

function onStatusChange(e: { detail: { value: number } }) {
  status.value = statusOptions[e.detail.value].value;
  load();
}

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: SheetItem[] }>("/payroll-sheets", {
      data: { period: period.value, status: status.value },
      silent: true,
    });
    items.value = res.items || [];
  } catch {
    items.value = [];
  } finally {
    loading.value = false;
  }
}

function openDetail(item: SheetItem) {
  uni.navigateTo({
    url: `/pages/payroll/detail?id=${item.id}&name=${encodeURIComponent(item.employee_name)}&period=${period.value}`,
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
.filter-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20rpx;
}
.period {
  display: flex;
  align-items: center;
  gap: 12rpx;
  background: #fff;
  border-radius: 12rpx;
  padding: 8rpx 16rpx;
}
.period-input {
  width: 160rpx;
  text-align: center;
  font-size: 28rpx;
  color: #2d3748;
}
.status-picker {
  background: #fff;
  border-radius: 12rpx;
  padding: 14rpx 24rpx;
  font-size: 26rpx;
  color: #2d3748;
}
.summary {
  display: flex;
  margin-bottom: 20rpx;
}
.summary-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.num {
  font-size: 30rpx;
  font-weight: 700;
  color: #2b6cb0;
}
.num.unpaid {
  color: #d69e2e;
}
.cap {
  font-size: 22rpx;
  color: #718096;
  margin-top: 4rpx;
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
  font-size: 26rpx;
}
.between {
  justify-content: space-between;
}
.name {
  font-size: 30rpx;
  font-weight: 600;
  color: #2d3748;
}
.status {
  font-size: 24rpx;
  color: #718096;
}
.status.draft {
  color: #d69e2e;
}
.status.confirmed {
  color: #2b6cb0;
}
.status.partially_paid {
  color: #dd6b20;
}
.status.paid {
  color: #38a169;
}
.label {
  color: #718096;
  width: 150rpx;
}
.value {
  color: #2d3748;
}
.amounts {
  border-top: 1px solid #edf2f7;
  margin-top: 10rpx;
  padding-top: 14rpx;
  font-size: 28rpx;
  font-weight: 600;
  color: #2d3748;
}
.unpaid {
  color: #d69e2e;
}
.paid {
  color: #38a169;
  font-weight: 400;
  font-size: 24rpx;
}
</style>
