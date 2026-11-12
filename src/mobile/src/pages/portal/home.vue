<template>
  <view class="portal">
    <!-- 顶部：本厂品牌 -->
    <view class="nav">
      <view class="brand">
        <image v-if="logo" class="logo" :src="logo" mode="aspectFit" />
        <view v-else class="logo-fallback">{{ (tenantName || "厂").slice(0, 1) }}</view>
        <view class="brand-text">
          <text class="brand-name">{{ tenantName || "制衣厂门户" }}</text>
          <text class="brand-sub">{{ systemName }} · {{ editionLabel }}</text>
        </view>
      </view>
      <text v-if="tenantCode" class="code-chip">{{ tenantCode }}</text>
    </view>

    <!-- 欢迎区 -->
    <view class="hero">
      <view class="hero-tags">
        <text class="tag">{{ editionLabel }}</text>
        <text v-if="daysLeft !== null" class="tag warn">剩余 {{ daysLeft }} 天</text>
      </view>
      <text class="hero-title">{{ welcomeTitle }}</text>
      <text class="hero-text">{{ welcomeText }}</text>
      <button class="btn-primary" @click="enterWork">进入{{ systemName }}工作台 ›</button>
      <text class="hero-tip">当前账号：{{ accountName }}</text>
    </view>

    <!-- 概览 -->
    <view v-if="stats.length" class="stats">
      <view v-for="s in stats" :key="s.label" class="stat">
        <text class="stat-num">{{ s.value }}</text>
        <text class="stat-label">{{ s.label }}</text>
      </view>
    </view>

    <!-- 常用入口 -->
    <view class="section">
      <text class="section-title">常用入口</text>
      <view class="grid">
        <view v-for="e in entries" :key="e.name" class="cell" @click="open(e.url)">
          <text class="cell-icon">{{ e.icon }}</text>
          <text class="cell-name">{{ e.name }}</text>
        </view>
      </view>
      <text v-if="!entries.length" class="empty">当前账号暂无移动端快捷入口</text>
    </view>

    <!-- 页脚操作 -->
    <view class="footer">
      <text class="link" @click="goSettings">门户内容设置</text>
      <text class="link" @click="switchFactory">切换工厂</text>
      <text class="link danger" @click="logout">退出登录</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request, getToken } from "../../utils/request";
import { auth } from "../../store/auth";

interface TenantProfile {
  code: string;
  name: string;
  short_code?: string;
  customer_type?: string;
  registration_status?: string;
  subscription?: { edition?: string; status?: string; expires_at?: number | null } | null;
}

interface PublicPortal {
  code: string;
  name: string;
  short_code?: string;
  edition?: "trial" | "paid";
  trial_ends_at?: number | null;
  content?: { welcome_title?: string; welcome_text?: string; logo_url?: string };
  stats?: { today_orders?: number; wip_orders?: number; employees?: number; total_orders?: number };
}

interface Entry {
  icon: string;
  name: string;
  url: string;
  perm?: string;
  permAny?: string[];
}

const profile = ref<TenantProfile | null>(null);
const portal = ref<PublicPortal | null>(null);

const tenantCode = computed(() => profile.value?.code || portal.value?.code || "");
const tenantName = computed(() => portal.value?.name || profile.value?.name || "");
const systemName = ref("一智算");
const logo = ref("");

const isTrial = computed(() => {
  const edition = portal.value?.edition || (profile.value?.customer_type === "trial" ? "trial" : "paid");
  return edition === "trial";
});
const editionLabel = computed(() => (isTrial.value ? "免费体验版" : "正式版"));

const trialEndsAt = computed(() => {
  const ends = portal.value?.trial_ends_at || profile.value?.subscription?.expires_at || 0;
  return Number(ends) || 0;
});
const daysLeft = computed<number | null>(() => {
  if (!isTrial.value || !trialEndsAt.value) return null;
  return Math.max(0, Math.ceil((trialEndsAt.value / 1000 - Date.now()) / 86400000));
});

