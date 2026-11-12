<template>
  <view class="page">
    <!-- 月份切换 -->
    <view class="card row between">
      <view class="month-nav" @click="shift(-1)">‹ 上月</view>
      <text class="title">{{ periodLabel }}</text>
      <view class="month-nav" @click="shift(1)" v-if="canNext">下月 ›</view>
      <text v-else class="month-nav" style="color: transparent">下月 ›</text>
    </view>

    <!-- 汇总 -->
    <view class="card">
      <view class="row between">
        <view class="col">
          <text class="muted">本期计件合计</text>
          <text class="num" style="margin-top: 8rpx">{{ money(pieceworkTotal) }}</text>
        </view>
        <view v-if="sheet" class="col" style="align-items: flex-end">
          <text class="tag" :class="sheetStatus.cls">{{ sheetStatus.text }}</text>
          <text class="muted" style="margin-top: 8rpx">应发 {{ money(sheet.net_amount) }} · 已发 {{ money(sheet.paid_amount) }}</text>
        </view>
      </view>
      <view v-if="sheet" class="kv" style="margin-top: 16rpx">
        <text class="k">未发余额</text>
        <text class="v strong">{{ money(sheet.outstanding_amount) }}</text>
      </view>
    </view>

    <!-- 计件明细 -->
    <view class="section-title">计件明细（{{ piecework.length }}）</view>
    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!piecework.length" class="empty">本期暂无计件记录</view>
    <view v-for="(p, i) in piecework" :key="i" class="card row between">
      <view class="grow">
        <text class="title" style="font-size: 28rpx">{{ p.process_name || "未分工序" }}</text>
        <text class="muted">{{ fmtDateTime(p.created_at) }} · {{ kindText(p.entry_kind) }}</text>
      </view>
      <view class="col" style="align-items: flex-end">
        <text class="title" style="font-size: 30rpx; color: #16a34a">{{ money(p.amount) }}</text>
        <text class="muted">{{ p.qty }} × {{ money(p.unit_price, "") }}</text>
      </view>
    </view>

    <!-- 发放记录 -->
    <template v-if="sheet && sheet.payments && sheet.payments.length">
      <view class="section-title">发放记录</view>
      <view v-for="(pay, i) in sheet.payments" :key="i" class="card row between">
        <view class="grow">
          <text class="title" style="font-size: 28rpx">{{ pay.payment_method || "现金" }}</text>
          <text class="muted">{{ fmtDate(pay.paid_at) }}</text>
        </view>
        <text class="title" style="font-size: 30rpx; color: #16a34a">{{ money(pay.amount) }}</text>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { fmtDate, fmtDateTime, statusTag } from "../../utils/format";

interface Piecework {
  created_at: number;
  process_name?: string;
  qty: number;
  unit_price: string;
  amount: string;
  entry_kind: string;
}

interface Sheet {
  payroll_period: string;
  gross_amount: string;
  net_amount: string;
  paid_amount: string;
  outstanding_amount: string;
  status: string;
  payments: { amount: string; paid_at: number; payment_method?: string }[];
}

interface MyWages {
  period: string;
  employee_name: string;
  piecework_total: string;
  piecework: Piecework[];
  sheet?: Sheet;
}

const period = ref("");
const pieceworkTotal = ref("0");
const piecework = ref<Piecework[]>([]);
const sheet = ref<Sheet | undefined>(undefined);
const loading = ref(false);

const periodLabel = computed(() => {
  if (!period.value) return "";
  const [y, m] = period.value.split("-");
  return `${y} 年 ${Number(m)} 月`;
});

const sheetStatus = computed(() => statusTag(sheet.value?.status || ""));

const canNext = computed(() => {
  const now = new Date();
  const cur = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
  return period.value < cur;
});

function money(v: string | undefined, unit = "元"): string {
  const n = Number(v);
  if (!Number.isFinite(n)) return `-- ${unit}`;
  return `${n.toFixed(2)}${unit ? " " + unit : ""}`;
}

function kindText(kind: string): string {
  const map: Record<string, string> = {
    confirmed: "已确认",
    reversal: "冲红",
    quality_adjustment: "质检调整",
    manual_adjustment: "手工调整",
    pending: "待审核",
  };
  return map[kind] || kind;
}

function shift(delta: number) {
  if (!period.value) return;
  const [y, m] = period.value.split("-").map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  const next = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
  const now = new Date();
  const cur = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
  if (delta > 0 && next > cur) return;
  period.value = next;
  load();
}

async function load() {
  loading.value = true;
  try {
    const res = await request<MyWages>(`/my/wages?period=${period.value}`, { silent: true });
    pieceworkTotal.value = res.piecework_total || "0";
    piecework.value = res.piecework || [];
    sheet.value = res.sheet;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

onLoad(() => {
  const now = new Date();
  period.value = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
  load();
});
</script>

<style scoped>
.month-nav {
  font-size: 26rpx;
  color: #1d4ed8;
}
</style>
