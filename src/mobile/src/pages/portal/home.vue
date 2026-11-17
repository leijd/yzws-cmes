<template>
  <view class="portal">
    <!-- 顶部：本厂品牌 -->
    <view class="nav">
      <view class="brand">
        <image v-if="logo" class="logo" :src="logo" mode="aspectFit" />
        <view v-else class="logo-fallback">{{ (tenantName || "厂").slice(0, 1) }}</view>
        <view class="brand-text">
          <text class="brand-name">{{ tenantName || "制衣厂门户" }}</text>
          <text class="brand-sub">{{ systemName || "生产管理系统" }} · {{ editionLabel }}</text>
        </view>
      </view>
      <!-- 名下多家制衣厂：编码位变为「切换」入口；单厂不显示 -->
      <button v-if="factories.length > 1" class="switch-btn" @click="switchFactory">切换 ▾</button>
    </view>

    <!-- 欢迎区 -->
    <view class="hero">
      <view class="hero-tags">
        <text class="tag">{{ editionLabel }}</text>
        <text v-if="daysLeft !== null" class="tag warn">剩余 {{ daysLeft }} 天</text>
      </view>
      <text class="hero-title">{{ welcomeTitle }}</text>
      <text class="hero-text">{{ welcomeText }}</text>
      <button class="btn-primary" @click="enterWork">进入工作台 ›</button>
      <text class="hero-tip">当前账号：{{ accountName }}</text>
    </view>

    <!-- 概览 -->
    <view v-if="stats.length" class="stats">
      <view v-for="s in stats" :key="s.label" class="stat">
        <text class="stat-num">{{ s.value }}</text>
        <text class="stat-label">{{ s.label }}</text>
      </view>
    </view>

    <!-- 场景痛点：与网页版门户 intro 区一致 -->
    <view class="section">
      <text class="section-title">{{ content.intro_title }}</text>
      <view v-for="(p, i) in content.pain_points" :key="i" class="pain">
        <text class="pain-no">{{ i + 1 }}</text>
        <view class="pain-body">
          <text class="pain-title">{{ p.title }}</text>
          <text class="pain-desc">{{ p.desc }}</text>
        </view>
      </view>
      <text v-if="!content.pain_points.length" class="empty">生产管理中的常见难题将在这里展示</text>
    </view>

    <!-- 解决方案：与网页版门户 solution 区一致 -->
    <view class="section">
      <text class="section-title">{{ content.solution_title }}</text>
      <text class="section-text">{{ content.solution_text }}</text>
    </view>

    <!-- 功能体验：与网页版门户 modules 区一致，可点击点亮 -->
    <view class="section">
      <text class="section-title">功能体验</text>
      <text class="section-text">已点亮 {{ litCount }}/{{ features.length }} 项，点击即可点亮对应功能</text>
      <view class="feature-grid">
        <view v-for="f in features" :key="f.code" class="feature" :class="{ lit: f.lit === 1 }" @click="lightFeature(f)">
          <text class="feature-name">{{ f.name }}</text>
          <text class="feature-state">{{ f.lit === 1 ? "已点亮 ✓" : "点击点亮" }}</text>
        </view>
      </view>
      <text v-if="!features.length" class="empty">暂无可点亮的功能</text>
      <text v-if="featureNotice" class="section-text">{{ featureNotice }}</text>
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
    <text class="brand-foot">© {{ year }} {{ tenantName || "本厂" }} · 本厂内部工作门户</text>

    <!-- 切换制衣厂弹层：名下多家厂时无感切换 -->
    <view v-if="factorySheet" class="sheet-mask" @click="factorySheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">切换制衣厂</view>
        <view
          v-for="f in factories"
          :key="f.code"
          class="card person"
          style="margin-bottom: 12rpx"
          @click="chooseFactory(f)"
        >
          <view style="flex: 1; min-width: 0">
            <view style="font-size: 28rpx">
              <text style="font-weight: 600">{{ f.name }}</text>
              <text class="muted" style="margin-left: 12rpx">{{ f.code }}</text>
            </view>
            <view v-if="f.kind" class="muted" style="font-size: 22rpx; margin-top: 4rpx">
              {{ f.kind === "trial" ? "体验厂" : "正式厂" }}
            </view>
          </view>
          <text v-if="f.current" class="tag tag-green">当前</text>
        </view>
        <button class="btn btn-block" style="margin-top: 12rpx" @click="factorySheet = false">取消</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request, getToken, setToken } from "../../utils/request";