const welcomeTitle = computed(
  () =>
    portal.value?.content?.welcome_title ||
    (isTrial.value
      ? "欢迎体验一智算制衣 MES"
      : `欢迎使用「${tenantName.value || "本厂"}」生产管理系统`)
);
const welcomeText = computed(
  () =>
    portal.value?.content?.welcome_text ||
    (isTrial.value
      ? "免费体验版已开通：这是一个真实的制衣厂数字化车间，订单、扫码、计件工资一网即成，您可以亲手把一个订单跑起来。"
      : "这里是一智算制衣 MES 的工作入口：订单、排产、扫菲报工、计件工资一条线走完，随时掌握车间实时进度。")
);

const accountName = computed(
  () => auth.user?.employee_name || auth.user?.login_name || "本厂用户"
);

const stats = computed(() => {
  const s = portal.value?.stats;
  if (!s) return [];
  return [
    { label: "今日工单", value: String(s.today_orders ?? 0) },
    { label: "在制订单", value: String(s.wip_orders ?? 0) },
    { label: "员工总数", value: String(s.employees ?? 0) },
    { label: "累计工单", value: String(s.total_orders ?? 0) },
  ];
});

const allEntries: Entry[] = [
  { icon: "📷", name: "扫码派工/报工", url: "/pages/report/scan", permAny: ["report:create", "task:assign"] },
  { icon: "📝", name: "我的报工", url: "/pages/report/mine", perm: "report:read" },
  { icon: "✅", name: "任务与派工", url: "/pages/tasks/list", perm: "task:read" },
  { icon: "📦", name: "项目与工单", url: "/pages/orders/list", perm: "work_order:read" },
  { icon: "☑️", name: "报工审批", url: "/pages/report/review", perm: "review:approve" },
  { icon: "📢", name: "生产通知", url: "/pages/notify/list" },
  { icon: "💰", name: "我的工资", url: "/pages/my/wages" },
  { icon: "🏢", name: "本厂信息", url: "/pages/settings/profile" },
];
const entries = computed(() =>
  allEntries.filter((e) => {
    if (e.permAny) return e.permAny.some((p) => auth.hasPerm(p));
    return !e.perm || auth.hasPerm(e.perm);
  })
);

async function load() {
  if (!getToken()) {
    uni.reLaunch({ url: "/pages/portal/platform?tab=login&method=sms" });
    return;
  }
  try {
    profile.value = await request<TenantProfile>("/tenant/profile", { silent: true });
  } catch {
    /* 资料加载失败不阻塞门户展示 */
  }
  const code = profile.value?.code || "";
  if (code) {
    try {
      portal.value = await request<PublicPortal>(
        `/public/tenant-portal?code=${encodeURIComponent(code)}`,
        { silent: true, skipAuthRedirect: true }
      );
    } catch {
      /* 门户内容未配置/未审批：用本厂资料与默认文案 */
    }
  }
  /* logo 与系统名：/tenant/branding 对所有登录用户开放 */
  try {
    const b = await request<{ tenant_name?: string; system_name?: string; logo_url?: string }>(
      "/tenant/branding",
      { silent: true }
    );
    if (b?.system_name) systemName.value = b.system_name;
    if (b?.logo_url) logo.value = b.logo_url;
  } catch {
    /* 未配置品牌时用平台默认 */
  }
  if (!auth.user) await auth.fetchMe().catch(() => undefined);
  if (!Object.keys(auth.permissions).length) await auth.fetchPermissions();
}

onShow(() => {
  void load();
});

function enterWork() {
  uni.switchTab({ url: "/pages/workbench/workbench" });
}
function open(url: string) {
  uni.navigateTo({ url });
}
function goSettings() {
  uni.navigateTo({ url: "/pages/settings/portal" });
}
function switchFactory() {
  uni.showModal({
    title: "切换工厂",
    content: "将返回登录页，用手机号短信登录选择其他制衣厂。",
    success: (r) => {
      if (!r.confirm) return;
      uni.reLaunch({ url: "/pages/portal/platform?tab=login&method=sms" });
    },
  });
}
function logout() {
  uni.showModal({
    title: "退出登录",
    content: "确认退出当前制衣厂？",
    success: (r) => {
      if (r.confirm) void auth.logout();
    },
  });
}
</script>

