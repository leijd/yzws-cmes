<template>
  <BrandNavBar title="物料入库" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">物料入库</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建入库单</button>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无入库单</view>

    <view v-for="r in items" :key="r.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ r.receipt_no }}</text>
            <text class="tag" :class="statusCls(r.status)">{{ statusText(r.status) }}</text>
          </view>
          <text class="muted">{{ r.project_code }} · {{ r.product_name }}</text>
        </view>
      </view>
      <view class="row between" style="margin-top: 14rpx">
        <text class="muted">来料 {{ r.received_total }} · 合格 {{ r.pass_total }} · 金额 {{ r.amount_total }}</text>
        <text class="muted">{{ r.supplier_name || "未指定供应商" }}</text>
      </view>
      <view class="actions">
        <button class="btn btn-outline btn-sm" @click="openDetail(r)">明细</button>
        <button v-if="r.status === 'draft'" class="btn btn-solid btn-sm" @click="submit(r)">提交入库</button>
        <button v-if="r.status === 'draft'" class="btn btn-danger btn-sm" @click="remove(r)">删除</button>
        <button v-if="r.status === 'submitted'" class="btn btn-danger btn-sm" @click="reverse(r)">冲销</button>
      </view>
    </view>

    <!-- 明细弹层 -->
    <view v-if="detailSheet" class="sheet-mask" @click="detailSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">入库明细 · {{ detail?.header?.receipt_no }}</view>
        <view v-if="detailLoading" class="empty">加载中…</view>
        <template v-else-if="detail">
          <view v-for="l in detail.lines" :key="l.id" class="list-row" style="align-items: flex-start">
            <view class="grow">
              <text class="title" style="font-size: 28rpx">{{ l.material_name }}</text>
              <text class="muted">{{ l.material_code }} · {{ l.specification || "无规格" }} · {{ l.unit }}</text>
              <text class="muted">来料 {{ l.received_qty }} · 合格 {{ l.pass_qty }} · 实收 {{ l.accepted_qty }} · 单价 {{ l.unit_price }}</text>
              <text v-if="l.iqc_result" class="muted">IQC：{{ iqcText(l.iqc_result) }}{{ l.inspector_name ? `（${l.inspector_name}）` : "" }}</text>
            </view>
            <button v-if="!l.iqc_result" class="btn btn-outline btn-sm" @click="openIqc(l)">IQC</button>
          </view>
        </template>
      </view>
    </view>

    <!-- IQC 弹层 -->
    <view v-if="iqcSheet" class="sheet-mask" @click="iqcSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">来料检验 IQC</view>
        <view class="chips">
          <view v-for="r in iqcResults" :key="r.value" class="chip" :class="{ on: iqcResult === r.value }" @click="iqcResult = r.value">{{ r.label }}</view>
        </view>
        <view class="field">
          <text class="field-label">缺陷描述（不通过/让步时填写）</text>
          <input class="input" v-model.trim="iqcDefect" placeholder="可选" />
        </view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveIqc">{{ saving ? "提交中…" : "提交检验结果" }}</button>
      </view>
    </view>

    <!-- 新建入库单 -->
    <view v-if="createSheet" class="sheet-mask" @click="createSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建入库单</view>
        <view class="field">
          <text class="field-label required">入库单号</text>
          <input class="input" v-model.trim="form.receipt_no" placeholder="如 MR-0001" />
        </view>
        <view class="field">
          <text class="field-label required">项目</text>
          <picker :range="projectLabels" :value="projectIndex" @change="projectIndex = $event.detail.value">
            <view class="picker-box">{{ projectLabels[projectIndex] || "选择项目" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">仓库</text>
          <picker :range="warehouseNames" :value="warehouseIndex" @change="warehouseIndex = $event.detail.value">
            <view class="picker-box">{{ warehouseNames[warehouseIndex] || "选择仓库" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">供应商</text>
          <picker :range="supplierNames" :value="supplierIndex" @change="supplierIndex = $event.detail.value">
            <view class="picker-box">{{ supplierNames[supplierIndex] || "选择供应商" }}</view>
          </picker>
        </view>

        <view class="divider"></view>
        <view class="row between" style="margin-bottom: 12rpx">
          <text class="section-title" style="margin: 0">入库明细</text>
          <button class="btn btn-outline btn-sm" @click="addLine">＋ 添加行</button>
        </view>
        <view v-for="(line, i) in form.lines" :key="i" class="line-card">
          <view class="row between" style="margin-bottom: 8rpx">
            <text class="muted">第 {{ i + 1 }} 行</text>
            <text v-if="form.lines.length > 1" class="remove" @click="form.lines.splice(i, 1)">删除</text>
          </view>
          <view class="field">
            <text class="field-label required">物料</text>
            <picker :range="materialNames" :value="line.materialIndex" @change="line.materialIndex = $event.detail.value">
              <view class="picker-box">{{ materialNames[line.materialIndex] || "选择物料" }}</view>
            </picker>
          </view>
          <view class="row gap8">
            <view class="field grow">
              <text class="field-label required">来料数</text>
              <input class="input" v-model="line.received_qty" type="digit" />
            </view>
            <view class="field grow">
              <text class="field-label required">合格数</text>
              <input class="input" v-model="line.pass_qty" type="digit" />
            </view>
            <view class="field grow">
              <text class="field-label">实收数</text>
              <input class="input" v-model="line.accepted_qty" type="digit" placeholder="默认同合格" />
            </view>
          </view>
          <view class="row gap8">
            <view class="field grow">
              <text class="field-label">单价</text>
              <input class="input" v-model="line.unit_price" type="digit" placeholder="0" />
            </view>
            <view class="field grow">
              <text class="field-label">床号</text>
              <input class="input" v-model.trim="line.bed_no" placeholder="可选" />
            </view>
          </view>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveCreate">{{ saving ? "保存中…" : "保存入库单" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { fmtDate } from "../../utils/format";

interface ReceiptItem {
  id: string; receipt_no: string; receipt_date: number; status: string; version: number;
  project_id: string; project_code: string; product_code: string; product_name: string;
  supplier_name?: string; warehouse_name?: string; receiver_name?: string;
  line_count: number; received_total: string; pass_total: string; amount_total: string;
}

interface ReceiptLine {
  id: string; material_id: string; material_code: string; material_name: string;
  specification?: string; unit: string; bed_no?: string; received_qty: string; pass_qty: string;
  accepted_qty: string; unit_price: string; iqc_result?: string; inspector_name?: string;
}

interface ReceiptDetail {
  header: Record<string, unknown>;
  lines: ReceiptLine[];
}

interface LineForm {
  materialIndex: number;
  received_qty: string;
  pass_qty: string;
  accepted_qty: string;
  unit_price: string;
  bed_no: string;
}

const items = ref<ReceiptItem[]>([]);
const loading = ref(false);
const saving = ref(false);

const projects = ref<{ id: string; project_code: string }[]>([]);
const projectLabels = computed(() => projects.value.map((p) => p.project_code));
const projectIndex = ref(0);
const warehouses = ref<{ id: string; name: string }[]>([]);
const warehouseNames = computed(() => warehouses.value.map((w) => w.name));
const warehouseIndex = ref(0);
const suppliers = ref<{ id: string; name: string }[]>([]);
const supplierNames = computed(() => suppliers.value.map((s) => s.name));
const supplierIndex = ref(0);
const materials = ref<{ id: string; name: string; unit?: string }[]>([]);
const materialNames = computed(() => materials.value.map((m) => m.name));

const createSheet = ref(false);
const formError = ref("");
const form = ref({ receipt_no: "", lines: [] as LineForm[] });

const detailSheet = ref(false);
const detailLoading = ref(false);
const detail = ref<ReceiptDetail | null>(null);

const iqcSheet = ref(false);
const iqcTarget = ref<ReceiptLine | null>(null);
const iqcResult = ref("pass");
const iqcDefect = ref("");
const iqcResults = [
  { label: "通过", value: "pass" },
  { label: "不通过", value: "fail" },
  { label: "让步接收", value: "concession" },
];

function statusText(s: string) {
  const map: Record<string, string> = { draft: "草稿", submitted: "已提交", reversed: "已冲销" };
  return map[s] || s || "--";
}
function statusCls(s: string) {
  return s === "submitted" ? "tag-green" : s === "reversed" ? "tag-gray" : "tag-orange";
}
function iqcText(s: string) {
  return s === "pass" ? "通过" : s === "fail" ? "不通过" : s === "concession" ? "让步接收" : s;
}

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: ReceiptItem[] }>("/material-receipts", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadOptions() {
  try {
    const [p, w, s, m] = await Promise.all([
      request<{ items: { id: string; project_code: string }[] }>("/projects", { silent: true }),
      request<{ items: { id: string; name: string }[] }>("/warehouses", { silent: true }),
      request<{ items: { id: string; name: string }[] }>("/suppliers", { silent: true }),
      request<{ items: { id: string; name: string; unit?: string }[] }>("/materials", { silent: true }),
    ]);
    projects.value = p.items || [];
    warehouses.value = w.items || [];
    suppliers.value = s.items || [];
    materials.value = m.items || [];
  } catch {
    /* 选项加载失败 */
  }
}

function addLine() {
  form.value.lines.push({ materialIndex: 0, received_qty: "", pass_qty: "", accepted_qty: "", unit_price: "", bed_no: "" });
}

function openCreate() {
  form.value = { receipt_no: "", lines: [{ materialIndex: 0, received_qty: "", pass_qty: "", accepted_qty: "", unit_price: "", bed_no: "" }] };
  projectIndex.value = 0;
  warehouseIndex.value = 0;
  supplierIndex.value = 0;
  formError.value = "";
  createSheet.value = true;
}

async function saveCreate() {
  formError.value = "";
  const f = form.value;
  if (!f.receipt_no) { formError.value = "请填写入库单号"; return; }
  const project = projects.value[projectIndex.value];
  const warehouse = warehouses.value[warehouseIndex.value];
  const supplier = suppliers.value[supplierIndex.value];
  if (!project || !warehouse || !supplier) { formError.value = "请选择项目、仓库与供应商"; return; }
  if (!f.lines.length) { formError.value = "请至少添加一行明细"; return; }
  const lines = f.lines.map((l) => {
    const material = materials.value[l.materialIndex];
    return {
      material_id: material?.id || "",
      received_qty: l.received_qty,
      pass_qty: l.pass_qty || l.received_qty,
      accepted_qty: l.accepted_qty || l.pass_qty || l.received_qty,
      unit_price: l.unit_price || "0",
      bed_no: l.bed_no || undefined,
    };
  });
  for (const [i, l] of lines.entries()) {
    if (!l.material_id) { formError.value = `第 ${i + 1} 行请选择物料`; return; }
    if (!Number(l.received_qty) || Number(l.received_qty) <= 0) { formError.value = `第 ${i + 1} 行来料数必须大于 0`; return; }
  }
  saving.value = true;
  try {
    await request("/material-receipts", {
      method: "POST",
      data: {
        receipt_no: f.receipt_no,
        project_id: project.id,
        warehouse_id: warehouse.id,
        supplier_id: supplier.id,
        lines,
      },
      idempotency: true,
    });
    uni.showToast({ title: "入库单已创建", icon: "none" });
    createSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

async function openDetail(r: ReceiptItem) {
  detailSheet.value = true;
  detailLoading.value = true;
  try {
    const res = await request<ReceiptDetail>(`/material-receipts/${r.id}`, { silent: true });
    detail.value = res;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    detailLoading.value = false;
  }
}

function openIqc(l: ReceiptLine) {
  iqcTarget.value = l;
  iqcResult.value = "pass";
  iqcDefect.value = "";
  iqcSheet.value = true;
}

async function saveIqc() {
  const l = iqcTarget.value;
  if (!l) return;
  saving.value = true;
  try {
    await request(`/material-receipt-lines/${l.id}/iqc`, {
      method: "POST",
      data: {
        result: iqcResult.value,
        defect_description: iqcDefect.value || undefined,
      },
    });
    uni.showToast({ title: "检验结果已提交", icon: "none" });
    iqcSheet.value = false;
    if (detail.value) openDetail({ id: detail.value.header.id as string } as ReceiptItem);
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "提交失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

function submit(r: ReceiptItem) {
  uni.showModal({
    title: "提交入库",
    content: `确定提交入库单「${r.receipt_no}」吗？提交后计入库存。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/material-receipts/${r.id}/submit`, { method: "POST", data: { version: r.version } });
        uni.showToast({ title: "已提交", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "提交失败", icon: "none" });
      }
    },
  });
}

function remove(r: ReceiptItem) {
  uni.showModal({
    title: "删除入库单",
    content: "仅草稿入库单可删除，确定删除吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/material-receipts/${r.id}`, { method: "DELETE" });
        uni.showToast({ title: "已删除", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "删除失败", icon: "none" });
      }
    },
  });
}

function reverse(r: ReceiptItem) {
  uni.showModal({
    title: "冲销入库单",
    content: `确定冲销「${r.receipt_no}」吗？将反向扣减库存。`,
    success: async (res) => {
      if (!res.confirm) return;
      const reason = await new Promise<string>((resolve) => {
        uni.showModal({
          title: "冲销原因",
          editable: true,
          placeholderText: "请填写冲销原因",
          success: (r2) => resolve(r2.confirm ? r2.content || "" : ""),
        });
      });
      if (!reason) return;
      try {
        await request(`/material-receipts/${r.id}/reverse`, { method: "POST", data: { version: r.version, reason } });
        uni.showToast({ title: "已冲销", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "冲销失败", icon: "none" });
      }
    },
  });
}

onLoad(() => {
  load();
  loadOptions();
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 90vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
.line-card { background: #f8fafc; border-radius: 12rpx; padding: 20rpx; margin-bottom: 16rpx; }
.remove { color: #b91c1c; font-size: 24rpx; }
</style>
