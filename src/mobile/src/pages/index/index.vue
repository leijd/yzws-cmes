<template>
  <view class="splash">
    <text class="logo">ClothP MES</text>
  </view>
</template>

<script setup lang="ts">
import { onShow } from "@dcloudio/uni-app";

onShow(() => {
  // 打开 App 一律进入平台门户，登录/体验/注册均从门户进入
  uni.reLaunch({ url: "/pages/portal/platform" });
});
</script>

<style scoped>
.splash {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #2b6cb0;
}
.logo {
  color: #fff;
  font-size: 32px;
  font-weight: 600;
  letter-spacing: 2px;
}
</style>
