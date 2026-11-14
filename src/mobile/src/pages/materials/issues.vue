<template>
  <BrandNavBar title="领料管理" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">领料管理</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建领料单</button>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无领料单</view>

    <view v-for="i in items" :key="i.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ i.issue_no }}</text>
            <text class="tag" :class="i.status === 'posted' ? 'tag-green' : 'tag-gray'">{{ i.status === "posted" ? "已领料" : "已冲销" }}</text>
          </view>
          <text class="muted">{{ i.work_order_code }} · {{ i.material_name }} · {{ i.warehouse_name }}</text>
        </view>
        <view class="col" style="align-items: flex-end">
          <text class="num" style="font-size: 34rpx">{{ Number(i.qty) }}</text>
          <text class="muted">{{ i.unit }}</text>
        </view>
      </view>
      <view class="row between" style="margin-top: 12rpx">
        <text class="muted">{{ i.purpose || "无用途说明" }} · {{ i.operator_name || "未记录" }}</text>
        <text class="muted">{{ i.issue_date }}</text>
      </view>
      <view class="actions" v-if="i.status === 'posted'">
        <button class="btn btn-danger btn-sm" @click="reverse(i)">冲销</button>
      </view>
    </view>

    <view v-if="createSheet" class="sheet-mask" @click="createSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建领料单</view>
        <view class="field">
          <text class="field-label required">领料单号</text>
          <input class="input" v-model.trim="form.issue_no" placeholder="如 MI-0001" />
        </view>
        <view class="field">
          <text class="field-label required">领料日期</text>
          <picker mode="date" :value="form.issue_date" @change="form.issue_date = $event.detail.value">
            <view class="picker-box">{{ form.issue_date || "选择日期" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">工单</text>
          <picker :range="workOrderLabels" :value="woIndex" @change="woIndex = $event.detail.value">
            <view class="picker-box">{{ workOrderLabels[woIndex] || "选择工单" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">仓库</text>
          <picker :range="warehouseNames" :value="warehouseIndex" @change="warehouseIndex = $event.detail.value">
            <view class="picker-box">{{ warehouseNames[warehouseIndex] || "选择仓库" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">物料</text>
          <picker :range="materialNames" :value="materialIndex" @change="materialIndex = $event.detail.value">
            <view class="picker-box">{{ materialNames[materialIndex] || "选择物料" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">领料数量</text>
          <input class="input" v-model="form.qty" type="digit" placeholder="数量" />
        </view>
        <view class="field">
          <text class="field-label">用途</text>
          <input class="input" v-model.trim="form.purpose" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="form.remark" placeholder="可选" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveCreate">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface IssueItem {
  id: string; issue_no: string; issue_date: string;
  work_order_code: string; warehouse_name: string; material_name: string;
  unit: string; qty: string; purpose?: string; operator_name?: string; status: string; version: number;
}

const items = ref<IssueItem[]>([]);
const loading = ref(false);
const saving = ref(false);

const workOrders = ref<{ id: string; work_order_code: string }[]>([]);
const workOrderLabels = computed(() => workOrders.value.map((w) => w.work_order_code));
const woIndex = ref(0);
const warehouses = ref<{ id: string; name: string }[]>([]);
const warehouseNames = computed(() => warehouses.value.map((w) => w.name));
const warehouseIndex = ref(0);
const materials = ref<{ id: string; name: string; unit?: string }[]>([]);
const materialNames = computed(() => materials.value.map((m) => m.name));
const materialIndex = ref(0);

const createSheet = ref(false);
const formError = ref("");
const form = ref({ issue_no: "", issue_date: "", qty: "", purpose: "", remark: "" });

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: IssueItem[] }>("/material-issues", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadOptions() {
  try {
    const [w, wh, m] = await Promise.all([
      request<{ items: { id: string; work_order_code: string }[] }>("/work-orders", { silent: true }),
      request<{ items: { id: string; name: string }[] }>("/warehouses", { silent: true }),
      request<{ items: { id: string; name: string; unit?: string }[] }>("/materials", { silent: true }),
    ]);
    workOrders.value = w.items || [];
    warehouses.value = wh.items || [];
    materials.value = m.items || [];
  } catch {
    /* 选项加载失败 */
  }
}

function openCreate() {
  form.value = { issue_no: "", issue_date: new Date().toISOString().slice(0, 10), qty: "", purpose: "", remark: "" };
  woIndex.value = 0;
  warehouseIndex.value = 0;
  materialIndex.value = 0;
  formError.value = "";
  createSheet.value = true;
}

async function saveCreate() {
  formError.value = "";
  const f = form.value;
  const wo = workOrders.value[woIndex.value];
  const wh = warehouses.value[warehouseIndex.value];
  const m = materials.value[materialIndex.value];
  if (!f.issue_no) { formError.value = "请填写领料单号"; return; }
  if (!wo || !wh || !m) { formError.value = "请选择工单、仓库与物料"; return; }
  if (!Number(f.qty) || Number(f.qty) <= 0) { formError.value = "领料数量必须大于 0"; return; }
  saving.value = true;
  try {
    await request("/material-issues", {
      method: "POST",
      data: {
        issue_no: f.issue_no,
        issue_date: f.issue_date,
        work_order_id: wo.id,
        warehouse_id: wh.id,
        material_id: m.id,
        qty: f.qty,
        purpose: f.purpose || undefined,
        remark: f.remark || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "领料单已创建", icon: "none" });
    createSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function reverse(i: IssueItem) {
  uni.showModal({
    title: "冲销领料单",
    editable: true,
    placeholderText: "请填写冲销原因",
    content: "",
    success: async (res) => {
      if (!res.confirm) return;
      const reason = res.content || "";
      if (!reason) {
        uni.showToast({ title: "请填写冲销原因", icon: "none" });
        return;
      }
      try {
        await request(`/material-issues/${i.id}/reverse`, { method: "POST", data: { reason } });
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
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
