<template>
  <BrandNavBar title="我的计件" />
  <view class="page">
    <view class="summary card">
      <view class="summary-item">
        <text class="num">{{ items.length }}</text>
        <text class="cap">计件笔数</text>
      </view>
      <view class="summary-item">
        <text class="num amount">¥ {{ totalAmount }}</text>
        <text class="cap">累计金额（已确认-冲销）</text>
      </view>
    </view>

    <view v-if="!loading && !items.length" class="empty">暂无计件记录</view>

    <view v-for="item in items" :key="item.id" class="card">
      <view class="row between">
        <text class="amount" :class="{ reversal: item.entry_kind === 'reversal' }">
          ¥ {{ item.amount }}
        </text>
        <text class="kind">{{ item.entry_kind === "reversal" ? "冲销" : "确认" }}</text>
      </view>
      <view class="row">
        <text class="label">数量</text>
        <text class="value">{{ item.qty }} × ¥{{ item.unit_price }}</text>
      </view>
      <view class="row">
        <text class="label">工资周期</text>
        <text class="value">{{ item.payroll_period || "—" }}</text>
      </view>
      <view class="row">
        <text class="label">时间</text>
        <text class="value">{{ formatDateTime(item.created_at) }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { formatDateTime } from "../../utils/format";

interface PieceworkItem {
  id: string;
  qty: string;
  unit_price: string;
  amount: string;
  entry_kind: string;
  payroll_period: string;
  created_at: number;
}

const items = ref<PieceworkItem[]>([]);
const loading = ref(false);

const totalAmount = computed(() =>
  items.value
    .reduce((sum, i) => sum + (Number(i.amount) || 0), 0)
    .toFixed(2)
);

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: PieceworkItem[] }>("/piecework/mine", {
      silent: true,
    });
    items.value = res.items || [];
  } catch {
    items.value = [];
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
  font-size: 36rpx;
  font-weight: 700;
  color: #2b6cb0;
}
.amount {
  color: #38a169;
}
.amount.reversal {
  color: #e53e3e;
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
  border-bottom: 1px solid #edf2f7;
  padding-bottom: 12rpx;
  margin-bottom: 10rpx;
  font-size: 30rpx;
  font-weight: 600;
}
.kind {
  font-size: 24rpx;
  color: #718096;
}
.label {
  color: #718096;
  width: 150rpx;
}
.value {
  color: #2d3748;
}
</style>
