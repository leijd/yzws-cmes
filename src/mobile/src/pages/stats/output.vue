<template>
  <BrandNavBar title="产量查询" />
  <view class="page">
    <view class="chips">
      <view v-for="m in metrics" :key="m.value" class="chip" :class="{ on: metric === m.value }" @click="metric = m.value; load()">{{ m.label }}</view>
    </view>
    <view class="period-bar">
      <button size="mini" @click="shiftMonth(-1)">‹ 上月</button>
      <text class="period">{{ period }}</text>
      <button size="mini" @click="shiftMonth(1)" v-if="canNext">下月 ›</button>
      <text v-else style="width: 96rpx"></text>
    </view>

    <view class="summary card">
      <view class="summary-item">
        <text class="num">{{ rows.length }}</text>
        <text class="cap">统计人数</text>
      </view>
      <view class="summary-item">
        <text class="num">{{ totalValue }}</text>
        <text class="cap">{{ metricLabel }}合计</text>
      </view>
    </view>

    <view v-if="!loading && !rows.length" class="empty">该月没有{{ metricLabel }}数据</view>

    <view v-for="row in rows" :key="row.name" class="card">
      <view class="row between">
        <text class="name">{{ row.name }}</text>
        <text class="total">{{ fmtValue(row.total) }}</text>
      </view>
      <view class="daily">
        <view v-for="d in row.days" :key="d.date" class="day">
          <text class="day-date">{{ d.date.slice(5) }}</text>
          <text class="day-qty">{{ fmtValue(d.qty, true) }}</text>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface StatRow {
  name: string;
  total: number;
  days: { date: string; qty: number }[];
}

const metrics = [
  { label: "产量（件）", value: "output" },
  { label: "工时（秒）", value: "worktime" },
  { label: "工资（元）", value: "wages" },
];
const metric = ref("output");
const metricLabel = computed(() => metrics.find((m) => m.value === metric.value)?.label || "");

const period = ref(currentMonth());
const rows = ref<StatRow[]>([]);
const loading = ref(false);

const totalValue = computed(() => fmtValue(rows.value.reduce((s, r) => s + r.total, 0)));
const canNext = computed(() => period.value < currentMonth());

function currentMonth(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

function fmtValue(n: number, compact = false): string {
  if (metric.value === "worktime") {
    return n >= 3600 ? (n / 3600).toFixed(1) + "h" : compact ? `${n}s` : `${n} 秒`;
  }
  if (metric.value === "wages") return n.toFixed(2);
  return String(n);
}

function shiftMonth(delta: number) {
  const [y, m] = period.value.split("-").map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  const next = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
  if (delta > 0 && next > currentMonth()) return;
  period.value = next;
  load();
}

async function load() {
  loading.value = true;
  try {
    /* 统计接口返回 {rows:[{name, values:{"YYYY-MM-DD": "123"}}], columns:[...]} */
    const res = await request<{ rows: { name: string; values: Record<string, string> }[] }>(
      `/statistics/${metric.value}?period=${period.value}`,
      { silent: true }
    );
    rows.value = (res.rows || [])
      .map((item) => {
        const days = Object.entries(item.values || {})
          .map(([date, qty]) => ({ date, qty: Math.round(Number(qty) || 0) }))
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
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 16rpx;
  margin-bottom: 20rpx;
}
.chip {
  padding: 12rpx 28rpx;
  border-radius: 999rpx;
  font-size: 24rpx;
  color: #475569;
  background: #fff;
  border: 1px solid #e2e8f0;
}
.chip.on {
  background: #2563eb;
  color: #fff;
  border-color: #2563eb;
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
  color: #2563eb;
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
  color: #2563eb;
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
