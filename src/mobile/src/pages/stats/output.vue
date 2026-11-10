<template>
  <view class="page">
    <view class="period-bar">
      <button size="mini" @click="shiftMonth(-1)">‹ 上月</button>
      <text class="period">{{ period }}</text>
      <button size="mini" @click="shiftMonth(1)">下月 ›</button>
    </view>

    <view class="summary card">
      <view class="summary-item">
        <text class="num">{{ employeeCount }}</text>
        <text class="cap">出勤人数</text>
      </view>
      <view class="summary-item">
        <text class="num">{{ totalOutput }}</text>
        <text class="cap">总产量（件）</text>
      </view>
    </view>

    <view v-if="!loading && !rows.length" class="empty">该月没有产量数据</view>

    <view v-for="row in rows" :key="row.name" class="card">
      <view class="row between">
        <text class="name">{{ row.name }}</text>
        <text class="total">{{ row.total }} 件</text>
      </view>
      <view class="daily">
        <view v-for="d in row.days" :key="d.date" class="day">
          <text class="day-date">{{ d.date.slice(5) }}</text>
          <text class="day-qty">{{ d.qty }}</text>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Row {
  name: string;
  total: number;
  days: { date: string; qty: number }[];
}

const period = ref(currentMonth());
const rows = ref<Row[]>([]);
const loading = ref(false);

const employeeCount = computed(() => rows.value.length);
const totalOutput = computed(() =>
  rows.value.reduce((sum, r) => sum + r.total, 0)
);

function currentMonth(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

function shiftMonth(delta: number) {
  const [y, m] = period.value.split("-").map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  period.value = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
  load();
}

async function load() {
  loading.value = true;
  try {
    const res = await request<{
      items: { name: string; values: Record<string, string> }[];
    }>("/statistics/output", {
      data: { period: period.value },
      silent: true,
    });
    rows.value = (res.items || [])
      .map((item) => {
        const days = Object.entries(item.values || {})
          .map(([date, qty]) => ({ date, qty: Number(qty) || 0 }))
          .sort((a, b) => (a.date < b.date ? -1 : 1));
        return { name: item.name, days, total: days.reduce((s, d) => s + d.qty, 0) };
      })
      .sort((a, b) => b.total - a.total);
  } catch {
    rows.value = [];
  } finally {
    loading.value = false;
  }
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
.period-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-radius: 12rpx;
  padding: 16rpx 24rpx;
  margin-bottom: 20rpx;
}
.period {
  font-size: 30rpx;
  font-weight: 600;
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
  font-size: 40rpx;
  font-weight: 700;
  color: #2b6cb0;
}
.cap {
  font-size: 24rpx;
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
}
.between {
  justify-content: space-between;
  margin-bottom: 12rpx;
}
.name {
  font-size: 30rpx;
  font-weight: 600;
  color: #2d3748;
}
.total {
  font-size: 28rpx;
  color: #2b6cb0;
  font-weight: 600;
}
.daily {
  display: flex;
  flex-wrap: wrap;
  gap: 12rpx;
}
.day {
  background: #f5f7fa;
  border-radius: 8rpx;
  padding: 8rpx 16rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.day-date {
  font-size: 20rpx;
  color: #a0aec0;
}
.day-qty {
  font-size: 26rpx;
  color: #2d3748;
  font-weight: 600;
}
</style>
