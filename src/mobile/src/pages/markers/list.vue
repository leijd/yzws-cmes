<template>
  <BrandNavBar title="唛架管理" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">唛架管理</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建唛架</button>
    </view>
    <view class="chips">
      <view v-for="s in statusOptions" :key="s.value" class="chip" :class="{ on: status === s.value }" @click="status = s.value; reload()">{{ s.label }}</view>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无唛架</view>

    <view v-for="m in items" :key="m.id" class="card" @click="goDetail(m)">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ m.marker_no }}</text>
            <text class="tag" :class="reviewCls(m.review_status)">{{ reviewText(m.review_status) }}</text>
          </view>
          <text class="muted">{{ m.product_name }} · {{ m.color }} · {{ m.material_name }}</text>
        </view>
        <text class="arrow">›</text>
      </view>
      <view class="row between" style="margin-top: 14rpx">
        <text class="muted">每唛 {{ m.pieces_per_marker }} 件 · 层数 {{ m.fabric_layers }} · 裁片 {{ m.cut_qty }}</text>
        <text class="muted">长 {{ m.marker_length_m }}m · 利用率 {{ utilization(m) }}</text>
      </view>
    </view>
    <view v-if="hasMore" class="loadmore" @click="loadMore">{{ loading ? "加载中…" : "加载更多" }}</view>

    <!-- 新建唛架 -->
    <view v-if="createSheet" class="sheet-mask" @click="createSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建唛架</view>
        <view class="field">
          <text class="field-label required">唛架编号</text>
          <input class="input" v-model.trim="form.marker_no" placeholder="如 MK-0001" />
        </view>
        <view class="field">
          <text class="field-label required">排版日期</text>
          <picker mode="date" :value="form.marker_date" @change="form.marker_date = $event.detail.value">
            <view class="picker-box">{{ form.marker_date || "选择日期" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">工单</text>
          <picker :range="workOrderLabels" :value="woIndex" @change="woIndex = $event.detail.value">
            <view class="picker-box">{{ workOrderLabels[woIndex] || "选择工单" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">颜色</text>
          <input class="input" v-model.trim="form.color" placeholder="如 宝蓝" />
        </view>
        <view class="field">
          <text class="field-label required">面料</text>
          <picker :range="materialNames" :value="materialIndex" @change="materialIndex = $event.detail.value">
            <view class="picker-box">{{ materialNames[materialIndex] || "选择面料" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">排版人</text>
          <picker :range="employeeNames" :value="employeeIndex" @change="employeeIndex = $event.detail.value">
            <view class="picker-box">{{ employeeNames[employeeIndex] || "选择排版人" }}</view>
          </picker>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label required">拉布层数</text>
            <input class="input" v-model="form.fabric_layers" type="number" />
          </view>
          <view class="field grow">
            <text class="field-label required">唛架长度（米）</text>
            <input class="input" v-model="form.marker_length_m" placeholder="如 6.8" />
          </view>
        </view>
        <view class="field">
          <text class="field-label">门幅（cm）</text>
          <input class="input" v-model="form.fabric_width_cm" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label">面料利用率（0-1）</text>
          <input class="input" v-model="form.utilization_rate" placeholder="可选，如 0.85" />
        </view>

        <view class="divider"></view>
        <view class="row between" style="margin-bottom: 12rpx">
          <text class="section-title" style="margin: 0">尺码配比</text>
          <button class="btn btn-outline btn-sm" @click="addRatio">＋ 添加尺码</button>
        </view>
        <view v-for="(ratio, i) in form.ratios" :key="i" class="ratio-row">
          <input class="input grow" v-model.trim="ratio.size" placeholder="尺码（如 M）" />
          <input class="input" style="width: 200rpx" v-model="ratio.ratio_qty" type="number" placeholder="每唛件数" />
          <text v-if="form.ratios.length > 1" class="remove" @click="form.ratios.splice(i, 1)">删</text>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveCreate">{{ saving ? "保存中…" : "保存唛架" }}</button>
        <text class="muted" style="display: block; margin-top: 16rpx">提示：唛架图需在网页版上传后才能提交审核。</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Marker {
  id: string; marker_no: string; marker_date: string; work_order_id: string;
  product_name: string; color: string; material_id: string; material_name: string;
  fabric_width_cm?: string; pieces_per_marker: number; fabric_layers: number;
  cut_qty: number; marker_length_m: string; utilization_rate?: string;
  review_status: string; version: number;
}

interface RatioForm { size: string; ratio_qty: string }

const items = ref<Marker[]>([]);
const loading = ref(false);
const hasMore = ref(false);
const nextCursor = ref("");
const status = ref("");
const statusOptions = [
  { label: "全部", value: "" },
  { label: "待审核", value: "pending" },
  { label: "已通过", value: "approved" },
  { label: "已驳回", value: "rejected" },
];

const workOrders = ref<{ id: string; work_order_code: string }[]>([]);
const workOrderLabels = computed(() => workOrders.value.map((w) => w.work_order_code));
const woIndex = ref(0);
const materials = ref<{ id: string; name: string }[]>([]);
const materialNames = computed(() => materials.value.map((m) => m.name));
const materialIndex = ref(0);
const employees = ref<{ id: string; name: string }[]>([]);
const employeeNames = computed(() => employees.value.map((e) => e.name));
const employeeIndex = ref(0);

const createSheet = ref(false);
const saving = ref(false);
const formError = ref("");
const form = ref({
  marker_no: "", marker_date: "", color: "",
  fabric_layers: "", marker_length_m: "", fabric_width_cm: "", utilization_rate: "",
  ratios: [{ size: "M", ratio_qty: "" }] as RatioForm[],
});

function reviewText(s: string) {
  const map: Record<string, string> = { pending: "待审核", approved: "已通过", rejected: "已驳回" };
  return map[s] || s || "--";
}
function reviewCls(s: string) {
  return s === "approved" ? "tag-green" : s === "rejected" ? "tag-red" : "tag-orange";
}
function utilization(m: Marker): string {
  if (!m.utilization_rate) return "--";
  const n = Number(m.utilization_rate);
  return Number.isFinite(n) ? `${Math.round(n * 100)}%` : m.utilization_rate;
}

async function load(reset = true) {
  loading.value = true;
  try {
    const q: string[] = [];
    if (status.value) q.push(`review_status=${status.value}`);
    if (!reset && nextCursor.value) q.push(`cursor=${nextCursor.value}`);
    const res = await request<{ items: Marker[]; next_cursor?: string; has_more?: boolean }>(
      `/markers${q.length ? "?" + q.join("&") : ""}`,
      { silent: true }
    );
    if (reset) items.value = res.items || [];
    else items.value = [...items.value, ...(res.items || [])];
    nextCursor.value = res.next_cursor || "";
    hasMore.value = !!res.has_more;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}
function reload() {
  load(true);
}
function loadMore() {
  load(false);
}

async function loadOptions() {
  try {
    const [w, m, e] = await Promise.all([
      request<{ items: { id: string; work_order_code: string }[] }>("/work-orders", { silent: true }),
      request<{ items: { id: string; name: string }[] }>("/materials", { silent: true }),
      request<{ items: { id: string; name: string }[] }>("/employees?status=active", { silent: true }),
    ]);
    workOrders.value = w.items || [];
    materials.value = m.items || [];
    employees.value = e.items || [];
  } catch {
    /* 选项加载失败 */
  }
}

function addRatio() {
  form.value.ratios.push({ size: "", ratio_qty: "" });
}

function openCreate() {
  form.value = {
    marker_no: "", marker_date: new Date().toISOString().slice(0, 10), color: "",
    fabric_layers: "", marker_length_m: "", fabric_width_cm: "", utilization_rate: "",
    ratios: [{ size: "M", ratio_qty: "" }],
  };
  woIndex.value = 0;
  materialIndex.value = 0;
  employeeIndex.value = 0;
  formError.value = "";
  createSheet.value = true;
}

async function saveCreate() {
  formError.value = "";
  const f = form.value;
  const wo = workOrders.value[woIndex.value];
  const material = materials.value[materialIndex.value];
  const employee = employees.value[employeeIndex.value];
  if (!f.marker_no || !f.marker_date) { formError.value = "唛架编号与排版日期必填"; return; }
  if (!wo || !material || !employee) { formError.value = "请选择工单、面料与排版人"; return; }
  if (!f.color) { formError.value = "请填写颜色"; return; }
  if (!Number(f.fabric_layers) || Number(f.fabric_layers) <= 0) { formError.value = "拉布层数必须大于 0"; return; }
  if (!Number(f.marker_length_m) || Number(f.marker_length_m) <= 0) { formError.value = "唛架长度必须大于 0"; return; }
  const ratios = f.ratios
    .filter((r) => r.size && Number(r.ratio_qty) > 0)
    .map((r, i) => ({ size: r.size, ratio_qty: Number(r.ratio_qty), sort_no: i + 1 }));
  if (!ratios.length) { formError.value = "尺码配比不能为空"; return; }
  saving.value = true;
  try {
    await request("/markers", {
      method: "POST",
      data: {
        marker_no: f.marker_no,
        marker_date: f.marker_date,
        work_order_id: wo.id,
        color: f.color,
        material_id: material.id,
        layout_employee_id: employee.id,
        fabric_layers: Number(f.fabric_layers),
        marker_length_m: f.marker_length_m,
        fabric_width_cm: f.fabric_width_cm || undefined,
        utilization_rate: f.utilization_rate || undefined,
        ratios,
      },
      idempotency: true,
    });
    uni.showToast({ title: "唛架已创建", icon: "none" });
    createSheet.value = false;
    reload();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function goDetail(m: Marker) {
  uni.navigateTo({ url: `/pages/markers/detail?id=${m.id}` });
}

onLoad(() => {
  reload();
  loadOptions();
});
</script>

<style scoped>
.arrow { font-size: 40rpx; color: #cbd5e1; }
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 90vh; overflow-y: auto; box-sizing: border-box; }
.ratio-row { display: flex; align-items: center; gap: 16rpx; margin-bottom: 12rpx; }
.remove { color: #b91c1c; font-size: 24rpx; flex-shrink: 0; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
