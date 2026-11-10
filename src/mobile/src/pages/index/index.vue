<template>
  <view class="splash">
    <text class="logo">ClothP MES</text>
  </view>
</template>

<script setup lang="ts">
import { onShow } from "@dcloudio/uni-app";
import { getToken } from "../../utils/request";

onShow(() => {
  if (getToken()) {
    uni.switchTab({ url: "/pages/workbench/workbench" });
  } else {
    uni.reLaunch({ url: "/pages/login/login" });
  }
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
