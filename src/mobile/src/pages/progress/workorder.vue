<template>
  <BrandNavBar :title="woCode || '工单进度'" />
  <view class="page">
    <view class="chips">
      <view v-for="t in tabs" :key="t.key" class="chip" :class="{ on: tab === t.key }" @click="tab = t.key; load()">{{ t.label }}</view>
    </view>

    <view v-if="loading" class="empty">加载中…</view>

    <!-- 工序进度 -->
    <template v-else-if="tab === 'process'">
      <view v-if="!processes.length" class="empty">暂无工序进度</view>
      <view v-for="p in processes" :key="p.process_code" class="card">
        <view class="row between">
          <text class="title">{{ p.sequence_no }}. {{ p.process_name }}</text>
          <text class="pct">{{ ratioPercent(p.progress_ratio) }}</text>
        </view>
        <view class="progress-bar"><view class="progress-fill" :style="{ width: ratioPercent(p.progress_ratio) }" /></view>
        <view class="row meta">
          <text>{{ p.process_code }}</text>
          <text>计划 {{ p.planned_qty }} · 良品 {{ p.approved_good_qty }}</text>
        </view>
        <view v-if="(p.current_employees || []).length" class="row meta">
          <text>当前作业：{{ empNames(p.current_employees) }}</text>
        </view>
      </view>
    </template>

    <!-- 人员进度 -->
    <template v-else-if="tab === 'employee'">
      <view v-if="!employees.length" class="empty">暂无人员进度</view>
      <view v-for="e in employees" :key="e.name" class="card">
        <view class="row between">
          <text class="title">{{ e.name }}</text>
          <text class="pct">{{ e.ratio }}</text>
        </view>
        <view class="progress-bar"><view class="progress-fill" :style="{ width: e.ratio }" /></view>
        <view class="row meta">
          <text>计划 {{ e.planned }}</text>
          <text>良品 {{ e.approved }} · 未审批 {{ e.pending }}</text>
        </view>
      </view>
    </template>

    <!-- 工序明细 -->
    <template v-else>
      <view v-if="!bundles.length" class="empty">暂无工序明细</view>
      <view v-for="(b, i) in bundles" :key="i" class="card">
        <view class="row between">
          <text class="title">{{ b.bundle_no }}</text>
          <text class="tag">{{ b.review_status || '—' }}</text>
        </view>
        <view class="row meta">
          <text>{{ b.process_code }} · {{ b.process_name }}</text>
        </view>
        <view class="row meta">
          <text>{{ b.color }}/{{ b.size }}</text>
          <text>作业人 {{ b.employee_name || '—' }}</text>
        </view>
        <view class="row meta">
          <text>良品 {{ b.good_qty }} · 不良 {{ b.defect_qty }}</text>
          <text v-if="b.reported_at">{{ fmtTime(b.reported_at) }}</text>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Task {
  employee_name: string | null;
  planned_qty: number;
  approved_good_qty: number;
  pending_qty: number;
}

const tabs = [
  { key: "process", label: "工序进度" },
  { key: "employee", label: "人员进度" },
  { key: "detail", label: "工序明细" },
];
const tab = ref("process");
const loading = ref(false);
const woId = ref("");
const woCode = ref("");
const processes = ref<any[]>([]);
const bundles = ref<any[]>([]);
const employees = ref<Array<{ name: string; planned: number; approved: number; pending: number; ratio: string }>>([]);

function ratioPercent(ratio: string | number | null | undefined): string {
  const n = Number(ratio || 0);
  return `${Math.min(100, Math.max(0, n * 100)).toFixed(1)}%`;
}
function empNames(list: Array<{ name: string }> | null | undefined): string {
  return (list || []).map(e => e.name).join("、");
}
function fmtTime(us: number): string {
  const d = new Date(Number(us) / 1000);
  if (isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

async function loadProcesses() {
  const res = await request<{ items: any[] }>(`/progress/work-orders/${woId.value}/processes`, { silent: true });
  processes.value = res.items || [];
}
async function loadEmployees() {
  const res = await request<{ items: Task[] }>(`/production-tasks?work_order_id=${woId.value}`, { silent: true });
  const map = new Map<string, { name: string; planned: number; approved: number; pending: number }>();
  for (const t of res.items || []) {
    const name = t.employee_name || "未分配";
    const entry = map.get(name) || { name, planned: 0, approved: 0, pending: 0 };
    entry.planned += Number(t.planned_qty || 0);
    entry.approved += Number(t.approved_good_qty || 0);
    entry.pending += Number(t.pending_qty || 0);
    map.set(name, entry);
  }
  employees.value = Array.from(map.values()).map(e => ({
    ...e,
    ratio: e.planned > 0 ? `${Math.min(100, Math.max(0, (e.approved / e.planned) * 100)).toFixed(1)}%` : "0.0%",
  }));
}
async function loadBundles() {
  const res = await request<{ items: any[] }>(`/progress/bundle-processes?work_order_id=${woId.value}`, { silent: true });
  bundles.value = res.items || [];
}

async function load() {
  loading.value = true;
  try {
    if (tab.value === "process") await loadProcesses();
    else if (tab.value === "employee") await loadEmployees();
    else await loadBundles();
  } catch {
    processes.value = []; bundles.value = []; employees.value = [];
  } finally {
    loading.value = false;
  }
}

onLoad((options) => {
  woId.value = options?.id || "";
  woCode.value = options?.code ? decodeURIComponent(options.code) : "";
  if (woId.value) load();
});
</script>

<style scoped>
.page { min-height: 100vh; background: #f5f7fa; padding: 20rpx 28rpx; box-sizing: border-box; }
.chips { display: flex; gap: 14rpx; margin-bottom: 20rpx; }
.chip { padding: 12rpx 26rpx; border-radius: 999rpx; background: #e6effc; color: #2b6cb0; font-size: 26rpx; }
.chip.on { background: #2b6cb0; color: #fff; }
.card { background: #fff; border-radius: 14rpx; padding: 22rpx 26rpx; margin-bottom: 16rpx; }
.row { display: flex; padding: 4rpx 0; }
.between { justify-content: space-between; align-items: center; }
.title { font-size: 30rpx; font-weight: 600; color: #2d3748; }
.pct { font-size: 32rpx; font-weight: 700; color: #2b6cb0; }
.progress-bar { height: 14rpx; background: #e6effc; border-radius: 8rpx; overflow: hidden; margin: 12rpx 0; }
.progress-fill { height: 100%; background: linear-gradient(90deg, #2563eb, #06b6d4); border-radius: 8rpx; }
.meta { font-size: 24rpx; color: #718096; }
.tag { font-size: 22rpx; color: #2c5282; background: #ebf4ff; border-radius: 8rpx; padding: 2rpx 12rpx; }
.empty { text-align: center; color: #a0aec0; padding: 60rpx 0; font-size: 28rpx; }
</style>
