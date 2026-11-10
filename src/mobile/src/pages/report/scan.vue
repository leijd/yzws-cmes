<template>
  <view class="page">
    <!-- 第一步：扫码 -->
    <view v-if="!ticket" class="scan-area">
      <button class="scan-btn" @click="scanCode">扫 码 派 工 / 报 工</button>
      <view class="manual">
        <input
          v-model.trim="manualCode"
          placeholder="手动输入菲票号 / 扎号 / 扫码链接"
          confirm-type="done"
          @confirm="resolve(manualCode)"
        />
        <button size="mini" @click="resolve(manualCode)">确定</button>
      </view>
      <view class="hint">二维码内容也可以是 /s/租户/菲票码 形式的链接</view>
    </view>

    <!-- 第二步：确认扎信息 -->
    <view v-else class="card">
      <view class="row between">
        <text class="wo">{{ ticket.work_order_code }}</text>
        <text class="mode">{{ ticket.report_mode === 'P' ? '按件' : '按扎' }}</text>
      </view>
      <view class="row">
        <text class="label">项目号</text>
        <text class="value">{{ ticket.project_code || "—" }}</text>
      </view>
      <view class="row">
        <text class="label">品牌</text>
        <text class="value">{{ ticket.customer_name || "—" }}</text>
      </view>
      <view class="row">
        <text class="label">款式</text>
        <text class="value">{{ ticket.product_name }}（{{ ticket.product_code }}）</text>
      </view>
      <view class="row">
        <text class="label">扎号</text>
        <text class="value">{{ ticket.bundle_no }} {{ ticket.part_code ? " / " + ticket.part_name : "" }}</text>
      </view>
      <view class="row">
        <text class="label">颜色/尺码</text>
        <text class="value">{{ ticket.color || "—" }} / {{ ticket.size || "—" }}</text>
      </view>
      <view class="row">
        <text class="label">本扎数量</text>
        <text class="value">{{ ticket.current_qty }} 件</text>
      </view>
      <view class="row">
        <text class="label">裁床人</text>
        <text class="value">{{ ticket.cutter_name || "—" }}</text>
      </view>
      <view class="row">
        <text class="label">床次</text>
        <text class="value">{{ ticket.bed_no || "—" }}</text>
      </view>
      <button class="reset small" @click="reset">重新扫码</button>
    </view>

    <!-- 第三步：选择报工 / 派工 -->
    <view v-if="ticket" class="card">
      <view class="tabs" v-if="canDispatch">
        <view class="tab" :class="{ active: mode === 'report' }" @click="mode = 'report'">报工</view>
        <view class="tab" :class="{ active: mode === 'dispatch' }" @click="mode = 'dispatch'">派工</view>
      </view>
      <text class="card-title">选择工序</text>
      <view
        v-for="p in tasks"
        :key="p.task_id"
        class="process"
        :class="{ active: selectedTaskId === p.task_id }"
        @click="selectTask(p.task_id)"
      >
        <view class="process-main">
          <text>{{ p.sequence_no }}. {{ p.process_name }}</text>
          <text class="process-sub">已报 {{ p.reported_qty }} / 计划 {{ p.planned_qty }}</text>
        </view>
        <text v-if="selectedTaskId === p.task_id" class="check">✓</text>
      </view>
      <view v-if="!tasks.length" class="hint">该工单暂无工序任务。</view>
    </view>

    <!-- 第四步：报工 -->
    <view v-if="selectedTask && mode === 'report'" class="card">
      <text class="card-title">报工数量 · {{ selectedTask.process_name }}</text>
      <view class="qty-row">
        <text class="label">合格数</text>
        <input v-model.number="goodQty" type="number" placeholder="0" />
      </view>
      <view class="qty-row">
        <text class="label">次品数</text>
        <input v-model.number="defectQty" type="number" placeholder="0" />
      </view>
      <view class="qty-row">
        <text class="label">备注</text>
        <input v-model.trim="remark" placeholder="选填" />
      </view>
      <view class="hint">本工序剩余可报 {{ selectedTask.remaining_qty }} 件；同一扎同一工序只能报工一次。</view>
      <button class="submit" :disabled="submitting || !ticket?.can_report" @click="submitReport">
        {{ submitting ? "提交中…" : "提交报工" }}
      </button>
    </view>

    <!-- 第五步：派工 -->
    <view v-if="selectedTask && canDispatch && mode === 'dispatch'" class="card">
      <text class="card-title">派工 · {{ selectedTask.process_name }}</text>
      <view class="qty-row">
        <text class="label">派工员工</text>
        <picker class="picker" :range="employees" range-key="name" @change="onEmployeeChange">
          <view class="picker-value">{{ selectedEmployeeName || "请选择员工" }}</view>
        </picker>
      </view>
      <view class="qty-row">
        <text class="label">派工数量</text>
        <input v-model.number="dispatchQty" type="number" placeholder="0" />
      </view>
      <view class="hint">本扎本工序可派 {{ assignableQty }} 件；派工数量不能超过扎件数。</view>
      <button class="submit" :disabled="submitting || !ticket?.can_assign" @click="submitDispatch">
        {{ submitting ? "提交中…" : "确认派工" }}
      </button>
      <button class="reset" @click="reset">重新扫码</button>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { request } from "../../utils/request";

