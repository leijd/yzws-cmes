<template>
  <view class="page">
    <!-- 查询入口 -->
    <view class="search-card">
      <text class="search-title">制衣厂门户</text>
      <text class="search-sub">输入工厂编码，查看该厂对外门户与实时产能概况</text>
      <view class="search-row">
        <input
          v-model.trim="code"
          class="search-input"
          placeholder="工厂编码（如 XXFZ）"
          confirm-type="search"
          @confirm="load"
        />
        <button class="search-btn" :disabled="loading || !code" @click="load">
          {{ loading ? "查询中…" : "查询" }}
        </button>
      </view>
    </view>

    <view v-if="error" class="error-card">
      <text class="error-icon">🔍</text>
      <text class="error-text">{{ error }}</text>
    </view>

    <!-- 门户内容 -->
    <template v-if="portal">
      <view class="hero">
        <view class="hero-top">
          <image
            v-if="logoFullUrl"
            :src="logoFullUrl"
            mode="aspectFit"
            class="hero-logo"
          />
          <view v-else class="hero-logo placeholder">
            <text>{{ portal.name.slice(0, 1) }}</text>
          </view>
          <view class="hero-info">
            <text class="hero-name">{{ portal.name }}</text>
            <view class="hero-meta">
              <text class="badge" :class="portal.edition">
                {{ portal.edition === "trial" ? "体验版" : "正式版" }}
              </text>
              <text class="code-text">{{ portal.code }}</text>
            </view>
          </view>
        </view>
        <text v-if="trialEndsText" class="trial-tip">体验期至 {{ trialEndsText }}</text>
      </view>

      <view class="welcome-card">
        <text class="welcome-title">{{ portal.content.welcome_title }}</text>
        <text class="welcome-text">{{ portal.content.welcome_text }}</text>
      </view>

      <view class="section-title">产能概况</view>
      <view class="stats-grid">
        <view class="stat-cell">
          <text class="stat-num">{{ portal.stats.today_orders }}</text>
          <text class="stat-label">今日工单</text>
        </view>
        <view class="stat-cell">
          <text class="stat-num">{{ portal.stats.wip_orders }}</text>
          <text class="stat-label">在制订单</text>
        </view>
        <view class="stat-cell">
          <text class="stat-num">{{ portal.stats.employees }}</text>
          <text class="stat-label">在职员工</text>
        </view>
        <view class="stat-cell">
          <text class="stat-num">{{ portal.stats.total_orders }}</text>
          <text class="stat-label">累计工单</text>
        </view>
      </view>

      <view class="actions">
        <button class="btn primary" @click="goLogin">进入本厂登录</button>
        <button class="btn ghost" @click="goRegister">我的工厂还没有入驻？申请注册</button>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { API_BASE_URL } from "../../config";
import { request } from "../../utils/request";

interface PortalData {
  code: string;
  name: string;
  short_code: string;
  edition: "trial" | "paid";
  trial_ends_at: number | null;
  content: {
    welcome_title: string;
    welcome_text: string;
    logo_url: string;
    customized: boolean;
  };
  stats: {
    today_orders: number;
    wip_orders: number;
    employees: number;
    total_orders: number;
  };
}

const code = ref("");
const loading = ref(false);
const error = ref("");
const portal = ref<PortalData | null>(null);

