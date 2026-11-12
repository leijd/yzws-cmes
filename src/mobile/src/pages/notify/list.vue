<template>
  <view class="page">
    <view class="chips">
      <view class="chip" :class="{ on: tab === 'notifications' }" @click="tab = 'notifications'">消息待办</view>
      <view class="chip" :class="{ on: tab === 'sessions' }" @click="tab = 'sessions'; loadSessions()">登录会话</view>
    </view>

    <!-- 消息待办 -->
    <template v-if="tab === 'notifications'">
      <view class="bar">
        <text class="unread">未读 {{ unread }}</text>
        <button v-if="unread > 0" size="mini" @click="readAll">全部已读</button>
      </view>

      <view v-if="!loading && !items.length" class="empty">暂无通知</view>

      <view
        v-for="item in items"
        :key="item.id"
        class="card"
        :class="{ unread: !item.read }"
        @click="open(item)"
      >
        <view class="row between">
          <text class="title">{{ item.title }}</text>
          <text class="time">{{ formatDate(item.created_at) }}</text>
        </view>
        <text class="body">{{ item.body }}</text>
      </view>

      <view v-if="hasMore" class="more">
        <button size="mini" :loading="loadingMore" @click="loadMore">加载更多</button>
      </view>
    </template>

    <!-- 登录会话 -->
    <template v-else>
      <view v-if="sessionsLoading" class="empty">加载中…</view>
      <view v-else-if="!sessions.length" class="empty">暂无登录会话</view>
      <view v-for="s in sessions" :key="s.id" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">{{ s.device_info || s.node_id }}</text>
              <text v-if="s.current" class="tag">本机</text>
            </view>
            <text class="muted">登录于 {{ formatDate(s.created_at) }} · 到期 {{ formatDate(s.expires_at) }}</text>
          </view>
        </view>
        <view class="actions" v-if="!s.revoked_at">
          <button class="btn-danger session-btn" @click="revoke(s)">{{ s.current ? "退出本机登录" : "踢下线" }}</button>
        </view>
        <text v-else class="muted" style="margin-top: 12rpx">已于 {{ formatDate(s.revoked_at) }} 撤销</text>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { formatDate } from "../../utils/format";

interface NotifyItem {
  id: string;
  type: string;
  title: string;
  body: string;
  read: boolean;
  created_at: number;
}

interface SessionItem {
  id: string;
  node_id: string;
  device_info?: string;
  created_at: number;
  expires_at: number;
  revoked_at?: number;
  current: boolean;
}

const tab = ref<"notifications" | "sessions">("notifications");
const items = ref<NotifyItem[]>([]);
const unread = ref(0);
const hasMore = ref(false);
const nextCursor = ref("");
const loading = ref(false);
const loadingMore = ref(false);

const sessions = ref<SessionItem[]>([]);
const sessionsLoading = ref(false);

async function load() {
  loading.value = true;
  try {
    const res = await request<{
      items: NotifyItem[];
      unread_count: number;
      has_more: boolean;
      next_cursor?: string;
    }>("/notifications", { silent: true });
    items.value = res.items || [];
    unread.value = res.unread_count || 0;
    hasMore.value = res.has_more;
    nextCursor.value = res.next_cursor || "";
  } catch {
    items.value = [];
  } finally {
    loading.value = false;
  }
}

async function loadMore() {
  if (!nextCursor.value) return;
  loadingMore.value = true;
  try {
    const res = await request<{
      items: NotifyItem[];
      unread_count: number;
      has_more: boolean;
      next_cursor?: string;
    }>("/notifications", {
      data: { cursor: nextCursor.value },
      silent: true,
    });
    items.value.push(...(res.items || []));
    unread.value = res.unread_count || 0;
    hasMore.value = res.has_more;
    nextCursor.value = res.next_cursor || "";
  } finally {
    loadingMore.value = false;
  }
}

async function open(item: NotifyItem) {
  if (!item.read) {
    try {
      await request("/notifications/read", {
        method: "POST",
        data: { ids: [item.id] },
        silent: true,
      });
      item.read = true;
      unread.value = Math.max(0, unread.value - 1);
    } catch {
      // 标记失败不阻塞查看
    }
  }
  uni.showModal({
    title: item.title,
    content: item.body,
    showCancel: false,
  });
}

async function readAll() {
  try {
    await request("/notifications/read", {
      method: "POST",
      data: { all_before: Date.now() * 1000 },
      silent: true,
    });
    items.value.forEach((i) => (i.read = true));
    unread.value = 0;
  } catch {
    // 错误已 toast
  }
}

/* ===== 登录会话 ===== */

async function loadSessions() {
  sessionsLoading.value = true;
  try {
    const res = await request<{ items: SessionItem[] }>("/sessions", { silent: true });
    sessions.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    sessionsLoading.value = false;
  }
}

function revoke(s: SessionItem) {
  const content = s.current
    ? "撤销本机会话后将立即退出登录，确定撤销吗？"
    : `确定撤销会话「${s.device_info || s.node_id}」吗？对方将立即下线。`;
  uni.showModal({
    title: "撤销会话",
    content,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/sessions/${s.id}`, { method: "DELETE" });
        uni.showToast({ title: "已撤销", icon: "none" });
        if (s.current) return; // 撤销本机会话后请求层 401 会自动跳登录
        loadSessions();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "撤销失败", icon: "none" });
      }
    },
  });
}

onShow(() => {
  load();
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 24rpx 32rpx;
  box-sizing: border-box;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 16rpx;
  margin-bottom: 20rpx;
}
.chip {
  padding: 12rpx 28rpx;
  border-radius: 999rpx;
  font-size: 24rpx;
  color: #475569;
  background: #fff;
  border: 1px solid #e2e8f0;
}
.chip.on {
  background: #2563eb;
  color: #fff;
  border-color: #2563eb;
}
.bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20rpx;
}
.unread {
  font-size: 26rpx;
  color: #d69e2e;
}
.empty {
  text-align: center;
  color: #a0aec0;
  font-size: 28rpx;
  padding: 120rpx 0;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 24rpx 32rpx;
  margin-bottom: 20rpx;
  opacity: 0.65;
}
.card.unread {
  opacity: 1;
}
.row {
  display: flex;
}
.between {
  justify-content: space-between;
  margin-bottom: 10rpx;
}
.col {
  display: flex;
  flex-direction: column;
}
.grow {
  flex: 1;
}
.gap8 {
  gap: 12rpx;
}
.title {
  font-size: 30rpx;
  font-weight: 600;
  color: #2d3748;
}
.time {
  font-size: 24rpx;
  color: #a0aec0;
}
.body {
  font-size: 26rpx;
  color: #718096;
}
.muted {
  font-size: 22rpx;
  color: #94a3b8;
  display: block;
  margin-top: 6rpx;
}
.tag {
  font-size: 20rpx;
  padding: 2rpx 14rpx;
  border-radius: 999rpx;
  background: #dcfce7;
  color: #15803d;
}
.actions {
  margin-top: 16rpx;
}
.session-btn {
  border-radius: 10rpx;
  font-size: 24rpx;
  padding: 0 28rpx;
  height: 68rpx;
  line-height: 68rpx;
  background: #fee2e2;
  color: #b91c1c;
  border: 1px solid #fecaca;
}
.more {
  display: flex;
  justify-content: center;
  padding: 16rpx 0 32rpx;
}
</style>
