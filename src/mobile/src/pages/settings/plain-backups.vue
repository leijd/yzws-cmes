<template>
  <view class="page">
    <view class="card">
      <view class="row between">
        <view class="col grow">
          <text class="title">明文备份导出</text>
          <text class="muted">将本厂数据（含解密后的敏感列）导出为 zip 包，需 backup:export 权限。下载凭证一次性、10 分钟有效。</text>
        </view>
      </view>
      <view class="field" style="margin-top: 20rpx">
        <text class="field-label required">导出原因</text>
        <input class="input" v-model.trim="reason" placeholder="如 数据归档 / 审计要求" />
      </view>
      <button class="btn btn-solid btn-block" :disabled="saving" @click="createExport">{{ saving ? "导出中…" : "发起明文导出" }}</button>
    </view>

    <view v-if="items.length" class="section-title" style="margin-top: 32rpx">导出任务</view>
    <view v-for="it in items" :key="it.id" class="card">
      <view class="row between">
        <view class="grow">
          <text class="title" style="font-size: 28rpx">导出任务 {{ it.id.slice(0, 8) }}…</text>
          <text class="muted">{{ fmtDateTime(it.started_at) }} · {{ it.table_count }} 张表 · {{ it.total_rows }} 行</text>
          <text class="muted">SHA-256：{{ it.manifest_sha256 ? it.manifest_sha256.slice(0, 16) + "…" : "--" }}</text>
        </view>
        <view class="col" style="align-items: flex-end">
          <text class="tag" :class="it.status === 'succeeded' ? 'tag-green' : it.status === 'failed' ? 'tag-red' : 'tag-orange'">
            {{ it.status === "succeeded" ? "成功" : it.status === "failed" ? "失败" : "进行中" }}
          </text>
          <text v-if="it.downloaded" class="muted" style="margin-top: 8rpx">已下载</text>
        </view>
      </view>
      <view class="actions" v-if="it.status === 'succeeded'">
        <button class="btn btn-outline btn-sm" @click="verify(it)">校验</button>
        <button v-if="!it.downloaded" class="btn btn-solid btn-sm" @click="download(it)">下载 zip</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { API_BASE_URL } from "../../config";
import { fmtDateTime } from "../../utils/format";

interface PlainBackup {
  id: string; status: string; manifest_sha256?: string; total_rows: number; table_count: number;
  snapshot_at?: number; started_at?: number; finished_at?: number; error?: string; downloaded: boolean;
}

const items = ref<PlainBackup[]>([]);
const saving = ref(false);
const reason = ref("");

async function load() {
  try {
    const res = await request<{ items: PlainBackup[] }>("/tenant/plain-backups", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  }
}

async function createExport() {
  if (!reason.value) {
    uni.showToast({ title: "请填写导出原因", icon: "none" });
    return;
  }
  saving.value = true;
  try {
    await request("/tenant/plain-backups", {
      method: "POST",
      data: { reason: reason.value },
      idempotency: true,
    });
    uni.showToast({ title: "导出完成", icon: "none" });
    reason.value = "";
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "导出失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

async function verify(it: PlainBackup) {
  try {
    await request(`/tenant/plain-backups/${it.id}/verify`, { method: "POST", data: {} });
    uni.showToast({ title: "校验通过", icon: "none" });
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "校验失败", icon: "none" });
  }
}

async function download(it: PlainBackup) {
  try {
    const res = await request<{ token: string }>(`/tenant/plain-backups/${it.id}/download-ticket`, {
      method: "POST",
      data: {},
    });
    const url = `${API_BASE_URL}/api/v1/tenant/plain-backups/${it.id}/download?token=${encodeURIComponent(res.token)}`;
    try {
      (window as unknown as { open: (u: string) => void }).open(url);
      uni.showToast({ title: "下载已开始（凭证 10 分钟有效）", icon: "none" });
    } catch {
      uni.setClipboardData({ data: url });
      uni.showToast({ title: "下载链接已复制，请在浏览器打开（10 分钟有效）", icon: "none" });
    }
    setTimeout(load, 1500);
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "获取下载凭证失败", icon: "none" });
  }
}

onLoad(load);
</script>