import { auth } from "../../store/auth";
import { API_BASE_URL } from "../../config";

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
const systemName = ref("");
const logo = ref("");
const year = new Date().getFullYear();

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
      ? "欢迎使用本厂生产管理系统"
      : `欢迎使用「${tenantName.value || "本厂"}」生产管理系统`)
);
const welcomeText = computed(
  () =>
    portal.value?.content?.welcome_text ||
    (isTrial.value
      ? "免费体验版已开通：这是一个真实的制衣厂数字化车间，订单、扫码、计件工资一网即成，您可以亲手把一个订单跑起来。"
      : "这里是本厂生产管理系统的工作入口：订单、排产、扫菲报工、计件工资一条线走完，随时掌握车间实时进度。")
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

/* 与网页版制衣厂门户一致的内容：场景痛点、解决方案、功能体验点亮 */
interface PainPoint { title: string; desc: string }
interface PortalContent {
  intro_title: string; pain_points: PainPoint[]; solution_title: string; solution_text: string
}
interface TrialFeature { code: string; name: string; path?: string; lit: number }
const content = ref<PortalContent>({
  intro_title: "这些场景，您是不是也遇到过？",
  pain_points: [],
  solution_title: "我们的解决方案",
  solution_text: "订单、排产、扫菲报工、计件工资一条线走完，随时掌握车间实时进度。",
});
const features = ref<TrialFeature[]>([]);
const litCount = computed(() => features.value.filter((f) => f.lit === 1).length);
const featureNotice = ref("");

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
    /* logo 是库内输出接口的相对路径（/api/v1/public/tenant-logo?...），
     * App 无同源概念，需要拼 API_BASE_URL；H5 下该值为空仍走同源。 */
    if (b?.logo_url) {
      const url = b.logo_url.trim();
      logo.value = /^(https?:|data:)/i.test(url)
        ? url
        : API_BASE_URL + (url.startsWith("/") ? url : "/" + url);
    }
  } catch {
    /* 未配置品牌时用平台默认 */
  }
  /* 门户文案与体验功能列表：与网页版制衣厂门户共用同一批公开接口 */
  try {
    const r = await request<{ content?: Partial<PortalContent> }>("/public/trial-content", {
      silent: true, skipAuthRedirect: true,
    });
    if (r?.content) content.value = { ...content.value, ...r.content, pain_points: r.content.pain_points || [] };
  } catch {
    /* 接口不可用时使用内置默认文案 */
  }
  if (code) {
    try {
      const r = await request<{ items: TrialFeature[] }>(
        `/public/trial-features?tenant=${encodeURIComponent(code)}`,
        { silent: true, skipAuthRedirect: true }
      );
      features.value = r.items || [];
    } catch {
      features.value = [];
    }
  }
  if (!auth.user) await auth.fetchMe().catch(() => undefined);
  if (!Object.keys(auth.permissions).length) await auth.fetchPermissions();
}

onShow(() => {
  void load();
  void loadFactoryList();
});

async function lightFeature(f: TrialFeature) {
  if (f.lit === 1) { featureNotice.value = `「${f.name}」已点亮`; return; }
  try {
    await request("/public/trial-features/light", {
      method: "POST",
      data: { tenant: tenantCode.value, code: f.code },
      silent: true, skipAuthRedirect: true,
    });
    f.lit = 1;
    featureNotice.value = `「${f.name}」已点亮（${litCount.value}/${features.value.length}）`;
  } catch (e) {
    featureNotice.value = (e as Error).message || "点亮失败，请稍后重试";
  }
}

function enterWork() {
  uni.switchTab({ url: "/pages/workbench/workbench" });
}
function open(url: string) {
  uni.navigateTo({ url });
}
function goSettings() {
  uni.navigateTo({ url: "/pages/settings/portal" });
}
/* ===== 切换制衣厂：名下多家厂时应用内无感切换（换会话刷新门户），无需回登录页 ===== */
interface FactoryItem { code: string; name: string; kind?: string; current?: boolean }
const factories = ref<FactoryItem[]>([]);
const factorySheet = ref(false);
const switching = ref(false);

