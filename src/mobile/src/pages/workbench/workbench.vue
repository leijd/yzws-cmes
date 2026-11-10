<template>
  <view class="page">
    <view class="hero">
      <text class="hello">{{ greeting }}，{{ displayName }}</text>
      <text class="sub">今天也是产能满满的一天</text>
    </view>

    <view v-if="visibleEntries.length" class="grid">
      <view
        v-for="item in visibleEntries"
        :key="item.name"
        class="cell"
        @click="open(item)"
      >
        <text class="cell-icon">{{ item.icon }}</text>
        <text class="cell-name">{{ item.name }}</text>
      </view>
    </view>
    <view v-else class="empty">
      <text>当前账号没有可用的移动端功能</text>
      <text class="empty-sub">请联系管理员分配角色权限</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onMounted } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { auth } from "../../store/auth";

const displayName = computed(
  () => auth.user?.employee_name || auth.user?.login_name || ""
);
const greeting = computed(() => {
  const h = new Date().getHours();
  if (h < 6) return "夜深了";
  if (h < 12) return "早上好";
  if (h < 14) return "中午好";
  if (h < 18) return "下午好";
  return "晚上好";
});

interface Entry {
  icon: string;
  name: string;
  url: string;
  /** 为空表示所有登录用户可见 */
  perm?: string;
  /** 满足任一权限即可见 */
  permAny?: string[];
}

const entries: Entry[] = [
  { icon: "📷", name: "扫码派工/报工", url: "/pages/report/scan", permAny: ["report:create", "task:assign"] },
  { icon: "📝", name: "我的报工", url: "/pages/report/mine", perm: "report:read" },
  { icon: "✅", name: "报工审批", url: "/pages/report/review", perm: "review:approve" },
  { icon: "📊", name: "产量查询", url: "/pages/stats/output", perm: "statistics:read" },
  { icon: "💰", name: "我的计件", url: "/pages/stats/piecework", perm: "payroll:read" },
  { icon: "📋", name: "工资单", url: "/pages/payroll/list", perm: "payroll:read" },
  { icon: "🏭", name: "项目进度", url: "/pages/progress/styles", perm: "progress:read" },
  { icon: "📢", name: "生产通知", url: "/pages/notify/list" },
];

const visibleEntries = computed(() =>
  entries.filter((e) => {
    if (e.permAny) return e.permAny.some((perm) => auth.hasPerm(perm));
    return !e.perm || auth.hasPerm(e.perm);
  })
);

function open(item: Entry) {
  uni.navigateTo({ url: item.url });
}

onMounted(() => {
  if (auth.hasSession) {
    if (!auth.user) auth.fetchMe();
    if (!Object.keys(auth.permissions).length) auth.fetchPermissions();
  }
});

onShow(() => {
  if (auth.hasSession && !Object.keys(auth.permissions).length) {
    auth.fetchPermissions();
  }
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 32rpx;
  box-sizing: border-box;
}
.hero {
  background: linear-gradient(135deg, #2b6cb0, #2c5282);
  border-radius: 20rpx;
  padding: 40rpx 32rpx;
  margin-bottom: 32rpx;
}
.hello {
  display: block;
  color: #fff;
  font-size: 36rpx;
  font-weight: 600;
}
.sub {
  display: block;
  color: rgba(255, 255, 255, 0.7);
  font-size: 24rpx;
  margin-top: 8rpx;
}
.grid {
  display: flex;
  flex-wrap: wrap;
  gap: 24rpx;
}
.cell {
  width: calc((100% - 24rpx) / 2);
  background: #fff;
  border-radius: 16rpx;
  padding: 48rpx 0;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.cell-icon {
  font-size: 56rpx;
  margin-bottom: 16rpx;
}
.cell-name {
  font-size: 28rpx;
  color: #2d3748;
}
.empty {
  background: #fff;
  border-radius: 16rpx;
  padding: 80rpx 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  color: #718096;
  font-size: 28rpx;
}
.empty-sub {
  font-size: 24rpx;
  margin-top: 12rpx;
  color: #a0aec0;
}
</style>
