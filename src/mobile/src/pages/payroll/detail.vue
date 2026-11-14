<template>
  <BrandNavBar title="工资单详情" />
  <view class="page">
    <view v-if="sheet" class="card head">
      <view class="row between">
        <text class="name">{{ employeeName }}</text>
        <text class="status" :class="status">{{ sheetStatusText[status] || status }}</text>
      </view>
      <view class="row">
        <text class="label">工资周期</text>
        <text class="value">{{ period }}</text>
      </view>
      <view class="row between total">
        <text>实发工资</text>
        <text class="net">¥ {{ money(sheet.net_amount) }}</text>
      </view>
      <view class="row between">
        <text class="sub">应发 ¥{{ money(sheet.gross_amount) }}</text>
        <text class="sub" v-if="Number(money(sheet.outstanding_amount)) > 0">
          未发 ¥{{ money(sheet.outstanding_amount) }}
        </text>
      </view>
    </view>

    <view class="card">
      <text class="card-title">工资构成</text>
      <view v-if="!items.length" class="none">无明细项目</view>
      <view v-for="(item, idx) in items" :key="idx" class="row between item">
        <text class="item-name">{{ item.item_name || item.item_code }}</text>
        <text class="item-amount" :class="{ minus: Number(item.amount) < 0 }">
          {{ Number(item.amount) < 0 ? "-" : "" }}¥{{
            money(Math.abs(Number(item.amount)))
          }}
          <text v-if="item.qty" class="qty">（{{ item.qty }}）</text>
        </text>
      </view>
    </view>

    <view class="card">
      <text class="card-title">付款记录</text>
      <view v-if="!payments.length" class="none">暂无付款</view>
      <view v-for="p in payments" :key="p.id" class="row between item">
        <view class="pay-left">
          <text class="item-name">{{ p.payment_method }}</text>
          <text class="pay-time">{{ formatDateTime(p.paid_at) }}</text>
        </view>
        <text class="item-amount">¥ {{ money(p.amount) }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { formatDateTime } from "../../utils/format";

interface Sheet {
  id: string;
  payroll_period?: string;
  status: string;
  gross_amount: number | string;
  net_amount: number | string;
  outstanding_amount: number | string;
  version: number;
}

interface SheetItem {
  item_type: string;
  item_code: string;
  item_name: string;
  qty?: string;
  amount: string;
}

interface Payment {
  id: string;
  amount: string;
  paid_at: number;
  payment_method: string;
}

const sheet = ref<Sheet | null>(null);
const items = ref<SheetItem[]>([]);
const payments = ref<Payment[]>([]);
const employeeName = ref("");
const period = ref("");
const status = ref("");

const sheetStatusText: Record<string, string> = {
  draft: "待确认",
  confirmed: "已确认",
  partially_paid: "部分发放",
  paid: "已发放",
  locked: "已锁定",
};

function money(v: number | string | undefined | null): string {
  const n = Number(v ?? 0);
  return Number.isFinite(n) ? n.toFixed(2) : String(v ?? "0");
}

let sheetId = "";

onLoad((options) => {
  sheetId = options?.id || "";
  employeeName.value = options?.name ? decodeURIComponent(options.name) : "";
  period.value = options?.period || "";
  if (sheetId) load();
});

async function load() {
  try {
    const res = await request<{ sheet: Sheet; items: SheetItem[] }>(
      `/payroll-sheets/${sheetId}`,
      { silent: true }
    );
    sheet.value = res.sheet;
    items.value = res.items || [];
    status.value = res.sheet.status;
    period.value = res.sheet.payroll_period || period.value;
  } catch {
    sheet.value = null;
  }
  try {
    const res = await request<{ items: Payment[] }>(
      `/payroll-sheets/${sheetId}/payments`,
      { silent: true }
    );
    payments.value = res.items || [];
  } catch {
    payments.value = [];
  }
}
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
.head .name,
.head .label,
.head .value,
.head .sub {
  color: #fff;
}
.head .label {
  opacity: 0.7;
}
.card-title {
  font-size: 28rpx;
  font-weight: 600;
  color: #2d3748;
  display: block;
  margin-bottom: 12rpx;
}
.row {
  display: flex;
  padding: 6rpx 0;
  font-size: 26rpx;
}
.between {
  justify-content: space-between;
}
.name {
  font-size: 32rpx;
  font-weight: 700;
}
.status {
  font-size: 24rpx;
  border: 1px solid rgba(255, 255, 255, 0.5);
  border-radius: 8rpx;
  padding: 2rpx 14rpx;
  color: #fff;
}
.total {
  border-top: 1px solid rgba(255, 255, 255, 0.3);
  margin-top: 14rpx;
  padding-top: 16rpx;
  font-size: 28rpx;
}
.net {
  font-size: 40rpx;
  font-weight: 700;
}
.sub {
  font-size: 24rpx;
  opacity: 0.8;
}
.label {
  color: #718096;
  width: 150rpx;
}
.value {
  color: #2d3748;
}
.none {
  color: #a0aec0;
  font-size: 26rpx;
  padding: 16rpx 0;
}
.item {
  border-bottom: 1px solid #edf2f7;
  padding: 14rpx 0;
}
.item:last-child {
  border-bottom: none;
}
.item-name {
  font-size: 28rpx;
  color: #2d3748;
}
.item-amount {
  font-size: 28rpx;
  font-weight: 600;
  color: #2d3748;
}
.item-amount.minus {
  color: #e53e3e;
}
.qty {
  font-size: 22rpx;
  color: #a0aec0;
  font-weight: 400;
}
.pay-left {
  display: flex;
  flex-direction: column;
}
.pay-time {
  font-size: 22rpx;
  color: #a0aec0;
  margin-top: 4rpx;
}
</style>
