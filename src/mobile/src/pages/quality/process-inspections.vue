<template>
  <BrandNavBar title="过程巡检" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">过程巡检</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建巡检</button>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无巡检记录</view>

    <view v-for="pi in items" :key="pi.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ pi.inspection_no }}</text>
            <text class="tag" :class="pi.status === 'confirmed' ? 'tag-green' : 'tag-orange'">{{ pi.status === "confirmed" ? "已确认" : "待确认" }}</text>
          </view>
          <text class="muted">{{ pi.work_order_code }} · {{ pi.process_name }} · {{ pi.bundle_no }} 扎 · {{ pi.color }} {{ pi.size }}</text>
        </view>
      </view>
      <view class="row between" style="margin-top: 14rpx">
        <text class="muted">检 {{ pi.inspected_qty }} · 合格 {{ pi.passed_qty }} · 返工 {{ pi.rework_qty }} · 报废 {{ pi.scrapped_qty }}</text>
        <text class="muted">{{ pi.inspector_name || "未记录" }}</text>
      </view>
      <view class="actions">
        <button v-if="pi.status === 'pending'" class="btn btn-solid btn-sm" @click="confirm(pi)">确认</button>
        <button v-if="pi.status === 'confirmed' && pi.rework_qty > 0" class="btn btn-outline btn-sm" @click="openRework(pi)">创建返工单</button>
      </view>
    </view>

    <!-- 新建巡检 -->
    <view v-if="sheet" class="sheet-mask" @click="sheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建过程巡检</view>
        <view class="field">
          <text class="field-label">巡检单号（留空自动生成）</text>
          <input class="input" v-model.trim="form.inspection_no" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label required">工单</text>
          <picker :range="workOrderLabels" :value="woIndex" @change="onWoChange">
            <view class="picker-box">{{ workOrderLabels[woIndex] || "选择工单" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">工序</text>
          <picker :range="processLabels" :value="processIndex" @change="processIndex = $event.detail.value">
            <view class="picker-box">{{ processLabels[processIndex] || "选择工序" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">扎</text>
          <picker :range="bundleLabels" :value="bundleIndex" @change="bundleIndex = $event.detail.value">
            <view class="picker-box">{{ bundleLabels[bundleIndex] || "选择扎" }}</view>
          </picker>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label required">巡检数</text>
            <input class="input" v-model="form.inspected_qty" type="number" />
          </view>
          <view class="field grow">
            <text class="field-label required">合格数</text>
            <input class="input" v-model="form.passed_qty" type="number" />
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">返工数</text>
            <input class="input" v-model="form.rework_qty" type="number" />
          </view>
          <view class="field grow">
            <text class="field-label">报废数</text>
            <input class="input" v-model="form.scrapped_qty" type="number" />
          </view>
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="form.remark" placeholder="可选" />
        </view>
        <view v-if="error" class="error-text">{{ error }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>

    <!-- 创建返工单 -->
    <view v-if="reworkSheet" class="sheet-mask" @click="reworkSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">创建返工单</view>
        <view class="field">
          <text class="field-label">返工单号（留空自动生成）</text>
          <input class="input" v-model.trim="reworkForm.rework_no" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label required">返工数量（不超过 {{ reworkTarget?.rework_qty || 0 }}）</text>
          <input class="input" v-model="reworkForm.qty" type="number" />
        </view>
        <view class="field">
          <text class="field-label required">返工原因</text>
          <input class="input" v-model.trim="reworkForm.reason" placeholder="如 线迹不良" />
        </view>
        <view v-if="reworkError" class="error-text">{{ reworkError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveRework">{{ saving ? "创建中…" : "创建返工单" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface ProcessInspection {
  id: string; inspection_no: string; work_order_id: string; work_order_code: string;
  process_snapshot_id: string; process_code: string; process_name: string;
  bundle_id: string; bundle_no: string; color: string; size: string;
  inspected_qty: number; passed_qty: number; rework_qty: number; scrapped_qty: number;
  inspector_name?: string; status: string; remark?: string; version: number;
}
interface WorkOrderOption { id: string; work_order_code: string }
interface ProcessOption { process_snapshot_id: string; process_name: string; process_code: string }
interface BundleOption { id: string; work_order_id: string; bundle_no: string; color: string; size: string; current_qty: number }

const items = ref<ProcessInspection[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const sheet = ref(false);
const workOrders = ref<WorkOrderOption[]>([]);
const processes = ref<ProcessOption[]>([]);
const bundles = ref<BundleOption[]>([]);
const woIndex = ref(0);
const processIndex = ref(0);
const bundleIndex = ref(0);
const workOrderLabels = computed(() => workOrders.value.map((w) => w.work_order_code));
const processLabels = computed(() => processes.value.map((p) => `${p.process_code} ${p.process_name}`));
const bundleLabels = computed(() =>
  bundles.value.filter((b) => b.work_order_id === workOrders.value[woIndex.value]?.id).map((b) => `${b.bundle_no} 扎 · ${b.color} ${b.size}`)
);

const form = ref({ inspection_no: "", inspected_qty: "", passed_qty: "", rework_qty: "0", scrapped_qty: "0", remark: "" });

const reworkSheet = ref(false);
const reworkTarget = ref<ProcessInspection | null>(null);
const reworkError = ref("");
const reworkForm = ref({ rework_no: "", qty: "", reason: "" });

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: ProcessInspection[] }>("/process-inspections", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function onWoChange(e: { detail: { value: number } }) {
  woIndex.value = e.detail.value;
  processIndex.value = 0;
  bundleIndex.value = 0;
  loadProcesses();
}

async function loadProcesses() {
  const wo = workOrders.value[woIndex.value];
  if (!wo) { processes.value = []; return; }
  try {
    /* 工序选项取自任务接口（返回 process_snapshot_id 供创建巡检） */
    const res = await request<{ items: { process_snapshot_id: string; process_code: string; process_name: string }[] }>(
      `/production-tasks?work_order_id=${wo.id}`, { silent: true }
    );
    processes.value = (res.items || []).map((p) => ({ process_snapshot_id: p.process_snapshot_id, process_code: p.process_code, process_name: p.process_name }));
  } catch {
    processes.value = [];
  }
}

function openCreate() {
  form.value = { inspection_no: "", inspected_qty: "", passed_qty: "", rework_qty: "0", scrapped_qty: "0", remark: "" };
  woIndex.value = 0;
  processIndex.value = 0;
  bundleIndex.value = 0;
  error.value = "";
  loadProcesses();
  sheet.value = true;
}

async function save() {
  error.value = "";
  const wo = workOrders.value[woIndex.value];
  const proc = processes.value[processIndex.value];
  const bundle = bundles.value.filter((b) => b.work_order_id === wo?.id)[bundleIndex.value];
  if (!wo || !proc || !bundle) { error.value = "请选择工单、工序与扎"; return; }
  const inspected = Number(form.value.inspected_qty);
  const passed = Number(form.value.passed_qty);
  const rework = Number(form.value.rework_qty || 0);
  const scrapped = Number(form.value.scrapped_qty || 0);
  if (!inspected || inspected !== passed + rework + scrapped) {
    error.value = "巡检数必须等于合格 + 返工 + 报废";
    return;
  }
  saving.value = true;
  try {
    await request("/process-inspections", {
      method: "POST",
      data: {
        inspection_no: form.value.inspection_no || undefined,
        work_order_id: wo.id,
        process_snapshot_id: proc.process_snapshot_id,
        bundle_id: bundle.id,
        inspected_qty: inspected,
        passed_qty: passed,
        rework_qty: rework,
        scrapped_qty: scrapped,
        remark: form.value.remark || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "巡检已创建", icon: "none" });
    sheet.value = false;
    load();
  } catch (e) {
    error.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

async function confirm(pi: ProcessInspection) {
  try {
    await request(`/process-inspections/${pi.id}/confirm`, { method: "POST", data: { version: pi.version } });
    uni.showToast({ title: "已确认", icon: "none" });
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "确认失败", icon: "none" });
  }
}

function openRework(pi: ProcessInspection) {
  reworkTarget.value = pi;
  reworkForm.value = { rework_no: "", qty: String(pi.rework_qty), reason: "" };
  reworkError.value = "";
  reworkSheet.value = true;
}

async function saveRework() {
  reworkError.value = "";
  const pi = reworkTarget.value;
  if (!pi) return;
  const qty = Number(reworkForm.value.qty);
  if (!qty || qty <= 0) { reworkError.value = "请填写返工数量"; return; }
  if (qty > pi.rework_qty) { reworkError.value = `返工数量不能超过巡检返工数 ${pi.rework_qty}`; return; }
  if (!reworkForm.value.reason) { reworkError.value = "请填写返工原因"; return; }
  saving.value = true;
  try {
    await request(`/process-inspections/${pi.id}/rework`, {
      method: "POST",
      data: {
        rework_no: reworkForm.value.rework_no || undefined,
        qty,
        reason: reworkForm.value.reason,
      },
      idempotency: true,
    });
    uni.showToast({ title: "返工单已创建", icon: "none" });
    reworkSheet.value = false;
  } catch (e) {
    reworkError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

onLoad(() => {
  load();
  Promise.all([
    request<{ items: WorkOrderOption[] }>("/work-orders", { silent: true }).then((r) => (workOrders.value = r.items || [])),
    request<{ items: BundleOption[] }>("/bundles", { silent: true }).then((r) => (bundles.value = r.items || [])),
  ]).catch(() => {});
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