<style scoped>
.portal {
  min-height: 100vh;
  background: linear-gradient(160deg, #e8f2ff 0%, #eef7ff 40%, #e6f1fb 100%);
  padding: 24rpx 28rpx 60rpx;
  box-sizing: border-box;
}

/* 顶部品牌 */
.nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16rpx;
}
.brand {
  display: flex;
  align-items: center;
  gap: 18rpx;
  flex: 1;
  min-width: 0;
}
.logo {
  width: 84rpx;
  height: 84rpx;
  border-radius: 20rpx;
  background: #fff;
  padding: 6rpx;
  box-shadow: 0 6rpx 18rpx rgba(37, 99, 235, 0.12);
}
.logo-fallback {
  width: 84rpx;
  height: 84rpx;
  border-radius: 20rpx;
  background: linear-gradient(135deg, #2563eb, #38bdf8);
  color: #fff;
  font-size: 40rpx;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.brand-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.brand-name {
  font-size: 32rpx;
  font-weight: 700;
  color: #0f172a;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.brand-sub {
  margin-top: 6rpx;
  font-size: 22rpx;
  color: #64748b;
}
.code-chip {
  font-size: 22rpx;
  color: #1d4ed8;
  background: rgba(37, 99, 235, 0.1);
  border-radius: 999rpx;
  padding: 8rpx 20rpx;
}

/* 欢迎区 */
.hero {
  margin-top: 28rpx;
  background: #fff;
  border-radius: 28rpx;
  padding: 36rpx 32rpx 32rpx;
  box-shadow: 0 12rpx 32rpx rgba(15, 23, 42, 0.06);
  display: flex;
  flex-direction: column;
}
.hero-tags {
  display: flex;
  gap: 12rpx;
  margin-bottom: 20rpx;
}
.tag {
  font-size: 22rpx;
  color: #0369a1;
  background: #e0f2fe;
  border-radius: 999rpx;
  padding: 6rpx 18rpx;
}
.tag.warn {
  color: #b45309;
  background: #fef3c7;
}
.hero-title {
  font-size: 36rpx;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.4;
}
.hero-text {
  margin-top: 16rpx;
  font-size: 26rpx;
  color: #475569;
  line-height: 1.7;
}
.btn-primary {
  margin-top: 32rpx;
  background: linear-gradient(135deg, #2563eb, #38bdf8);
  color: #fff;
  font-size: 30rpx;
  font-weight: 600;
  border-radius: 999rpx;
  padding: 22rpx 0;
  box-shadow: 0 12rpx 26rpx rgba(37, 99, 235, 0.28);
}
.btn-primary::after {
  border: none;
}
.hero-tip {
  margin-top: 18rpx;
  font-size: 22rpx;
  color: #94a3b8;
  text-align: center;
}

/* 概览 */
.stats {
  margin-top: 24rpx;
  background: #fff;
  border-radius: 24rpx;
  padding: 28rpx 8rpx;
  display: flex;
  box-shadow: 0 10rpx 26rpx rgba(15, 23, 42, 0.05);
}
.stat {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.stat-num {
  font-size: 34rpx;
  font-weight: 700;
  color: #1d4ed8;
}
.stat-label {
  margin-top: 8rpx;
  font-size: 22rpx;
  color: #64748b;
}

/* 常用入口 */
.section {
  margin-top: 24rpx;
  background: #fff;
  border-radius: 24rpx;
  padding: 28rpx;
  box-shadow: 0 10rpx 26rpx rgba(15, 23, 42, 0.05);
}
.section-title {
  font-size: 28rpx;
  font-weight: 600;
  color: #0f172a;
}
.grid {
  margin-top: 22rpx;
  display: flex;
  flex-wrap: wrap;
  gap: 18rpx;
}
.cell {
  width: calc((100% - 54rpx) / 4);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 16rpx 0;
  border-radius: 18rpx;
}
.cell:active {
  background: #eff6ff;
}
.cell-icon {
  font-size: 40rpx;
}
.cell-name {
  margin-top: 10rpx;
  font-size: 20rpx;
  color: #475569;
  text-align: center;
  line-height: 1.3;
}
.empty {
  display: block;
  margin-top: 20rpx;
  font-size: 24rpx;
  color: #94a3b8;
}

/* 页脚 */
.footer {
  margin-top: 34rpx;
  display: flex;
  justify-content: center;
  gap: 40rpx;
}
.link {
  font-size: 24rpx;
  color: #1d4ed8;
}
.link.danger {
  color: #dc2626;
}
</style>
