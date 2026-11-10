<template>
  <view class="page">
    <view class="header">
      <text class="title">ClothP MES</text>
      <text class="subtitle">制衣生产移动工作台</text>
    </view>
    <view class="card">
      <view class="field">
        <input
          v-model.trim="loginName"
          placeholder="账号"
          autocomplete="username"
        />
      </view>
      <view class="field">
        <input
          v-model="password"
          password
          placeholder="密码"
          autocomplete="current-password"
        />
      </view>
      <view v-if="error" class="error">{{ error }}</view>
      <button class="submit" :disabled="loading" @click="submit">
        {{ loading ? "正在登录…" : "登 录" }}
      </button>
    </view>
    <view class="footer">
      <text>服务端：{{ serverDisplay }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { API_BASE_URL } from "../../config";
import { auth } from "../../store/auth";

const loginName = ref("");
const password = ref("");
const loading = ref(false);
const error = ref("");
const serverDisplay = API_BASE_URL.replace(/^https?:\/\//, "");

async function submit() {
  error.value = "";
  if (!loginName.value || !password.value) {
    error.value = "请填写账号和密码";
    return;
  }
  loading.value = true;
  try {
    await auth.login(loginName.value, password.value);
    uni.switchTab({ url: "/pages/workbench/workbench" });
  } catch (e) {
    error.value = (e as Error).message || "登录失败，请稍后重试";
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: linear-gradient(160deg, #2b6cb0 0%, #1a4368 60%, #12283e 100%);
  padding: 120rpx 48rpx 48rpx;
  box-sizing: border-box;
}
.header {
  margin-bottom: 64rpx;
}
.title {
  display: block;
  color: #fff;
  font-size: 48rpx;
  font-weight: 700;
  letter-spacing: 2rpx;
}
.subtitle {
  display: block;
  color: rgba(255, 255, 255, 0.75);
  font-size: 26rpx;
  margin-top: 12rpx;
}
.card {
  background: #fff;
  border-radius: 20rpx;
  padding: 40rpx 32rpx;
}
.field {
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 20rpx 24rpx;
  margin-bottom: 28rpx;
}
.error {
  color: #e53e3e;
  font-size: 26rpx;
  margin-bottom: 24rpx;
}
.submit {
  background: #2b6cb0;
  color: #fff;
  border-radius: 12rpx;
  font-size: 32rpx;
}
.submit[disabled] {
  opacity: 0.6;
}
.footer {
  margin-top: 48rpx;
  text-align: center;
  color: rgba(255, 255, 255, 0.5);
  font-size: 22rpx;
}
</style>
