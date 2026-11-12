<template>
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">返工复检</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建返工单</button>
    </view>
    <view class="chips">
      <view v-for="s in statusOptions" :key="s.value" class="chip" :class="{ on: status === s.value }" @click="status = s.value">{{ s.label }}</view>
    </view>

    <view v-if="loading && !filtered.length" class="empty">加载中…</view>
    <view v-else-if="!filtered.length" class="empty">暂无返工单</view>

    <view v-for="r in filtered" :key="r.id" class="card" @click="goDetail(r)">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ r.rework_no }}</text>
            <text class="tag" :class="statusCls(r.status)">{{ statusText(r.status) }}</text>
          </view>
          <text class="muted">{{ r.work_order_code }} · {{ r.bundle_no }} 扎 · {{ r.color }} {{ r.size }}</text>
        </view>
      </view>
      <view class="row between" style="margin-top: 14rpx">
        <text class="muted">返工 {{ r.qty }} · 已报 {{ r.reported_qty }} · {{ r.employee_name || "未分派" }}</text>
        <text class="arrow">›</text>
      </view>
      <text v-if="r.reason" class="muted" style="margin-top: 8rpx">原因：{{ r.reason }}</text>
    </view>

    <!-- 新建返工单 -->
    <view v-if="createSheet" class="sheet-mask" @click="createSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建返工单</view>
        <view class="field">
          <text class="field-label">返工单号（留空自动生成）</text>
          <input class="input" v-model.trim="form.rework_no" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label required">总检单（已确认）</text>
          <picker :range="inspectionLabels" :value="inspectionIndex" @change="inspectionIndex = $event.detail.value">
            <view class="picker-box">{{ inspectionLabels[inspectionIndex] || "选择总检单" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">返工数量</text>
          <input class="input" v-model="form.qty" type="number" />
        </view>
        <view class="field">
          <text class="field-label required">返工原因</text>
          <input class="input" v-model.trim="form.reason" placeholder="如 线迹不良" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveCreate">{{ saving ? "创建中…" : "创建返工单" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface ReworkItem {
  id: string; rework_no: string; inspection_id: string; work_order_code: string;
  bundle_no: string; color: string; size: string; qty: number; reported_qty: number;
  employee_name?: string; status: string; reason?: string; version: number;
}

interface FinalInspectionOption {
  id: string; inspection_no: string; rework_qty: number; status: string;
}

const items = ref<ReworkItem[]>([]);
const loading = ref(false);
const status = ref("");
const statusOptions = [
  { label: "全部", value: "" },
  { label: "待处理", value: "pending" },
  { label: "已分派", value: "assigned" },
  { label: "已报工", value: "reported" },
  { label: "已完成", value: "completed" },
];

const filtered = computed(() => (status.value ? items.value.filter((r) => r.status === status.value) : items.value));

const inspections = ref<FinalInspectionOption[]>([]);
const inspectionLabels = computed(() => inspections.value.map((i) => `${i.inspection_no} · 返工数 ${i.rework_qty}`));
const inspectionIndex = ref(0);

const createSheet = ref(false);
const saving = ref(false);
const formError = ref("");
const form = ref({ rework_no: "", qty: "", reason: "" });

function statusText(s: string) {
  const map: Record<string, string> = { pending: "待处理", assigned: "已分派", reported: "已报工", completed: "已完成", cancelled: "已取消" };
  return map[s] || s || "--";
}
function statusCls(s: string) {
  return s === "completed" ? "tag-green" : s === "reported" ? "tag-blue" : s === "assigned" ? "tag-purple" : s === "cancelled" ? "tag-gray" : "tag-orange";
}

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: ReworkItem[] }>("/rework-orders", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadInspections() {
  try {
    const res = await request<{ items: FinalInspectionOption[] }>("/final-inspections", { silent: true });
    inspections.value = (res.items || []).filter((i) => i.status === "confirmed" && i.rework_qty > 0);
  } catch {
    inspections.value = [];
  }
}

function openCreate() {
  form.value = { rework_no: "", qty: "", reason: "" };
  inspectionIndex.value = 0;
  formError.value = "";
  createSheet.value = true;
}

async function saveCreate() {
  formError.value = "";
  const inspection = inspections.value[inspectionIndex.value];
  if (!inspection) { formError.value = "请选择总检单"; return; }
  const qty = Number(form.value.qty);
  if (!qty || qty <= 0) { formError.value = "请填写返工数量"; return; }
  if (qty > inspection.rework_qty) { formError.value = `返工数量不能超过总检返工数 ${inspection.rework_qty}`; return; }
  if (!form.value.reason) { formError.value = "请填写返工原因"; return; }
  saving.value = true;
  try {
    await request("/rework-orders", {
      method: "POST",
      data: {
        rework_no: form.value.rework_no || undefined,
        inspection_id: inspection.id,
        qty,
        reason: form.value.reason,
      },
      idempotency: true,
    });
    uni.showToast({ title: "返工单已创建", icon: "none" });
    createSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function goDetail(r: ReworkItem) {
  uni.navigateTo({ url: `/pages/rework/detail?id=${r.id}` });
}

onLoad(() => {
  load();
  loadInspections();
});
</script>

<style scoped>
.arrow { font-size: 40rpx; color: #cbd5e1; }
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