interface ScanTask {
  task_id: string;
  version: number;
  sequence_no: number;
  process_code: string;
  process_name: string;
  planned_qty: number;
  reported_qty: number;
  remaining_qty: number;
  assigned_qty: number;
}

interface ScanEmployee {
  id: string;
  employee_no: string;
  name: string;
}

interface TicketInfo {
  ticket_id: string;
  bundle_id: string;
  work_order_code: string;
  project_code: string;
  customer_name: string;
  product_code: string;
  product_name: string;
  bundle_no: string;
  part_code: string;
  part_name: string;
  color: string;
  size: string;
  current_qty: number;
  cutter_name: string;
  bed_no: string;
  report_mode: string;
  tasks: ScanTask[];
  employees?: ScanEmployee[];
  can_assign?: boolean;
  can_report?: boolean;
}

const ticket = ref<TicketInfo | null>(null);
const tasks = computed(() => ticket.value?.tasks || []);
const employees = computed(() => ticket.value?.employees || []);
const selectedTaskId = ref("");
const selectedEmployeeId = ref("");
const mode = ref<"report" | "dispatch">("report");
const goodQty = ref<number | null>(null);
const defectQty = ref<number | null>(null);
const remark = ref("");
const dispatchQty = ref<number | null>(null);
const manualCode = ref("");
const submitting = ref(false);

const selectedTask = computed(
  () => tasks.value.find((task) => task.task_id === selectedTaskId.value) || null
);
const selectedEmployeeName = computed(
  () => employees.value.find((item) => item.id === selectedEmployeeId.value)?.name || ""
);
const supportsDispatch = computed(() => ticket.value?.report_mode === "A");
const canDispatch = computed(() => supportsDispatch.value && !!ticket.value?.can_assign);
const assignableQty = computed(() => {
  if (!ticket.value || !selectedTask.value) return 0;
  return Math.max(
    0,
    Math.min(
      selectedTask.value.remaining_qty,
      ticket.value.current_qty - selectedTask.value.assigned_qty
    )
  );
});

function scanCode() {
  // #ifdef H5
  uni.showToast({ title: "H5 请用下方手动输入", icon: "none" });
  // #endif
  // #ifndef H5
  uni.scanCode({
    success: (res) => resolve(res.result),
    fail: () => {},
  });
  // #endif
}

async function resolve(payload: string) {
  if (!payload) {
    uni.showToast({ title: "请输入扫码内容", icon: "none" });
    return;
  }
  try {
    const res = await request<TicketInfo>("/scan/resolve", {
      method: "POST",
      data: { payload },
    });
    ticket.value = res;
    selectedTaskId.value = "";
    selectedEmployeeId.value = "";
    mode.value = res.can_report || res.report_mode !== "A" || !res.can_assign ? "report" : "dispatch";
    goodQty.value = null;
    defectQty.value = null;
    dispatchQty.value = null;
    remark.value = "";
    manualCode.value = "";
    if (res.tasks?.length) {
      selectTask(res.tasks[0].task_id);
    } else {
      uni.showToast({ title: "该扎没有可操作工序", icon: "none" });
    }
  } catch {
    // 错误信息 request 层已 toast
  }
}

function selectTask(taskId: string) {
  selectedTaskId.value = taskId;
  goodQty.value = null;
  defectQty.value = null;
  dispatchQty.value = assignableQty.value > 0 ? assignableQty.value : null;
}

function onEmployeeChange(event: { detail: { value: number | string } }) {
  const index = Number(event.detail.value);
  selectedEmployeeId.value = employees.value[index]?.id || "";
}

async function submitReport() {
  if (!selectedTask.value) return;
  const good = Number(goodQty.value || 0);
  const defect = Number(defectQty.value || 0);
  if (good <= 0 && defect <= 0) {
    uni.showToast({ title: "请填写报工数量", icon: "none" });
    return;
  }
  submitting.value = true;
  try {
    await request("/work-reports", {
      method: "POST",
      idempotency: true,
      data: {
        task_id: selectedTask.value.task_id,
        bundle_id: ticket.value!.bundle_id,
        good_qty: good,
        defect_qty: defect,
        report_kind: "normal",
        occurred_at: Date.now() * 1000,
        remark: remark.value || null,
      },
    });
    uni.showToast({ title: "报工成功", icon: "success" });
    reset();
  } catch {
    // 失败保留输入，便于修改后重试
  } finally {
    submitting.value = false;
  }
}

