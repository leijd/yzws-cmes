<template>
  <BrandNavBar title="功能设置" />
  <view class="page">
    <view class="title" style="margin-bottom: 20rpx">功能设置</view>
    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无功能开关</view>

    <view v-for="s in items" :key="s.setting_domain + '/' + s.setting_key" class="card row between">
      <view class="grow">
        <text class="title" style="font-size: 28rpx">{{ s.setting_domain }} / {{ s.setting_key }}</text>
        <text class="muted">当前值：{{ displayValue(s.setting_value) }}</text>
      </view>
      <switch :checked="boolValue(s.setting_value)" @change="toggle(s, $event)" color="#2563eb" />
    </view>

    <view v-if="items.length" class="card">
      <view class="field">
        <text class="field-label required">变更原因（每次调整必填，记入审计）</text>
        <input class="input" v-model.trim="reason" placeholder="如 按厂长要求开启计件校验" />
      </view>
      <text class="muted">开关调整会生成新的设置版本，点击右侧开关立即生效。</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface FeatureSetting {
  setting_domain: string; setting_key: string; setting_value: unknown; version: number;
}

const items = ref<FeatureSetting[]>([]);
const loading = ref(false);
const reason = ref("");

function displayValue(v: unknown): string {
  if (typeof v === "boolean") return v ? "开启" : "关闭";
  return JSON.stringify(v);
}
function boolValue(v: unknown): boolean {
  return v === true || v === "true" || v === 1;
}

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: FeatureSetting[] }>("/feature-settings", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function toggle(s: FeatureSetting, e: unknown) {
  const value = (e as { detail: { value: boolean } }).detail.value;
  if (!reason.value) {
    uni.showToast({ title: "请先在底部填写变更原因", icon: "none" });
    return;
  }
  try {
    await request(`/feature-settings/${s.setting_domain}/${s.setting_key}`, {
      method: "PUT",
      data: { value, version: s.version, reason: reason.value },
    });
    uni.showToast({ title: "已更新", icon: "none" });
    reason.value = "";
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "更新失败", icon: "none" });
  }
}

onLoad(load);
</script>
