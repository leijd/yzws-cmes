<template>
  <view class="brand-bar">
    <view class="brand-bar-inner">
      <text v-if="canBack" class="back" @click="goBack">‹ 返回</text>
      <text class="title">{{ title || "生产管理" }}</text>
      <!-- 标题栏最右侧：logo 在上，下面是小字制衣厂名称，整体右对齐。
           名下多家制衣厂时整块可点击，弹出切换列表（换会话后回工作台重载）。 -->
      <view class="brand" :class="{ clickable: factories.length > 1 }" @click="onBrandTap">
        <image v-if="logo" class="brand-logo" :src="logo" mode="aspectFit" />
        <view v-else class="brand-logo brand-logo-fallback">{{ (brandName || "厂").slice(0, 1) }}</view>
        <text class="brand-name">{{ brandName }}{{ factories.length > 1 ? " ▾" : "" }}</text>
      </view>
    </view>

    <!-- 切换制衣厂弹层 -->
    <view v-if="sheetOpen" class="nav-sheet-mask" @click="sheetOpen = false">
      <view class="nav-sheet" @click.stop>
        <view class="nav-sheet-title">切换制衣厂</view>
        <view
          v-for="f in factories"
          :key="f.code"
          class="nav-sheet-item"
          :class="{ current: f.current }"
          @click="chooseFactory(f)"
        >
          <text class="nav-sheet-name">{{ f.name }}</text>
          <text v-if="f.current" class="nav-sheet-current">当前</text>
        </view>
        <button class="nav-sheet-cancel" @click="sheetOpen = false">取消</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { request, getToken, setToken } from "../../utils/request";
import { auth } from "../../store/auth";
import { API_BASE_URL } from "../../config";

/* 各工作模块统一自定义标题栏：中间页面标题，右侧本厂品牌（logo + 小字厂名）。
 * 品牌数据与网页版同源 /tenant/branding；App 每个页面是独立 webview，
 * 用本地缓存（5 分钟）避免切页时反复请求。 */
defineProps<{ title?: string }>();

const canBack = ref(false);
const logo = ref("");
const brandName = ref("本厂");

interface CachedBrand {
  ts: number;
  name: string;
  logo: string;
}
const CACHE_KEY = "clothp_brand_cache";
const CACHE_TTL = 5 * 60 * 1000;

function apply(name: string, url: string) {
  brandName.value = name || "本厂";
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

/* ===== 多制衣厂切换：名下多家厂时品牌区可点击，弹出列表换会话（回工作台重载） ===== */
interface FactoryItem { code: string; name: string; current?: boolean }
const factories = ref<FactoryItem[]>([]);
const sheetOpen = ref(false);
const switching = ref(false);

async function loadFactories() {
  if (!getToken()) return;
  try {
    const r = await request<{ items: FactoryItem[] }>("/auth/my-factories", { silent: true });
    factories.value = r.items || [];
  } catch {
    factories.value = [];
  }
}

function onBrandTap() {
  if (factories.value.length <= 1 || switching.value) return;
  sheetOpen.value = true;
}

async function chooseFactory(f: FactoryItem) {
  if (f.current || switching.value) return;
  switching.value = true;
  uni.showLoading({ title: "切换中…", mask: true });
  try {
    const r = await request<{ access_token: string; tenant_name?: string }>("/auth/switch-tenant", {
      method: "POST",
      data: { tenant_code: f.code, node_id: "mobile-app" },
      silent: true,
    });
    setToken(r.access_token);
    await Promise.all([auth.fetchMe(), auth.fetchPermissions()]);
    /* 清品牌缓存，避免切厂后标题栏还显示旧厂名 */
    try { uni.removeStorageSync(CACHE_KEY); } catch { /* 忽略 */ }
    sheetOpen.value = false;
    uni.showToast({ title: `已切换到「${r.tenant_name || f.name}」`, icon: "success" });
    /* 从任何页面切厂后统一落到工作台重载，避免旧页面栈持旧会话数据 */
    setTimeout(() => uni.reLaunch({ url: "/pages/workbench/workbench" }), 400);
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "切换失败", icon: "none" });
  } finally {
    uni.hideLoading();
    switching.value = false;
  }
}

onMounted(() => {
  const stack = getCurrentPages();
  const route = (stack[stack.length - 1] as { route?: string } | undefined)?.route || "";
  /* tabBar 页没有返回上一层 */
  const tabRoutes = ["pages/workbench/workbench", "pages/me/me"];
  canBack.value = stack.length > 1 && !tabRoutes.includes(route);
  if (!getToken()) return;
  void loadFactories();
  try {
    const raw = uni.getStorageSync(CACHE_KEY) as CachedBrand | "";
    if (raw && typeof raw === "object" && Date.now() - raw.ts < CACHE_TTL) {
      logo.value = raw.logo || "";
      brandName.value = raw.name || "本厂";
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
      const name = b.tenant_name || b.name || b.system_name || "本厂";
      apply(name, (b.logo_url || "").trim());
      cache(brandName.value, logo.value);
    })
    .catch(() => apply("本厂", ""));
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
.brand.clickable { cursor: pointer; }
.brand.clickable .brand-name { color: #2563eb; }
.nav-sheet-mask { position: fixed; inset: 0; background: rgba(15, 23, 42, 0.45); z-index: 100; display: flex; align-items: flex-end; }
.nav-sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 60vh; overflow-y: auto; box-sizing: border-box; }
.nav-sheet-title { font-size: 30rpx; font-weight: 600; margin-bottom: 24rpx; }
.nav-sheet-item { display: flex; align-items: center; justify-content: space-between; padding: 24rpx 8rpx; border-bottom: 1rpx solid #eef1f5; }
.nav-sheet-item.current { opacity: 0.55; }
.nav-sheet-name { font-size: 28rpx; }
.nav-sheet-current { font-size: 22rpx; color: #1a9e5c; background: #e6f7ee; border-radius: 20rpx; padding: 4rpx 16rpx; }
.nav-sheet-cancel { margin-top: 24rpx; width: 100%; background: #f1f5f9; color: #475569; border: none; border-radius: 12rpx; padding: 20rpx 0; font-size: 28rpx; }
</style>