/* 预加载名下制衣厂列表：>1 时顶部显示「切换」入口 */
async function loadFactoryList() {
  if (!getToken()) return;
  try {
    const r = await request<{ items: FactoryItem[] }>("/auth/my-factories", { silent: true });
    factories.value = r.items || [];
  } catch {
    factories.value = [];
  }
}

async function switchFactory() {
  if (!getToken()) {
    uni.reLaunch({ url: "/pages/portal/platform?tab=login&method=sms" });
    return;
  }
  if (!factories.value.length) await loadFactoryList();
  if (factories.value.length <= 1) {
    uni.showToast({ title: "名下只有一家制衣厂", icon: "none" });
    return;
  }
  factorySheet.value = true;
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
    factorySheet.value = false;
    await load();
    await loadFactoryList(); /* 刷新列表的"当前"标记 */
    uni.showToast({ title: `已切换到「${r.tenant_name || f.name}」`, icon: "success" });
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "切换失败", icon: "none" });
  } finally {
    uni.hideLoading();
    switching.value = false;
  }
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
.sheet-mask { position: fixed; inset: 0; background: rgba(15, 23, 42, 0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 70vh; overflow-y: auto; box-sizing: border-box; }
.card { background: #fff; border: 1rpx solid #eef1f5; border-radius: 16rpx; padding: 20rpx 24rpx; }
.person { display: flex; align-items: center; gap: 16rpx; cursor: pointer; }
.muted { color: #8a94a6; font-size: 26rpx; }
.tag { font-size: 22rpx; padding: 4rpx 16rpx; border-radius: 20rpx; }
.tag-green { background: #e6f7ee; color: #1a9e5c; }
.btn-block { width: 100%; }
.switch-btn { background: #eef4ff; color: #2563eb; border: 1rpx solid #c7dcff; border-radius: 24rpx; padding: 6rpx 24rpx; font-size: 22rpx; }
.portal {
  min-height: 100vh;
  background: linear-gradient(160deg, #e8f2ff 0%, #eef7ff 40%, #e6f1fb 100%);
  /* 自定义导航：让出手机系统状态栏高度（App 端 uni-app 注入 --status-bar-height；H5 为 0 不受影响） */
  padding: calc(var(--status-bar-height, 0px) + 24rpx) 28rpx 60rpx;
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
/* 与网页版登录窗口的「登录」按钮一致：全宽矩形、渐变、字号与内边距 */
.btn-primary {
  width: 100%;
  margin-top: 32rpx;
  border: 0;
  border-radius: 12rpx;
  padding: 24rpx 0;
  font-size: 34rpx;
  font-weight: 600;
  letter-spacing: 0.16em;
  color: #fff;
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  box-shadow: 0 16rpx 40rpx rgba(37, 99, 235, 0.25);
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

/* 与网页版门户一致的内容区 */
.section-text {
  display: block;
  margin-top: 12rpx;
  font-size: 24rpx;
  color: #64748b;
  line-height: 1.7;
}
.pain {
  display: flex;
  gap: 16rpx;
  margin-top: 18rpx;
  padding: 18rpx;
  border-radius: 16rpx;
  background: #f8fafc;
}
.pain-no {
  width: 40rpx;
  height: 40rpx;
  flex: none;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: #2563eb;
  color: #fff;
  font-size: 22rpx;
}
.pain-body {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6rpx;
}
.pain-title {
  font-size: 26rpx;
  font-weight: 600;
  color: #0f172a;
}
.pain-desc {
  font-size: 22rpx;
  color: #64748b;
  line-height: 1.6;
}
.feature-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 16rpx;
  margin-top: 20rpx;
}
.feature {
  width: calc((100% - 16rpx) / 2);
  box-sizing: border-box;
  padding: 20rpx;
  border-radius: 16rpx;
  border: 1rpx solid #e2e8f0;
  background: #fff;
  display: flex;
  flex-direction: column;
  gap: 8rpx;
}
.feature.lit {
  background: #eff6ff;
  border-color: #bfdbfe;
}
.feature-name {
  font-size: 26rpx;
  font-weight: 600;
  color: #0f172a;
}
.feature-state {
  font-size: 22rpx;
  color: #64748b;
}
.feature.lit .feature-state {
  color: #1d4ed8;
}
.brand-foot {
  display: block;
  text-align: center;
  font-size: 20rpx;
  color: #94a3b8;
  margin-top: 28rpx;
}
</style>
