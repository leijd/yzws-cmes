<template>
  <view class="page">
    <view class="card">
      <view class="row between">
        <view class="col grow">
          <text class="title">备份与同步</text>
          <text class="muted">节点 {{ syncStatus?.node_id || "--" }} · 待同步事件 {{ syncStatus?.outbox_pending ?? 0 }} · 冲突 {{ syncStatus?.conflicts ?? 0 }}</text>
        </view>
      </view>
      <view class="actions" style="margin-top: 20rpx">
        <button class="btn btn-solid btn-sm" :disabled="saving" @click="createBackup('manifest')">创建清单备份</button>
        <button class="btn btn-outline btn-sm" :disabled="saving" @click="createBackup('database')">创建数据库备份</button>
      </view>
    </view>

    <view v-if="jobs.length" class="section-title" style="margin-top: 32rpx">备份任务</view>
    <view v-for="j in jobs" :key="j.id" class="card">
      <view class="row between">
        <view class="grow">
          <text class="title" style="font-size: 28rpx">{{ j.backup_type === "database" ? "数据库备份" : "清单备份" }}</text>
          <text class="muted">{{ fmtDateTime(j.started_at) }} · {{ j.includes_attachments ? "含附件" : "不含附件" }}</text>
        </view>
        <text class="tag" :class="j.status === 'succeeded' || j.status === 'verified' ? 'tag-green' : j.status === 'failed' ? 'tag-red' : 'tag-orange'">
          {{ statusText(j.status) }}
        </text>
      </view>
      <view class="actions" v-if="j.status === 'succeeded'">
        <button class="btn btn-outline btn-sm" @click="verify(j)">校验备份</button>
      </view>
    </view>

    <view v-if="conflicts.length" class="section-title" style="margin-top: 32rpx">同步冲突（{{ conflicts.length }}）</view>
    <view v-for="c in conflicts" :key="c.id" class="card">
      <view class="row between">
        <view class="grow">
          <text class="title" style="font-size: 28rpx">{{ c.aggregate_type }} · {{ c.conflict_code }}</text>
          <text class="muted">本地值：{{ truncate(c.current_value) }}</text>
          <text class="muted">同步值：{{ truncate(c.incoming_value) }}</text>
        </view>
        <text class="tag" :class="c.status === 'resolved' ? 'tag-green' : 'tag-orange'">{{ c.status === "resolved" ? "已处理" : "待处理" }}</text>
      </view>
      <view class="actions" v-if="c.status !== 'resolved'">
        <button class="btn btn-outline btn-sm" @click="resolve(c, 'keep_current')">保留本地</button>
        <button class="btn btn-outline btn-sm" @click="resolve(c, 'keep_incoming')">采用同步值</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { fmtDateTime } from "../../utils/format";

interface BackupJob {
  id: string; node_id: string; backup_type: string; status: string;
  includes_attachments: boolean; manifest_sha256?: string; error?: string;
  started_at: number; finished_at: number;
}
interface SyncStatus { node_id: string; outbox_pending: number; conflicts: number; online: boolean }
interface Conflict {
  id: string; aggregate_type: string; aggregate_id: string; conflict_code: string;
  incoming_value?: string; current_value?: string; status: string;
}

const jobs = ref<BackupJob[]>([]);
const conflicts = ref<Conflict[]>([]);
const syncStatus = ref<SyncStatus | null>(null);
const saving = ref(false);

function statusText(s: string): string {
  const map: Record<string, string> = { succeeded: "成功", verified: "已校验", failed: "失败", running: "进行中" };
  return map[s] || s || "--";
}
function truncate(v?: string): string {
  if (!v) return "--";
  return v.length > 80 ? v.slice(0, 80) + "…" : v;
}

async function load() {
  try {
    const [j, s, c] = await Promise.all([
      request<{ items: BackupJob[] }>("/backup-jobs", { silent: true }),
      request<SyncStatus>("/sync/status", { silent: true }),
      request<{ items: Conflict[] }>("/sync/conflicts", { silent: true }),
    ]);
    jobs.value = j.items || [];
    syncStatus.value = s;
    conflicts.value = c.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  }
}

async function createBackup(type: "manifest" | "database") {
  saving.value = true;
  try {
    await request("/backup-jobs", {
      method: "POST",
      data: { backup_type: type, include_attachments: false },
      idempotency: true,
    });
    uni.showToast({ title: "备份已创建", icon: "none" });
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "备份失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

async function verify(j: BackupJob) {
  try {
    const res = await request<{ status: string; match: boolean }>(`/backup-jobs/${j.id}/verify`, { method: "POST", data: {} });
    uni.showToast({ title: res.match ? "校验通过" : "校验失败", icon: "none" });
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "校验失败", icon: "none" });
  }
}

function resolve(c: Conflict, resolutionType: "keep_current" | "keep_incoming") {
  uni.showModal({
    title: "处理冲突",
    editable: true,
    placeholderText: "请填写处理原因",
    content: "",
    success: async (res) => {
      if (!res.confirm) return;
      const reason = res.content || "";
      if (!reason) {
        uni.showToast({ title: "请填写处理原因", icon: "none" });
        return;
      }
      try {
        await request(`/sync/conflicts/${c.id}/resolve`, {
          method: "POST",
          data: { resolution_type: resolutionType, reason },
        });
        uni.showToast({ title: "冲突已处理", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "处理失败", icon: "none" });
      }
    },
  });
}

onLoad(load);
</script>
