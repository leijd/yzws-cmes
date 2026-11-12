<template>
  <view class="page">
    <view class="profile">
      <view class="avatar">{{ avatarText }}</view>
      <text class="name">{{ auth.user?.employee_name || auth.user?.login_name || "—" }}</text>
      <text class="account">账号：{{ auth.user?.login_name || "—" }}</text>
    </view>

    <!-- 员工功能入口（与网页版一致） -->
    <view class="menu-card">
      <view class="menu-row" @click="go('/pages/my/wages')">
        <text class="menu-icon">💰</text>
        <text class="menu-label">我的工资</text>
        <text class="menu-arrow">›</text>
      </view>
      <view class="menu-row" @click="go('/pages/stats/piecework')">
        <text class="menu-icon">🧮</text>
        <text class="menu-label">我的计件</text>
        <text class="menu-arrow">›</text>
      </view>
      <view class="menu-row" @click="go('/pages/report/mine')">
        <text class="menu-icon">📝</text>
        <text class="menu-label">我的报工</text>
        <text class="menu-arrow">›</text>
      </view>
      <view class="menu-row" @click="go('/pages/my/profile')">
        <text class="menu-icon">👤</text>
        <text class="menu-label">我的资料</text>
        <text class="menu-arrow">›</text>
      </view>
      <view class="menu-row" @click="passwordSheet = true">
        <text class="menu-icon">🔒</text>
        <text class="menu-label">修改密码</text>
        <text class="menu-arrow">›</text>
      </view>
    </view>

    <view class="list">
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

    <!-- 修改密码 -->
    <view v-if="passwordSheet" class="sheet-mask" @click="passwordSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="font-size: 32rpx; font-weight: 700; color: #1e293b; margin-bottom: 24rpx">修改密码</view>
        <view style="padding: 16rpx 0">
          <text class="field-label">当前密码</text>
          <input class="input" v-model="oldPassword" password placeholder="当前密码" />
        </view>
        <view style="padding: 16rpx 0">
          <text class="field-label">新密码（至少 12 位）</text>
          <input class="input" v-model="newPassword" password placeholder="至少 12 位，不能与当前密码相同" />
        </view>
        <view style="padding: 16rpx 0">
          <text class="field-label">确认新密码</text>
          <input class="input" v-model="confirmPassword" password placeholder="再次输入新密码" />
        </view>
        <view v-if="passwordError" class="error-text">{{ passwordError }}</view>
        <button class="btn-solid change-btn" :disabled="saving" @click="changePassword">
          {{ saving ? "提交中…" : "确认修改" }}
        </button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { API_BASE_URL } from "../../config";
import { auth } from "../../store/auth";
import { request } from "../../utils/request";

const serverDisplay = API_BASE_URL.replace(/^https?:\/\//, "");
const avatarText = computed(
  () => (auth.user?.employee_name || auth.user?.login_name || "?").slice(0, 1)
);

const passwordSheet = ref(false);
const oldPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const passwordError = ref("");
const saving = ref(false);

onShow(() => {
  if (auth.hasSession && !auth.user) auth.fetchMe();
});

function go(url: string) {
  uni.navigateTo({ url });
}

async function changePassword() {
  passwordError.value = "";
  if (!oldPassword.value || !newPassword.value) {
    passwordError.value = "请填写当前密码与新密码";
    return;
  }
  if (newPassword.value.length < 12) {
    passwordError.value = "新密码至少 12 位";
    return;
  }
  if (newPassword.value === oldPassword.value) {
    passwordError.value = "新密码不能与当前密码相同";
    return;
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = "两次输入的新密码不一致";
    return;
  }
  saving.value = true;
  try {
    await request("/auth/password/change", {
      method: "POST",
      data: { old_password: oldPassword.value, new_password: newPassword.value },
    });
    uni.showToast({ title: "密码已修改", icon: "none" });
    passwordSheet.value = false;
    oldPassword.value = "";
    newPassword.value = "";
    confirmPassword.value = "";
  } catch (e) {
    passwordError.value = (e as Error).message || "修改失败";
  } finally {
    saving.value = false;
  }
}

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
  margin-bottom: 24rpx;
}
.avatar {
  width: 120rpx;
  height: 120rpx;
  border-radius: 50%;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
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
.menu-card {
  background: #fff;
  border-radius: 20rpx;
  padding: 8rpx 0;
  margin-bottom: 24rpx;
}
.menu-row {
  display: flex;
  align-items: center;
  padding: 28rpx 32rpx;
  border-bottom: 1px solid #f1f5f9;
}
.menu-row:last-child {
  border-bottom: none;
}
.menu-icon {
  font-size: 36rpx;
  margin-right: 20rpx;
}
.menu-label {
  flex: 1;
  font-size: 28rpx;
  color: #2d3748;
}
.menu-arrow {
  font-size: 40rpx;
  color: #cbd5e1;
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
.field-label {
  display: block;
  font-size: 24rpx;
  color: #64748b;
  margin-bottom: 10rpx;
}
.change-btn {
  border-radius: 999rpx;
  color: #fff;
  font-size: 30rpx;
  height: 88rpx;
  line-height: 88rpx;
  margin-top: 24rpx;
}
.error-text {
  color: #d92d20;
  font-size: 24rpx;
  margin-top: 12rpx;
}
</style>