const logoFullUrl = computed(() => {
  const url = portal.value?.content.logo_url || "";
  if (!url) return "";
  if (/^https?:\/\//.test(url)) return url;
  return API_BASE_URL + (url.startsWith("/") ? url : "/" + url);
});

const trialEndsText = computed(() => {
  const t = portal.value?.trial_ends_at;
  if (!t) return "";
  // 后端时间为微秒时间戳
  const ms = t > 1e12 ? Math.floor(t / 1000) : t;
  const d = new Date(ms);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(
    d.getDate()
  ).padStart(2, "0")}`;
});

async function load() {
  error.value = "";
  portal.value = null;
  if (!code.value) {
    error.value = "请输入工厂编码";
    return;
  }
  loading.value = true;
  try {
    const res = await request<PortalData>(
      `/public/tenant-portal?code=${encodeURIComponent(code.value)}`,
      { silent: true, skipAuthRedirect: true }
    );
    portal.value = res;
    uni.setStorageSync("clothp_last_portal_code", res.code);
  } catch (e) {
    error.value = (e as Error).message || "查询失败，请稍后重试";
  } finally {
    loading.value = false;
  }
}

function goLogin() {
  uni.reLaunch({ url: "/pages/portal/platform?tab=login&method=account" });
}
function goRegister() {
  uni.navigateTo({ url: "/pages/register/register" });
}

onLoad((query) => {
  const q = (query as Record<string, string>) || {};
  if (q.code) {
    code.value = q.code;
    load();
  } else {
    code.value = (uni.getStorageSync("clothp_last_portal_code") as string) || "";
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
.search-card {
  background: #fff;
  border-radius: 16rpx;
  padding: 32rpx;
}
.search-title {
  display: block;
  font-size: 34rpx;
  font-weight: 700;
  color: #2d3748;
}
.search-sub {
  display: block;
  font-size: 24rpx;
  color: #718096;
  margin-top: 10rpx;
}
.search-row {
  display: flex;
  gap: 16rpx;
  margin-top: 28rpx;
}
.search-input {
  flex: 1;
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 16rpx 24rpx;
  font-size: 28rpx;
  line-height: 44rpx;
  min-height: 84rpx;
  box-sizing: border-box;
}
.search-btn {
  flex-shrink: 0;
  margin: 0;
  background: #2b6cb0;
  color: #fff;
  font-size: 26rpx;
  border-radius: 12rpx;
  padding: 0 32rpx;
  height: 84rpx;
  line-height: 84rpx;
}
.search-btn[disabled] {
  opacity: 0.5;
}
.error-card {
  background: #fff5f5;
  border: 1px solid #feb2b2;
  border-radius: 16rpx;
  padding: 48rpx 32rpx;
  margin-top: 32rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.error-icon {
  font-size: 56rpx;
  margin-bottom: 16rpx;
}
.error-text {
  font-size: 26rpx;
  color: #c53030;
  text-align: center;
}
.hero {
  background: linear-gradient(160deg, #2b6cb0 0%, #1a4368 70%, #12283e 100%);
  border-radius: 20rpx;
  padding: 40rpx 32rpx;
  margin-top: 32rpx;
}
.hero-top {
  display: flex;
  align-items: center;
}
.hero-logo {
  width: 112rpx;
  height: 112rpx;
  border-radius: 20rpx;
  background: rgba(255, 255, 255, 0.12);
  margin-right: 24rpx;
}
.hero-logo.placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
}
.hero-logo.placeholder text {
  color: #fff;
  font-size: 48rpx;
  font-weight: 700;
}
.hero-name {
  display: block;
  color: #fff;
  font-size: 36rpx;
  font-weight: 600;
}
.hero-meta {
  display: flex;
  align-items: center;
  gap: 16rpx;
  margin-top: 14rpx;
}
.badge {
  font-size: 20rpx;
  padding: 4rpx 16rpx;
  border-radius: 999rpx;
  color: #fff;
}
.badge.trial {
  background: #dd6b20;
}
.badge.paid {
  background: #38a169;
}
.code-text {
  color: rgba(255, 255, 255, 0.7);
  font-size: 24rpx;
}
.trial-tip {
  display: block;
  color: rgba(255, 255, 255, 0.7);
  font-size: 22rpx;
  margin-top: 24rpx;
}
.welcome-card {
  background: #fff;
  border-radius: 16rpx;
  padding: 32rpx;
  margin-top: 24rpx;
}
.welcome-title {
  display: block;
  font-size: 30rpx;
  font-weight: 600;
  color: #2d3748;
}
.welcome-text {
  display: block;
  font-size: 26rpx;
  color: #718096;
  line-height: 1.7;
  margin-top: 16rpx;
}
.section-title {
  font-size: 30rpx;
  font-weight: 600;
  color: #2d3748;
  margin: 32rpx 0 20rpx;
}
.stats-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 20rpx;
}
.stat-cell {
  width: calc((100% - 20rpx) / 2);
  background: #fff;
  border-radius: 16rpx;
  padding: 32rpx 0;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.stat-num {
  font-size: 44rpx;
  font-weight: 700;
  color: #2b6cb0;
}
.stat-label {
  font-size: 24rpx;
  color: #718096;
  margin-top: 8rpx;
}
.actions {
  margin-top: 40rpx;
  display: flex;
  flex-direction: column;
  gap: 20rpx;
}
.btn {
  border-radius: 14rpx;
  font-size: 30rpx;
}
.btn.primary {
  background: #2b6cb0;
  color: #fff;
}
.btn.ghost {
  background: #edf2f7;
  color: #4a5568;
}
</style>
