<template>
  <view class="brand-bar">
    <view class="brand-bar-inner">
      <text v-if="canBack" class="back" @click="goBack">‹ 返回</text>
      <text class="title">{{ title || "ClothP MES" }}</text>
      <!-- 标题栏最右侧：logo 在上，下面是小字制衣厂名称，整体右对齐 -->
      <view class="brand">
        <image v-if="logo" class="brand-logo" :src="logo" mode="aspectFit" />
        <view v-else class="brand-logo brand-logo-fallback">智</view>
        <text class="brand-name">{{ brandName }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { request, getToken } from "../../utils/request";
import { API_BASE_URL } from "../../config";

/* 各工作模块统一自定义标题栏：中间页面标题，右侧本厂品牌（logo + 小字厂名）。
 * 品牌数据与网页版同源 /tenant/branding；App 每个页面是独立 webview，
 * 用本地缓存（5 分钟）避免切页时反复请求。 */
defineProps<{ title?: string }>();

const canBack = ref(false);
const logo = ref("");
const brandName = ref("一智算");

interface CachedBrand {
  ts: number;
  name: string;
  logo: string;
}
const CACHE_KEY = "clothp_brand_cache";
const CACHE_TTL = 5 * 60 * 1000;

function apply(name: string, url: string) {
  brandName.value = name || "一智算";
  if (!url) {
    logo.value = "";
    return;
  }
  /* 上传的 logo 是 /api/v1/public/tenant-logo?... 相对路径，App 需拼绝对地址 */
  logo.value = /^(https?:|data:)/i.test(url)
    ? url
    : API_BASE_URL + (url.startsWith("/") ? url : "/" + url);
}

function cache(name: string, url: string) {
  try {
    uni.setStorageSync(CACHE_KEY, { ts: Date.now(), name, logo: url } as CachedBrand);
  } catch {
    /* 存储不可用不影响显示 */
  }
}

function goBack() {
  uni.navigateBack();
}

onMounted(() => {
  const stack = getCurrentPages();
  const route = (stack[stack.length - 1] as { route?: string } | undefined)?.route || "";
  /* tabBar 页没有返回上一层 */
  const tabRoutes = ["pages/workbench/workbench", "pages/me/me"];
  canBack.value = stack.length > 1 && !tabRoutes.includes(route);
  if (!getToken()) return;
  try {
    const raw = uni.getStorageSync(CACHE_KEY) as CachedBrand | "";
    if (raw && typeof raw === "object" && Date.now() - raw.ts < CACHE_TTL) {
      logo.value = raw.logo || "";
      brandName.value = raw.name || "一智算";
      return;
    }
  } catch {
    /* 缓存不可用时继续请求 */
  }
  void request<{
    tenant_name?: string;
    name?: string;
    system_name?: string;
    logo_url?: string;
    default_system_name?: string;
  }>("/tenant/branding", { silent: true })
    .then((b) => {
      const name = b.tenant_name || b.name || b.system_name || b.default_system_name || "一智算";
      apply(name, (b.logo_url || "").trim());
      cache(brandName.value, logo.value);
    })
    .catch(() => apply("一智算", ""));
});
</script>

<style scoped>
.brand-bar {
  /* 自定义导航时让出状态栏高度（H5 下变量为 0） */
  padding-top: var(--status-bar-height, 0px);
  background: #fff;
  border-bottom: 1px solid #edf2f7;
}
.brand-bar-inner {
  position: relative;
  height: 88rpx;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 20rpx;
}
.back {
  position: absolute;
  left: 20rpx;
  font-size: 26rpx;
  color: #2563eb;
}
.title {
  max-width: 46%;
  font-size: 32rpx;
  font-weight: 600;
  color: #1f2937;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.brand {
  position: absolute;
  right: 20rpx;
  top: 8rpx;
  bottom: 8rpx;
  max-width: 34%;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  justify-content: center;
  gap: 2rpx;
}
.brand-logo {
  width: 40rpx;
  height: 40rpx;
  flex: none;
  border-radius: 8rpx;
  background: #eff6ff;
}
.brand-logo-fallback {
  background: linear-gradient(135deg, #2563eb, #38bdf8);
  color: #fff;
  font-size: 22rpx;
  font-weight: 600;
  text-align: center;
  line-height: 40rpx;
}
.brand-name {
  max-width: 100%;
  font-size: 20rpx;
  color: #64748b;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
</style>
