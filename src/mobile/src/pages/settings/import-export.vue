<template>
  <BrandNavBar title="导入导出" />
  <view class="page">
    <view class="chips">
      <view class="chip" :class="{ on: tab === 'export' }" @click="tab = 'export'; load()">导出</view>
      <view class="chip" :class="{ on: tab === 'import' }" @click="tab = 'import'; load()">导入</view>
    </view>

    <!-- 导出 -->
    <template v-if="tab === 'export'">
      <view class="card">
        <view class="section-title" style="margin-top: 0">新建导出</view>
        <view class="chips">
          <view class="chip" :class="{ on: exportType === 'employees' }" @click="exportType = 'employees'">员工清单</view>
          <view class="chip" :class="{ on: exportType === 'wages' }" @click="exportType = 'wages'">工资清单</view>
        </view>
        <view v-if="exportType === 'wages'" class="field">
          <text class="field-label">工资期间（YYYY-MM，默认当月）</text>
          <input class="input" v-model.trim="exportPeriod" placeholder="如 2026-11" />
        </view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="createExport">{{ saving ? "创建中…" : "创建导出（CSV）" }}</button>
      </view>

      <view v-if="exportJobs.length" class="section-title" style="margin-top: 32rpx">导出任务</view>
      <view v-for="j in exportJobs" :key="j.id" class="card">
        <view class="row between">
          <view class="grow">
            <text class="title" style="font-size: 28rpx">{{ j.job_type === "employees" ? "员工清单" : "工资清单" }}</text>
            <text class="muted">{{ fmtDateTime(j.created_at) }} · 进度 {{ j.progress }}%</text>
          </view>
          <text class="tag" :class="j.status === 'succeeded' ? 'tag-green' : j.status === 'failed' ? 'tag-red' : 'tag-orange'">{{ j.status === "succeeded" ? "成功" : j.status === "failed" ? "失败" : "处理中" }}</text>
        </view>
        <view class="actions" v-if="j.status === 'succeeded'">
          <button class="btn btn-outline btn-sm" @click="downloadExport(j)">下载 CSV</button>
        </view>
      </view>
    </template>

    <!-- 导入 -->
    <template v-else>
      <view class="card">
        <view class="section-title" style="margin-top: 0">员工导入（CSV）</view>
        <text class="muted" style="display: block; margin-bottom: 16rpx">模板列：工号、姓名、岗位、车间、产线、状态。先在网页版下载模板或按此格式准备 CSV。</text>
        <view class="actions" style="margin-top: 0">
          <button class="btn btn-outline btn-sm" @click="chooseFile">选择 CSV 文件</button>
        </view>
        <text v-if="importFileName" class="muted" style="display: block; margin-top: 12rpx">已选择：{{ importFileName }}</text>
        <button v-if="sourceAttachmentId" class="btn btn-solid btn-block" :disabled="saving" @click="createImport">{{ saving ? "校验中…" : "开始导入校验" }}</button>
      </view>

      <view v-if="importJobs.length" class="section-title" style="margin-top: 32rpx">导入任务</view>
      <view v-for="j in importJobs" :key="j.id" class="card">
        <view class="row between">
          <view class="grow">
            <text class="title" style="font-size: 28rpx">导入任务 {{ j.id.slice(0, 8) }}…</text>
            <text class="muted">共 {{ j.total_rows }} 行 · 成功 {{ j.success_rows }} · 错误 {{ j.error_rows }}</text>
          </view>
          <text class="tag" :class="j.status === 'committed' ? 'tag-green' : j.status === 'failed' ? 'tag-red' : 'tag-orange'">
            {{ j.status === "committed" ? "已提交" : j.status === "failed" ? "失败" : "已校验" }}
          </text>
        </view>
        <view class="actions">
          <button v-if="j.error_rows > 0" class="btn btn-outline btn-sm" @click="viewErrors(j)">错误明细</button>
          <button v-if="j.status === 'validated'" class="btn btn-solid btn-sm" @click="commitImport(j)">提交导入</button>
        </view>
      </view>

      <!-- 错误明细 -->
      <view v-if="errorSheet" class="sheet-mask" @click="errorSheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">导入错误明细</view>
          <view v-if="!importErrors.length" class="empty">无错误</view>
          <view v-for="e in importErrors" :key="e.row_no + e.message" class="list-row">
            <view class="grow">
              <text class="title" style="font-size: 26rpx; color: #b91c1c">第 {{ e.row_no }} 行：{{ e.message }}</text>
              <text class="muted">{{ e.raw_value }}</text>
            </view>
          </view>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request, getToken } from "../../utils/request";
import { API_BASE_URL } from "../../config";
import { fmtDateTime } from "../../utils/format";