async function submitDispatch() {
  if (!canDispatch.value) {
    uni.showToast({ title: "当前账号无派工权限或工单不支持扎级派工", icon: "none" });
    return;
  }
  if (!selectedTask.value) return;
  if (!selectedEmployeeId.value) {
    uni.showToast({ title: "请选择派工员工", icon: "none" });
    return;
  }
  const qty = Number(dispatchQty.value || 0);
  if (qty <= 0) {
    uni.showToast({ title: "请填写派工数量", icon: "none" });
    return;
  }
  if (qty > assignableQty.value) {
    uni.showToast({ title: `派工数量不能超过 ${assignableQty.value}`, icon: "none" });
    return;
  }
  submitting.value = true;
  try {
    await request(`/production-tasks/${selectedTask.value.task_id}/bundle-assignments`, {
      method: "POST",
      data: {
        version: selectedTask.value.version,
        items: [
          {
            bundle_id: ticket.value!.bundle_id,
            employee_id: selectedEmployeeId.value,
            planned_qty: qty,
          },
        ],
      },
    });
    uni.showToast({ title: "派工成功", icon: "success" });
    await resolve(ticket.value!.ticket_id);
  } catch {
    // 错误信息 request 层已 toast
  } finally {
    submitting.value = false;
  }
}

function reset() {
  ticket.value = null;
  selectedTaskId.value = "";
  selectedEmployeeId.value = "";
  goodQty.value = null;
  defectQty.value = null;
  dispatchQty.value = null;
  remark.value = "";
  manualCode.value = "";
}
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 32rpx;
  box-sizing: border-box;
}
.scan-area {
  padding: 80rpx 0;
}
.scan-btn {
  background: #2b6cb0;
  color: #fff;
  font-size: 36rpx;
  border-radius: 16rpx;
  padding: 24rpx 0;
}
.manual {
  display: flex;
  gap: 16rpx;
  margin-top: 32rpx;
  align-items: center;
}
.manual input {
  flex: 1;
  background: #fff;
  border-radius: 12rpx;
  padding: 16rpx 24rpx;
}
.hint {
  color: #718096;
  font-size: 24rpx;
  line-height: 1.6;
  margin-top: 20rpx;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 28rpx 32rpx;
  margin-bottom: 24rpx;
}
.card-title {
  font-size: 28rpx;
  font-weight: 600;
  color: #2d3748;
  display: block;
  margin-bottom: 16rpx;
}
.row {
  display: flex;
  padding: 10rpx 0;
  font-size: 28rpx;
}
.between {
  justify-content: space-between;
  border-bottom: 1px solid #edf2f7;
  padding-bottom: 16rpx;
  margin-bottom: 8rpx;
}
.wo {
  font-size: 32rpx;
  font-weight: 700;
  color: #2b6cb0;
}
.mode {
  font-size: 24rpx;
  color: #718096;
  border: 1px solid #cbd5e0;
  border-radius: 8rpx;
  padding: 2rpx 12rpx;
}
.label {
  color: #718096;
  width: 160rpx;
}
.value {
  color: #2d3748;
  flex: 1;
}
.tabs {
  display: flex;
  gap: 12rpx;
  background: #f2f4f7;
  border-radius: 12rpx;
  padding: 6rpx;
  margin-bottom: 24rpx;
}
.tab {
  flex: 1;
  text-align: center;
  padding: 14rpx 0;
  border-radius: 10rpx;
  color: #475467;
  font-size: 28rpx;
}
.tab.active {
  background: #fff;
  color: #2b6cb0;
  font-weight: 600;
}
.process {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 22rpx 24rpx;
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  margin-bottom: 16rpx;
  font-size: 28rpx;
  color: #2d3748;
}
.process.active {
  border-color: #2b6cb0;
  background: #ebf4ff;
}
.process-main {
  display: flex;
  flex-direction: column;
}
.process-sub {
  color: #718096;
  font-size: 22rpx;
  margin-top: 4rpx;
}
.check {
  color: #2b6cb0;
  font-weight: 700;
}
.qty-row {
  display: flex;
  align-items: center;
  margin-bottom: 20rpx;
}
.qty-row input {
  flex: 1;
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 14rpx 24rpx;
  text-align: right;
}
.picker {
  flex: 1;
}
.picker-value {
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 14rpx 24rpx;
  color: #2d3748;
}
.submit {
  background: #2b6cb0;
  color: #fff;
  border-radius: 12rpx;
  margin-top: 12rpx;
}
.submit[disabled] {
  opacity: 0.6;
}
.reset {
  background: #fff;
  color: #718096;
  border-radius: 12rpx;
  margin-top: 16rpx;
  font-size: 28rpx;
}
.reset.small {
  margin-top: 20rpx;
  font-size: 24rpx;
  padding: 8rpx 0;
}
</style>
