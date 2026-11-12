<template>
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">{{ workOrderCode ? `工单 ${workOrderCode} · 任务` : "任务与派工" }}</view>
    </view>
    <view class="chips">
      <view
        v-for="s in statusOptions"
        :key="s.value"
        class="chip"
        :class="{ on: status === s.value }"
        @click="status = s.value; reload()"
      >{{ s.label }}</view>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无任务</view>

    <view v-for="t in items" :key="t.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 30rpx">{{ t.process_name }}</text>
            <text class="tag" :class="statusCls(t)">{{ statusText(t) }}</text>
          </view>
          <text class="muted">{{ t.process_code }} · {{ t.parent_task_id ? `派给 ${t.employee_name || "—"}` : "工序任务" }}</text>
        </view>
      </view>
      <view class="row between" style="margin-top: 16rpx">
        <text class="muted">计划 {{ t.planned_qty }} · 已报 {{ t.reported_qty }} · 良品 {{ t.approved_good_qty }} · 余 {{ t.remaining_qty }}</text>
        <text class="muted">单价 {{ t.unit_price }}</text>
      </view>
      <view class="actions">
        <button v-if="!t.parent_task_id && t.status !== 'completed' && canAssign" class="btn btn-solid btn-sm" @click="openAssign(t)">派工</button>
        <button v-if="!t.parent_task_id && t.status !== 'completed' && t.report_mode === 'A' && canAssign" class="btn btn-outline btn-sm" @click="openBundleAssign(t)">扎级派工</button>
        <button v-if="t.status === 'assigned' && canAssign" class="btn btn-solid btn-sm" @click="action(t, 'start')">开工</button>
        <button v-if="t.status === 'in_progress' && canAssign" class="btn btn-outline btn-sm" @click="action(t, 'pause')">暂停</button>
        <button v-if="(t.status === 'in_progress' || t.status === 'paused') && canAssign" class="btn btn-solid btn-sm" @click="action(t, 'complete')">完成</button>
        <button v-if="t.parent_task_id && t.remaining_qty > 0 && t.status !== 'paused' && t.status !== 'completed' && t.status !== 'cancelled' && canReport" class="btn btn-solid btn-sm" @click="openReport(t)">报工</button>
        <button v-if="t.parent_task_id" class="btn btn-ghost btn-sm" @click="viewReports(t)">报工记录</button>
      </view>
    </view>

    <!-- 派工弹层 -->
    <view v-if="assignSheet" class="sheet-mask" @click="assignSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">派工 · {{ assignTarget?.process_name }}</view>
        <text class="muted" style="display: block; margin-bottom: 16rpx">父任务余额 {{ assignTarget?.remaining_qty || 0 }}</text>
        <view class="field">
          <text class="field-label required">员工</text>
          <picker :range="employeeNames" :value="employeeIndex" @change="employeeIndex = $event.detail.value">
            <view class="picker-box">{{ employeeNames[employeeIndex] || "请选择员工" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">派工数量</text>
          <input class="input" v-model="assignQty" type="number" placeholder="派工数量" />
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="assignRemark" placeholder="可选" />
        </view>
        <view v-if="assignError" class="error-text">{{ assignError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="submitAssign">{{ saving ? "派工中…" : "确认派工" }}</button>
      </view>
    </view>

    <!-- 报工记录弹层 -->
    <view v-if="reportSheet" class="sheet-mask" @click="reportSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">报工记录</view>
        <view v-if="reportsLoading" class="empty">加载中…</view>
        <view v-else-if="!reports.length" class="empty">暂无报工</view>
        <view v-for="r in reports" :key="r.id" class="list-row">
          <view class="grow">
            <text class="title" style="font-size: 28rpx">良品 {{ r.good_qty }} · 次品 {{ r.defect_qty }}</text>
            <text class="muted">{{ fmtDateTime(r.occurred_at) }} · {{ r.review_status === "approved" ? "已审批" : r.review_status === "pending" ? "待审批" : r.review_status === "rejected" ? "已驳回" : r.review_status }}</text>
          </view>
        </view>
      </view>
    </view>
    <!-- 报工弹层 -->
    <view v-if="reportSheet" class="sheet-mask" @click="reportSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">快捷报工 · {{ reportTarget?.process_name }}</view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label required">良品数</text>
            <input class="input" v-model="reportGood" type="number" />
          </view>
          <view class="field grow">
            <text class="field-label">次品数</text>
            <input class="input" v-model="reportDefect" type="number" />
          </view>
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="reportRemark" placeholder="可选" />
        </view>
        <view v-if="reportError" class="error-text">{{ reportError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="submitReport">{{ saving ? "提交中…" : "提交报工" }}</button>
      </view>
    </view>

    <!-- 扎级派工弹层 -->
    <view v-if="bundleSheet" class="sheet-mask" @click="bundleSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">扎级派工 · {{ bundleTarget?.process_name }}</view>
        <view v-if="bundlesLoading" class="empty">加载中…</view>
        <template v-else>
          <view class="field">
            <text class="field-label required">扎（显示可派余额）</text>
            <picker :range="bundleLabels" :value="bundleIndex" @change="bundleIndex = $event.detail.value">
              <view class="picker-box">{{ bundleLabels[bundleIndex] || "选择扎" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label required">员工</text>
            <picker :range="employeeNames" :value="bundleEmployeeIndex" @change="bundleEmployeeIndex = $event.detail.value">
              <view class="picker-box">{{ employeeNames[bundleEmployeeIndex] || "请选择员工" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label required">派工数量</text>
            <input class="input" v-model="bundleQty" type="number" />
          </view>
          <view v-if="bundleError" class="error-text">{{ bundleError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="submitBundleAssign">{{ saving ? "派工中…" : "确认扎级派工" }}</button>
        </template>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { auth } from "../../store/auth";
import { fmtDateTime } from "../../utils/format";

interface Task {
  id: string;
  work_order_id: string;
  parent_task_id?: string;
  employee_id?: string;
  employee_name?: string;
  process_code: string;
  process_name: string;
  planned_qty: number;
  reported_qty: number;
  approved_good_qty: number;
  remaining_qty: number;
  unit_price: string;
  status: string;
  version: number;
  report_mode: string;
}

interface TaskReport {
  id: string;
  report_kind: string;
  good_qty: number;
  defect_qty: number;
  employee_id: string;
  review_status: string;
  occurred_at: number;
}

const workOrderId = ref("");
const workOrderCode = ref("");
const items = ref<Task[]>([]);
const loading = ref(false);
const status = ref("");
const statusOptions = [
  { label: "全部", value: "" },
  { label: "未派工", value: "unassigned" },
  { label: "已派工", value: "assigned" },
  { label: "进行中", value: "in_progress" },
  { label: "已暂停", value: "paused" },
  { label: "已完成", value: "completed" },
];

const canAssign = computed(() => auth.hasPerm("task:assign"));
const canReport = computed(() => auth.hasPerm("report:create"));

interface AssignableBundle {
  bundle_id: string; bundle_no: string; color: string; size: string;
  bundle_qty: number; assigned_qty: number; assignable_qty: number;
}

const employees = ref<{ id: string; name: string }[]>([]);
const employeeNames = computed(() => employees.value.map((e) => e.name));
const employeeIndex = ref(0);

const assignSheet = ref(false);
const assignTarget = ref<Task | null>(null);
const assignQty = ref("");
const assignRemark = ref("");
const assignError = ref("");
const saving = ref(false);

const reportSheet = ref(false);
const reportsLoading = ref(false);
const reports = ref<TaskReport[]>([]);

/* 快捷报工 */
const reportTarget = ref<Task | null>(null);
const reportGood = ref("");
const reportDefect = ref("0");
const reportRemark = ref("");
const reportError = ref("");

/* 扎级派工 */
const bundleSheet = ref(false);
const bundleTarget = ref<Task | null>(null);
const bundlesLoading = ref(false);
const assignableBundles = ref<AssignableBundle[]>([]);
const bundleIndex = ref(0);
const bundleEmployeeIndex = ref(0);
const bundleQty = ref("");
const bundleError = ref("");
const bundleLabels = computed(() =>
  assignableBundles.value.map((b) => `${b.bundle_no} 扎 · ${b.color} ${b.size} · 可派 ${b.assignable_qty}`)
);

function statusText(t: Task): string {
  const map: Record<string, string> = {
    unassigned: "未派工",
    assigned: "已派工",
    in_progress: "进行中",
    paused: "已暂停",
    completed: "已完成",
    cancelled: "已取消",
  };
  return map[t.status] || t.status || "--";
}
function statusCls(t: Task): string {
  const s = t.status;
  return s === "in_progress" ? "tag-blue" : s === "completed" ? "tag-green" : s === "paused" ? "tag-orange" : s === "assigned" ? "tag-purple" : "tag-gray";
}

async function reload() {
  loading.value = true;
  try {
    const q: string[] = [];
    if (workOrderId.value) q.push(`work_order_id=${workOrderId.value}`);
    if (status.value) q.push(`status=${status.value}`);
    const res = await request<{ items: Task[] }>(`/production-tasks${q.length ? "?" + q.join("&") : ""}`, { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadEmployees() {
  try {
    const res = await request<{ items: { id: string; name: string; status: string }[] }>("/employees?status=active", { silent: true });
    employees.value = res.items || [];
  } catch {
    employees.value = [];
  }
}

function openAssign(t: Task) {
  assignTarget.value = t;
  assignQty.value = "";
  assignRemark.value = "";
  assignError.value = "";
  employeeIndex.value = 0;
  assignSheet.value = true;
}

async function submitAssign() {
  assignError.value = "";
  const t = assignTarget.value;
  const emp = employees.value[employeeIndex.value];
  if (!t || !emp) {
    assignError.value = "请选择员工";
    return;
  }
  const qty = Number(assignQty.value);
  if (!qty || qty <= 0) {
    assignError.value = "请填写派工数量";
    return;
  }
  saving.value = true;
  try {
    await request(`/production-tasks/${t.id}/assign`, {
      method: "POST",
      data: {
        version: t.version,
        assignments: [{ employee_id: emp.id, planned_qty: qty }],
        remark: assignRemark.value || undefined,
      },
    });
    uni.showToast({ title: "派工成功", icon: "none" });
    assignSheet.value = false;
    reload();
  } catch (e) {
    assignError.value = (e as Error).message || "派工失败";
  } finally {
    saving.value = false;
  }
}

function action(t: Task, act: "start" | "pause" | "complete") {
  const textMap: Record<string, string> = { start: "开工", pause: "暂停", complete: "完成" };
  uni.showModal({
    title: textMap[act],
    content: `确定${textMap[act]}任务「${t.process_name}」吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/production-tasks/${t.id}/${act}`, {
          method: "POST",
          data: { version: t.version },
        });
        uni.showToast({ title: "操作成功", icon: "none" });
        reload();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "操作失败", icon: "none" });
      }
    },
  });
}

async function viewReports(t: Task) {
  reportSheet.value = true;
  reportsLoading.value = true;
  try {
    const res = await request<{ items: TaskReport[] }>(`/production-tasks/${t.id}/reports`, { silent: true });
    reports.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    reportsLoading.value = false;
  }
}

/* ===== 快捷报工 ===== */

function openReport(t: Task) {
  reportTarget.value = t;
  reportGood.value = "";
  reportDefect.value = "0";
  reportRemark.value = "";
  reportError.value = "";
  reportSheet.value = true;
}

async function submitReport() {
  reportError.value = "";
  const t = reportTarget.value;
  if (!t) return;
  const good = Number(reportGood.value);
  const defect = Number(reportDefect.value || 0);
  if (!good || good <= 0) { reportError.value = "请填写良品数"; return; }
  if (defect < 0) { reportError.value = "次品数不能为负"; return; }
  saving.value = true;
  try {
    await request("/work-reports", {
      method: "POST",
      data: {
        task_id: t.id,
        bundle_id: "",
        good_qty: good,
        defect_qty: defect,
        report_kind: "normal",
        occurred_at: Date.now() * 1000,
        remark: reportRemark.value || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "报工已提交", icon: "none" });
    reportSheet.value = false;
    reload();
  } catch (e) {
    reportError.value = (e as Error).message || "报工失败，可使用相同命令安全重试";
  } finally {
    saving.value = false;
  }
}

/* ===== 扎级派工 ===== */

async function openBundleAssign(t: Task) {
  bundleTarget.value = t;
  bundleSheet.value = true;
  bundlesLoading.value = true;
  bundleIndex.value = 0;
  bundleEmployeeIndex.value = 0;
  bundleQty.value = "";
  bundleError.value = "";
  try {
    const res = await request<{ items: AssignableBundle[] }>(
      `/work-orders/${t.work_order_id}/assignable-bundles`, { silent: true }
    );
    assignableBundles.value = (res.items || []).filter((b) => b.assignable_qty > 0);
  } catch (e) {
    bundleError.value = (e as Error).message || "加载可派扎失败";
  } finally {
    bundlesLoading.value = false;
  }
}

async function submitBundleAssign() {
  bundleError.value = "";
  const t = bundleTarget.value;
  const bundle = assignableBundles.value[bundleIndex.value];
  const emp = employees.value[bundleEmployeeIndex.value];
  if (!t || !bundle || !emp) { bundleError.value = "请选择扎与员工"; return; }
  const qty = Number(bundleQty.value);
  if (!qty || qty <= 0) { bundleError.value = "请填写派工数量"; return; }
  if (qty > bundle.assignable_qty) {
    bundleError.value = `派工数量不能超过扎可派余额 ${bundle.assignable_qty}`;
    return;
  }
  saving.value = true;
  try {
    await request(`/production-tasks/${t.id}/bundle-assignments`, {
      method: "POST",
      data: {
        version: t.version,
        items: [{ bundle_id: bundle.bundle_id, employee_id: emp.id, planned_qty: qty }],
      },
    });
    uni.showToast({ title: "扎级派工成功", icon: "none" });
    bundleSheet.value = false;
    reload();
  } catch (e) {
    bundleError.value = (e as Error).message || "派工失败";
  } finally {
    saving.value = false;
  }
}

onLoad((query) => {
  const q = (query as Record<string, string>) || {};
  workOrderId.value = q.work_order_id || "";
  workOrderCode.value = q.work_order_code ? decodeURIComponent(q.work_order_code) : "";
  if (auth.hasSession && !Object.keys(auth.permissions).length) auth.fetchPermissions();
  loadEmployees().then(reload);
});
</script>

<style scoped>
.sheet-mask {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  z-index: 50;
  display: flex;
  align-items: flex-end;
}
.sheet {
  background: #fff;
  border-radius: 24rpx 24rpx 0 0;
  padding: 32rpx;
  width: 100%;
  max-height: 86vh;
  overflow-y: auto;
  box-sizing: border-box;
}
.error-text {
  color: #d92d20;
  font-size: 24rpx;
  margin-top: 12rpx;
}
</style>
