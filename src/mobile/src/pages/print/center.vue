<template>
  <BrandNavBar title="菲票打印" />
  <view class="page">
    <view class="chips">
      <view class="chip" :class="{ on: tab === 'jobs' }" @click="tab = 'jobs'">打印任务</view>
      <view class="chip" :class="{ on: tab === 'printers' }" @click="tab = 'printers'; loadPrinters()">打印机</view>
      <view class="chip" :class="{ on: tab === 'templates' }" @click="tab = 'templates'; loadTemplates()">模板</view>
    </view>

    <!-- 打印任务 -->
    <template v-if="tab === 'jobs'">
      <view class="card">
        <view class="section-title" style="margin-top: 0">新建打印任务</view>
        <view class="field">
          <text class="field-label required">打印机</text>
          <picker :range="printerNames" :value="printerIndex" @change="printerIndex = $event.detail.value">
            <view class="picker-box">{{ printerNames[printerIndex] || "选择打印机" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">模板</text>
          <picker :range="templateNames" :value="templateIndex" @change="templateIndex = $event.detail.value">
            <view class="picker-box">{{ templateNames[templateIndex] || "选择模板" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">菲票 ID</text>
          <input class="input" v-model.trim="ticketId" placeholder="扫描或输入菲票 ID" />
        </view>
        <view class="field">
          <text class="field-label">份数（1-50）</text>
          <input class="input" v-model="copies" type="number" placeholder="1" />
        </view>
        <view v-if="jobError" class="error-text">{{ jobError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="createJob">{{ saving ? "创建中…" : "创建打印任务" }}</button>
      </view>

      <view v-if="jobs.length" class="section-title">本次会话创建的任务</view>
      <view v-for="j in jobs" :key="j.id" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">任务 {{ j.id.slice(0, 8) }}…</text>
              <text class="tag" :class="jobStatusCls(j.status)">{{ jobStatusText(j.status) }}</text>
            </view>
            <text class="muted">份数 {{ j.copies }} · 重试 {{ j.retry_count }}</text>
          </view>
        </view>
        <view class="actions">
          <button v-if="j.status === 'queued' || j.status === 'sent'" class="btn btn-solid btn-sm" @click="receipt(j, 'succeeded')">打印成功</button>
          <button v-if="j.status === 'queued' || j.status === 'sent'" class="btn btn-danger btn-sm" @click="receipt(j, 'failed')">打印失败</button>
          <button class="btn btn-ghost btn-sm" @click="refreshJob(j)">刷新状态</button>
          <button v-if="j.status === 'succeeded'" class="btn btn-outline btn-sm" @click="openReprint(j)">补打</button>
        </view>
      </view>
    </template>

    <!-- 打印机 -->
    <template v-else-if="tab === 'printers'">
      <view class="row between" style="margin-bottom: 16rpx">
        <text class="section-title" style="margin: 0">打印机</text>
        <button class="btn btn-solid btn-sm" @click="printerSheet = true">＋ 登记打印机</button>
      </view>
      <view v-if="!printers.length" class="empty">暂无打印机，点击右上角登记</view>
      <view v-for="p in printers" :key="p.id" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">{{ p.name }}</text>
              <text class="tag" :class="p.status === 'active' ? 'tag-green' : 'tag-gray'">{{ p.status === "active" ? "可用" : "停用" }}</text>
            </view>
            <text class="muted">{{ p.model || "未知型号" }} · {{ connectionText(p.connection_type) }}</text>
          </view>
        </view>
      </view>

      <view v-if="printerSheet" class="sheet-mask" @click="printerSheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">登记打印机</view>
          <view class="field">
            <text class="field-label required">名称</text>
            <input class="input" v-model.trim="printerForm.name" placeholder="如 办公室热敏打印机" />
          </view>
          <view class="field">
            <text class="field-label">型号</text>
            <input class="input" v-model.trim="printerForm.model" placeholder="可选" />
          </view>
          <view class="field">
            <text class="field-label required">连接方式</text>
            <picker :range="connectionLabels" :value="connectionIndex" @change="connectionIndex = $event.detail.value">
              <view class="picker-box">{{ connectionLabels[connectionIndex] }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label">地址</text>
            <input class="input" v-model.trim="printerForm.address" placeholder="USB 端口 / IP / 蓝牙地址（可选）" />
          </view>
          <view v-if="formError" class="error-text">{{ formError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="savePrinter">{{ saving ? "保存中…" : "保存" }}</button>
        </view>
      </view>
    </template>

    <!-- 模板 -->
    <template v-else>
      <view class="section-title" style="margin-top: 0">打印模板</view>
      <view v-if="!templates.length" class="empty">暂无模板</view>
      <view v-for="t in templates" :key="t.id" class="card">
        <view class="row between">
          <view class="col grow">
            <text class="title" style="font-size: 28rpx">{{ t.name }}</text>
            <text class="muted">类型 {{ t.template_type }} · 版本 v{{ t.version }}</text>
          </view>
          <text class="tag" :class="t.status === 'active' ? 'tag-green' : 'tag-gray'">{{ t.status === "active" ? "可用" : "停用" }}</text>
        </view>
      </view>
    </template>

    <!-- 补打 -->
    <view v-if="reprintSheet" class="sheet-mask" @click="reprintSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">补打菲票</view>
        <view class="field">
          <text class="field-label required">补打原因</text>
          <input class="input" v-model.trim="reprintReason" placeholder="如 打印机卡纸" />
        </view>
        <view class="field">
          <text class="field-label">份数</text>
          <input class="input" v-model="reprintCopies" type="number" placeholder="1" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="reprint">{{ saving ? "创建中…" : "创建补打任务" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Printer { id: string; name: string; model?: string; connection_type: string; status: string }
interface Template { id: string; name: string; template_type: string; version: number; status: string }
interface PrintJob {
  id: string; status: string; copies: number; retry_count: number;
  error?: string; original_job_id?: string;
}

const tab = ref<"jobs" | "printers" | "templates">("jobs");
const saving = ref(false);
const formError = ref("");
const jobError = ref("");

const printers = ref<Printer[]>([]);
const templates = ref<Template[]>([]);
const jobs = ref<PrintJob[]>([]);

const printerIndex = ref(0);
const templateIndex = ref(0);
const ticketId = ref("");
const copies = ref("1");

const printerNames = computed(() => printers.value.map((p) => p.name));
const templateNames = computed(() => templates.value.map((t) => `${t.name} v${t.version}`));

const printerSheet = ref(false);
const connectionLabels = ["USB", "网络", "蓝牙", "云端", "系统默认"];
const connectionValues = ["usb", "network", "bluetooth", "cloud", "system"];
const connectionIndex = ref(0);
const printerForm = ref({ name: "", model: "", address: "" });

const reprintSheet = ref(false);
const reprintTarget = ref<PrintJob | null>(null);
const reprintReason = ref("");
const reprintCopies = ref("1");

function connectionText(v: string): string {
  return connectionLabels[connectionValues.indexOf(v)] || v;
}
function jobStatusText(s: string): string {
  const map: Record<string, string> = { queued: "排队中", sent: "已发送", succeeded: "成功", failed: "失败" };
  return map[s] || s || "--";
}
function jobStatusCls(s: string): string {
  return s === "succeeded" ? "tag-green" : s === "failed" ? "tag-red" : "tag-orange";
}

async function loadPrinters() {
  try {
    const res = await request<{ items: Printer[] }>("/printers", { silent: true });
    printers.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  }
}
async function loadTemplates() {
  try {
    const res = await request<{ items: Template[] }>("/print-templates", { silent: true });
    templates.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  }
}

async function createJob() {
  jobError.value = "";
  const printer = printers.value[printerIndex.value];
  const template = templates.value[templateIndex.value];
  if (!printer || !template) { jobError.value = "请选择打印机与模板"; return; }
  if (!ticketId.value) { jobError.value = "请填写菲票 ID"; return; }
  const n = Number(copies.value || 1);
  if (n < 1 || n > 50) { jobError.value = "份数须在 1-50 之间"; return; }
  saving.value = true;
  try {
    const res = await request<{ id: string; status: string }>("/print-jobs", {
      method: "POST",
      data: { printer_id: printer.id, template_id: template.id, ticket_id: ticketId.value, copies: n },
      idempotency: true,
    });
    uni.showToast({ title: "打印任务已创建", icon: "none" });
    ticketId.value = "";
    await refreshJob({ id: res.id, status: res.status, copies: n, retry_count: 0 });
  } catch (e) {
    jobError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

async function refreshJob(j: PrintJob) {
  try {
    const res = await request<PrintJob>(`/print-jobs/${j.id}`, { silent: true });
    const idx = jobs.value.findIndex((x) => x.id === j.id);
    if (idx >= 0) jobs.value[idx] = { ...jobs.value[idx], ...res };
    else jobs.value.unshift(res);
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "刷新失败", icon: "none" });
  }
}

async function receipt(j: PrintJob, result: "succeeded" | "failed") {
  try {
    await request(`/print-jobs/${j.id}/receipt`, {
      method: "POST",
      data: { result, error: result === "failed" ? "手动登记失败" : undefined },
    });
    uni.showToast({ title: "回执已登记", icon: "none" });
    refreshJob(j);
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "登记失败", icon: "none" });
  }
}

function openReprint(j: PrintJob) {
  reprintTarget.value = j;
  reprintReason.value = "";
  reprintCopies.value = "1";
  formError.value = "";
  reprintSheet.value = true;
}

async function reprint() {
  formError.value = "";
  const j = reprintTarget.value;
  if (!j) return;
  if (!reprintReason.value) { formError.value = "请填写补打原因"; return; }
  saving.value = true;
  try {
    await request(`/print-jobs/${j.id}/reprint`, {
      method: "POST",
      data: { reason: reprintReason.value, copies: Number(reprintCopies.value || 1) },
      idempotency: true,
    });
    uni.showToast({ title: "补打任务已创建", icon: "none" });
    reprintSheet.value = false;
  } catch (e) {
    formError.value = (e as Error).message || "补打失败";
  } finally {
    saving.value = false;
  }
}

async function savePrinter() {
  formError.value = "";
  if (!printerForm.value.name) { formError.value = "请填写打印机名称"; return; }
  saving.value = true;
  try {
    await request("/printers", {
      method: "POST",
      data: {
        name: printerForm.value.name,
        model: printerForm.value.model || undefined,
        connection_type: connectionValues[connectionIndex.value],
        address: printerForm.value.address || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "已登记", icon: "none" });
    printerSheet.value = false;
    printerForm.value = { name: "", model: "", address: "" };
    loadPrinters();
  } catch (e) {
    formError.value = (e as Error).message || "登记失败";
  } finally {
    saving.value = false;
  }
}

onLoad(() => {
  loadPrinters();
  loadTemplates();
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