interface ExportJob { id: string; job_type: string; status: string; progress: number; error?: string; created_at: number }
interface ImportJob { id: string; job_type: string; status: string; total_rows: number; success_rows: number; error_rows: number; created_at: number }
interface ImportError { row_no: number; field?: string; error_code: string; message: string; raw_value?: string }

const tab = ref<"export" | "import">("export");
const saving = ref(false);
const exportType = ref<"employees" | "wages">("employees");
const exportPeriod = ref("");
const exportJobs = ref<ExportJob[]>([]);
const importJobs = ref<ImportJob[]>([]);
const importFileName = ref("");
const sourceAttachmentId = ref("");
const errorSheet = ref(false);
const importErrors = ref<ImportError[]>([]);

async function load() {
  try {
    if (tab.value === "export") {
      const res = await request<{ items: ExportJob[] }>("/export-jobs", { silent: true });
      exportJobs.value = res.items || [];
    } else {
      const res = await request<{ items: ImportJob[] }>("/import-jobs", { silent: true });
      importJobs.value = res.items || [];
    }
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  }
}

async function createExport() {
  saving.value = true;
  try {
    const res = await request<{ id: string; rows: number }>("/export-jobs", {
      method: "POST",
      data: {
        job_type: exportType.value,
        filter_snapshot: exportType.value === "wages" && exportPeriod.value ? { period: exportPeriod.value } : {},
      },
      idempotency: true,
    });
    uni.showToast({ title: `导出成功（${res.rows} 行）`, icon: "none" });
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "导出失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

function downloadExport(j: ExportJob) {
  const url = `${API_BASE_URL}/api/v1/export-jobs/${j.id}/content`;
  // #ifdef H5
  /* H5：新窗口打开下载链接 */
  (window as unknown as { open: (u: string) => void }).open(url);
  // #endif
  // #ifndef H5
  /* App：没有浏览器下载入口，复制链接后到系统浏览器打开 */
  uni.setClipboardData({ data: url });
  uni.showToast({ title: "链接已复制，请在浏览器打开下载", icon: "none" });
  // #endif
}

function chooseFile() {
  // #ifdef H5
  uni.chooseFile({
    count: 1,
    extension: [".csv"],
    success: (res) => {
      const files = res.tempFiles as unknown as { path: string; name: string }[];
      const file = files[0];
      if (!file) return;
      importFileName.value = file.name;
      uploadCsv(file.path);
    },
    fail: () => uni.showToast({ title: "未选择文件", icon: "none" }),
  });
  // #endif
  // #ifndef H5
  /* App 端不支持 uni.chooseFile（无法唤起系统文件选择器），引导到网页版完成导入 */
  uni.showModal({
    title: "请在网页版操作",
    content: "App 端暂不支持选择 CSV 文件，员工导入请在电脑浏览器打开网页版完成。",
    showCancel: false,
  });
  // #endif
}

function uploadCsv(path: string) {
  uni.uploadFile({
    url: API_BASE_URL + "/api/v1/attachments/init",
    filePath: path,
    name: "file",
    header: { Authorization: "Bearer " + (getToken() || "") },
    success: (res) => {
      try {
        const data = JSON.parse(res.data) as { attachment_id?: string; id?: string };
        sourceAttachmentId.value = data.attachment_id || data.id || "";
        if (sourceAttachmentId.value) {
          uni.showToast({ title: "文件已上传", icon: "none" });
        } else {
          uni.showToast({ title: "上传失败", icon: "none" });
        }
      } catch {
        uni.showToast({ title: "上传失败", icon: "none" });
      }
    },
    fail: () => uni.showToast({ title: "上传失败", icon: "none" }),
  });
}

async function createImport() {
  if (!sourceAttachmentId.value) return;
  saving.value = true;
  try {
    await request("/import-jobs", {
      method: "POST",
      data: { job_type: "employees", source_attachment_id: sourceAttachmentId.value },
      idempotency: true,
    });
    uni.showToast({ title: "校验完成", icon: "none" });
    importFileName.value = "";
    sourceAttachmentId.value = "";
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "校验失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

async function viewErrors(j: ImportJob) {
  try {
    const res = await request<{ items: ImportError[] }>(`/import-jobs/${j.id}/errors`, { silent: true });
    importErrors.value = res.items || [];
    errorSheet.value = true;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  }
}

function commitImport(j: ImportJob) {
  uni.showModal({
    title: "提交导入",
    content: `将把 ${j.success_rows} 条有效员工数据写入本厂，确定提交吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/import-jobs/${j.id}/commit`, { method: "POST", data: {} });
        uni.showToast({ title: "导入已提交", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "提交失败", icon: "none" });
      }
    },
  });
}

onLoad(load);
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
</style>
