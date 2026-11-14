<template>
  <BrandNavBar title="返工单详情" />
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!order" class="empty">返工单不存在</view>
    <template v-else>
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title">{{ order.rework_no }}</text>
              <text class="tag" :class="statusCls">{{ statusText }}</text>
            </view>
            <text class="muted">返工 {{ order.qty }} · 已报 {{ order.reported_qty }}</text>
          </view>
        </view>
        <view class="kv" style="margin-top: 16rpx"><text class="k">返工人员</text><text class="v">{{ order.employee_name || "未分派" }}</text></view>
        <view class="kv"><text class="k">原因</text><text class="v">{{ order.reason || "--" }}</text></view>
        <view class="kv"><text class="k">到期时间</text><text class="v">{{ order.due_at ? fmtDate(order.due_at) : "--" }}</text></view>
      </view>

      <view class="actions" style="margin-top: 24rpx">
        <button v-if="order.status !== 'completed' && order.status !== 'cancelled'" class="btn btn-solid btn-sm" @click="openAssign">分派人员</button>
        <button v-if="order.status !== 'completed' && order.status !== 'cancelled'" class="btn btn-outline btn-sm" @click="openReport">返工报工</button>
        <button v-if="order.reported_qty > 0 && order.status !== 'completed' && order.status !== 'cancelled'" class="btn btn-outline btn-sm" @click="openReinspection">复检</button>
      </view>

      <view v-if="reinspections.length" class="section-title" style="margin-top: 32rpx">复检记录</view>
      <view v-for="r in reinspections" :key="r.id" class="card">
        <view class="row between">
          <text class="muted">{{ fmtDateTime(r.created_at) }}</text>
          <text class="muted">{{ r.inspector_employee_id }}</text>
        </view>
        <view class="row between" style="margin-top: 12rpx">
          <text class="muted">检 {{ r.inspected_qty }} · 合格 {{ r.passed_qty }} · 返工 {{ r.rework_qty }} · 报废 {{ r.scrapped_qty }}</text>
        </view>
        <text v-if="r.remark" class="muted" style="margin-top: 8rpx">{{ r.remark }}</text>
      </view>

      <!-- 分派 -->
      <view v-if="assignSheet" class="sheet-mask" @click="assignSheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">分派返工人员</view>
          <view class="field">
            <text class="field-label required">员工</text>
            <picker :range="employeeNames" :value="employeeIndex" @change="employeeIndex = $event.detail.value">
              <view class="picker-box">{{ employeeNames[employeeIndex] || "选择员工" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label">到期时间</text>
            <picker mode="date" :value="assignDue" @change="assignDue = $event.detail.value">
              <view class="picker-box">{{ assignDue || "选择日期（可选）" }}</view>
            </picker>
          </view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="saveAssign">{{ saving ? "分派中…" : "确认分派" }}</button>
        </view>
      </view>

      <!-- 报工 -->
      <view v-if="reportSheet" class="sheet-mask" @click="reportSheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">返工报工</view>
          <view class="field">
            <text class="field-label required">报工数量（余 {{ order.qty - order.reported_qty }}）</text>
            <input class="input" v-model="reportQty" type="number" />
          </view>
          <view class="field">
            <text class="field-label">备注</text>
            <input class="input" v-model.trim="reportRemark" placeholder="可选" />
          </view>
          <view v-if="formError" class="error-text">{{ formError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="saveReport">{{ saving ? "报工中…" : "提交报工" }}</button>
        </view>
      </view>

      <!-- 复检 -->
      <view v-if="reinspectionSheet" class="sheet-mask" @click="reinspectionSheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">返工复检</view>
          <view class="row gap8">
            <view class="field grow">
              <text class="field-label required">复检数</text>
              <input class="input" v-model="reForm.inspected_qty" type="number" />
            </view>
            <view class="field grow">
              <text class="field-label required">合格数</text>
              <input class="input" v-model="reForm.passed_qty" type="number" />
            </view>
          </view>
          <view class="row gap8">
            <view class="field grow">
              <text class="field-label">返工数</text>
              <input class="input" v-model="reForm.rework_qty" type="number" />
            </view>
            <view class="field grow">
              <text class="field-label">报废数</text>
              <input class="input" v-model="reForm.scrapped_qty" type="number" />
            </view>
          </view>
          <view class="field">
            <text class="field-label">备注</text>
            <input class="input" v-model.trim="reForm.remark" placeholder="可选" />
          </view>
          <view v-if="formError" class="error-text">{{ formError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="saveReinspection">{{ saving ? "提交中…" : "提交复检" }}</button>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { fmtDate, fmtDateTime } from "../../utils/format";

interface ReworkOrder {
  id: string; rework_no: string; qty: number; reported_qty: number; status: string;
  employee_id?: string; employee_name?: string; reason?: string; due_at?: number; version: number;
}
interface Reinspection {
  id: string; inspected_qty: number; passed_qty: number; rework_qty: number; scrapped_qty: number;
  inspector_employee_id: string; remark?: string; created_at: number;
}

const id = ref("");
const loading = ref(false);
const saving = ref(false);
const order = ref<ReworkOrder | null>(null);
const reinspections = ref<Reinspection[]>([]);

const employees = ref<{ id: string; name: string }[]>([]);
const employeeNames = computed(() => employees.value.map((e) => e.name));
const employeeIndex = ref(0);

const assignSheet = ref(false);
const assignDue = ref("");
const reportSheet = ref(false);
const reportQty = ref("");
const reportRemark = ref("");
const reinspectionSheet = ref(false);
const formError = ref("");
const reForm = ref({ inspected_qty: "", passed_qty: "", rework_qty: "0", scrapped_qty: "0", remark: "" });

const statusText = computed(() => {
  const s = order.value?.status || "";
  const map: Record<string, string> = { pending: "待处理", assigned: "已分派", reported: "已报工", completed: "已完成", cancelled: "已取消" };
  return map[s] || s || "--";
});
const statusCls = computed(() => {
  const s = order.value?.status || "";
  return s === "completed" ? "tag-green" : s === "reported" ? "tag-blue" : s === "assigned" ? "tag-purple" : s === "cancelled" ? "tag-gray" : "tag-orange";
});

async function load() {
  loading.value = true;
  try {
    const res = await request<{ order: ReworkOrder; reinspections: Reinspection[] }>(`/rework-orders/${id.value}`, { silent: true });
    order.value = res.order;
    reinspections.value = res.reinspections || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadEmployees() {
  try {
    const res = await request<{ items: { id: string; name: string }[] }>("/employees?status=active", { silent: true });
    employees.value = res.items || [];
  } catch {
    employees.value = [];
  }
}

function openAssign() {
  employeeIndex.value = 0;
  assignDue.value = "";
  assignSheet.value = true;
}

async function saveAssign() {
  const emp = employees.value[employeeIndex.value];
  if (!emp) {
    uni.showToast({ title: "请选择员工", icon: "none" });
    return;
  }
  saving.value = true;
  try {
    await request(`/rework-orders/${id.value}/assign`, {
      method: "POST",
      data: {
        employee_id: emp.id,
        due_at: assignDue.value ? new Date(assignDue.value).getTime() * 1000 : undefined,
      },
    });
    uni.showToast({ title: "已分派", icon: "none" });
    assignSheet.value = false;
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "分派失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

function openReport() {
  reportQty.value = "";
  reportRemark.value = "";
  formError.value = "";
  reportSheet.value = true;
}

async function saveReport() {
  formError.value = "";
  const qty = Number(reportQty.value);
  if (!qty || qty <= 0) { formError.value = "请填写报工数量"; return; }
  if (order.value && qty > order.value.qty - order.value.reported_qty) {
    formError.value = `报工数量不能超过余额 ${order.value.qty - order.value.reported_qty}`;
    return;
  }
  saving.value = true;
  try {
    await request(`/rework-orders/${id.value}/report`, {
      method: "POST",
      data: { qty, remark: reportRemark.value || undefined },
    });
    uni.showToast({ title: "报工成功", icon: "none" });
    reportSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "报工失败";
  } finally {
    saving.value = false;
  }
}

function openReinspection() {
  reForm.value = { inspected_qty: "", passed_qty: "", rework_qty: "0", scrapped_qty: "0", remark: "" };
  formError.value = "";
  reinspectionSheet.value = true;
}

async function saveReinspection() {
  formError.value = "";
  const f = reForm.value;
  const inspected = Number(f.inspected_qty);
  const passed = Number(f.passed_qty);
  const rework = Number(f.rework_qty || 0);
  const scrapped = Number(f.scrapped_qty || 0);
  if (!inspected || inspected !== passed + rework + scrapped) {
    formError.value = "复检数必须等于合格 + 返工 + 报废";
    return;
  }
  saving.value = true;
  try {
    await request(`/rework-orders/${id.value}/reinspections`, {
      method: "POST",
      data: {
        inspected_qty: inspected,
        passed_qty: passed,
        rework_qty: rework,
        scrapped_qty: scrapped,
        remark: f.remark || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "复检已提交", icon: "none" });
    reinspectionSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "提交失败";
  } finally {
    saving.value = false;
  }
}

onLoad((query) => {
  id.value = ((query as Record<string, string>) || {}).id || "";
  load();
  loadEmployees();
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
