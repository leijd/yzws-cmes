<template>
  <view class="page">
    <view class="profile">
      <view class="avatar">{{ avatarText }}</view>
      <text class="name">{{ auth.user?.employee_name || auth.user?.login_name || "—" }}</text>
      <text class="account">账号：{{ auth.user?.login_name || "—" }}</text>
    </view>

    <view class="list">
      <view class="row">
        <text class="label">所属租户</text>
        <text class="value">{{ auth.user?.tenant_id || "—" }}</text>
      </view>
      <view class="row">
        <text class="label">服务端</text>
        <text class="value">{{ serverDisplay }}</text>
      </view>
      <view class="row">
        <text class="label">版本</text>
        <text class="value">1.0.0</text>
      </view>
    </view>

    <button class="logout" @click="confirmLogout">退出登录</button>
  </view>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { API_BASE_URL } from "../../config";
import { auth } from "../../store/auth";

const serverDisplay = API_BASE_URL.replace(/^https?:\/\//, "");
const avatarText = computed(
  () => (auth.user?.employee_name || auth.user?.login_name || "?").slice(0, 1)
);

onShow(() => {
  if (auth.hasSession && !auth.user) auth.fetchMe();
});

function confirmLogout() {
  uni.showModal({
    title: "退出登录",
    content: "确定要退出当前账号吗？",
    success: (res) => {
      if (res.confirm) auth.logout();
    },
  });
}
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 32rpx;
  box-sizing: border-box;
}
.profile {
  background: #fff;
  border-radius: 20rpx;
  padding: 48rpx 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  margin-bottom: 32rpx;
}
.avatar {
  width: 120rpx;
  height: 120rpx;
  border-radius: 50%;
  background: #2b6cb0;
  color: #fff;
  font-size: 48rpx;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 20rpx;
}
.name {
  font-size: 34rpx;
  font-weight: 600;
  color: #2d3748;
}
.account {
  font-size: 24rpx;
  color: #718096;
  margin-top: 8rpx;
}
.list {
  background: #fff;
  border-radius: 20rpx;
  padding: 8rpx 32rpx;
  margin-bottom: 48rpx;
}
.row {
  display: flex;
  justify-content: space-between;
  padding: 28rpx 0;
  border-bottom: 1px solid #edf2f7;
}
.row:last-child {
  border-bottom: none;
}
.label {
  color: #718096;
  font-size: 28rpx;
}
.value {
  color: #2d3748;
  font-size: 28rpx;
}
.logout {
  background: #fff;
  color: #e53e3e;
  border-radius: 16rpx;
  font-size: 30rpx;
}
</style>
