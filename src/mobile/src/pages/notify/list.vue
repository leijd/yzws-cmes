<template>
  <view class="page">
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
      <button size="mini" :loading="loadingMore" @click="loadMore">
        加载更多
      </button>
    </view>
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

const items = ref<NotifyItem[]>([]);
const unread = ref(0);
const hasMore = ref(false);
const nextCursor = ref("");
const loading = ref(false);
const loadingMore = ref(false);

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
.more {
  display: flex;
  justify-content: center;
  padding: 16rpx 0 32rpx;
}
</style>
