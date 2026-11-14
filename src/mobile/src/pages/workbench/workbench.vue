<template>
  <BrandNavBar title="工作台" />
  <view class="page">
    <view class="hero">
      <text class="hero-logout" @click="onLogout">退出</text>
      <text class="hello">{{ greeting }}，{{ displayName }}</text>
      <text class="sub">{{ tenantName || "今天也是产能满满的一天" }}</text>
      <view class="portal-entry" @click="goPortalHome">
        <text class="portal-entry-text">返回制衣厂门户 ›</text>
      </view>
      <view v-if="kpis.length" class="kpi-strip">
        <view v-for="k in kpis" :key="k.label" class="kpi-cell">
          <text class="kpi-num">{{ k.value }}</text>
          <text class="kpi-label">{{ k.label }}</text>
        </view>
      </view>
    </view>

    <template v-for="group in visibleGroups" :key="group.name">
      <view class="group-title">{{ group.name }}</view>
      <view class="grid">
        <view
          v-for="item in group.entries"
          :key="item.name"
          class="cell"
          @click="open(item)"
        >
          <text class="cell-icon">{{ item.icon }}</text>
          <text class="cell-name">{{ item.name }}</text>
        </view>
      </view>
    </template>

    <view v-if="!visibleGroups.length" class="empty">
      <text>当前账号没有可用的移动端功能</text>
      <text class="empty-sub">请联系管理员分配角色权限</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { auth } from "../../store/auth";
import { request } from "../../utils/request";

const displayName = computed(
  () => auth.user?.employee_name || auth.user?.login_name || ""
);
/* 品牌（logo + 厂名）已由统一标题栏组件 BrandNavBar 渲染，
 * 这里只保留欢迎语中的厂名。 */
const tenantName = ref("");
async function loadBranding() {
  try {
    const res = await request<{ tenant_name?: string; name?: string }>("/tenant/branding", { silent: true });
    tenantName.value = res.tenant_name || res.name || "";
  } catch {
    tenantName.value = "";
  }
}
function goPortalHome() {
  uni.navigateTo({ url: "/pages/portal/home" });
}
/* 从工作页面退出：确认后调平台注销（吊销服务端会话）并回登录页 */
function onLogout() {
  uni.showModal({
    title: "退出登录",
    content: "退出后将注销当前账号，需要重新登录，确认退出？",
    confirmText: "退出",
    confirmColor: "#dc2626",
    success: (res) => {
      if (res.confirm) void auth.logout();
    },
  });
}
const greeting = computed(() => {
  const h = new Date().getHours();
  if (h < 6) return "夜深了";
  if (h < 12) return "早上好";
  if (h < 14) return "中午好";
  if (h < 18) return "下午好";
  return "晚上好";
});

/* 工作台 KPI：GET /workbench 返回 {metrics:{...}}，失败则隐藏 */
interface WorkbenchResponse {
  metrics?: {
    pending_release?: number | string;
    in_production?: number | string;
    unassigned_tasks?: number | string;
    pending_reviews?: number | string;
    today_wages?: number | string;
  };
}
const kpis = ref<{ label: string; value: string | number }[]>([]);
async function loadKpis() {
  try {
    const res = await request<WorkbenchResponse>("/workbench", { silent: true });
    const m = res.metrics || {};
    const list: { label: string; value: string | number }[] = [];
    if (m.in_production !== undefined) list.push({ label: "生产中工单", value: m.in_production });
    if (m.pending_release !== undefined) list.push({ label: "待下发", value: m.pending_release });
    if (m.unassigned_tasks !== undefined) list.push({ label: "未派工任务", value: m.unassigned_tasks });
    if (m.pending_reviews !== undefined) list.push({ label: "待审批报工", value: m.pending_reviews });
    kpis.value = list.slice(0, 4);
  } catch {
    kpis.value = [];
  }
}

interface Entry {
  icon: string;
  name: string;
  url: string;
  /** 为空表示所有登录用户可见 */
  perm?: string;
  /** 满足任一权限即可见 */
  permAny?: string[];
}

interface Group {
  name: string;
  entries: Entry[];
}

const groups: Group[] = [
  {
    name: "我的常用",
    entries: [
      { icon: "📷", name: "扫码派工/报工", url: "/pages/report/scan", permAny: ["report:create", "task:assign"] },
      { icon: "📝", name: "我的报工", url: "/pages/report/mine", perm: "report:read" },
      { icon: "💰", name: "我的工资", url: "/pages/my/wages" },
      { icon: "🧮", name: "我的计件", url: "/pages/stats/piecework", perm: "payroll:read" },
      { icon: "👤", name: "我的资料", url: "/pages/my/profile" },
      { icon: "📢", name: "生产通知", url: "/pages/notify/list" },
    ],
  },
  {
    name: "生产管理",
    entries: [
      { icon: "📦", name: "项目与工单", url: "/pages/orders/list", perm: "work_order:read" },
      { icon: "✅", name: "任务与派工", url: "/pages/tasks/list", perm: "task:read" },
      { icon: "☑️", name: "报工审批", url: "/pages/report/review", perm: "review:approve" },
      { icon: "▣", name: "质检与库存", url: "/pages/quality/index", perm: "work_order:read" },
      { icon: "🔍", name: "过程巡检", url: "/pages/quality/process-inspections", perm: "work_order:read" },
      { icon: "🖨️", name: "菲票打印", url: "/pages/print/center", perm: "print:read" },
      { icon: "🏷️", name: "扎标签", url: "/pages/print/labels", perm: "work_order:read" },
      { icon: "📥", name: "物料入库", url: "/pages/materials/receipts", perm: "work_order:read" },
      { icon: "📦", name: "物料管理", url: "/pages/masterdata/list?type=materials", perm: "masterdata:read" },
      { icon: "🔁", name: "返工复检", url: "/pages/rework/list", perm: "work_order:read" },
    ],
  },
  {
    name: "分析中心",
    entries: [
      { icon: "📊", name: "产量查询", url: "/pages/stats/output", perm: "statistics:read" },
      { icon: "📋", name: "工资单管理", url: "/pages/payroll/list", perm: "payroll:read" },
      { icon: "📈", name: "进度与统计", url: "/pages/progress/styles", perm: "progress:read" },
    ],
  },
  {
    name: "基础资料",
    entries: [
      { icon: "🧵", name: "产品与工序", url: "/pages/products/list", perm: "product:read" },
      { icon: "📐", name: "唛架管理", url: "/pages/markers/list", perm: "marker:read" },
      { icon: "🏭", name: "车间管理", url: "/pages/settings/workshops", perm: "workshop:read" },
      { icon: "🛠️", name: "产线管理", url: "/pages/settings/lines", perm: "line:read" },
      { icon: "👥", name: "人员与组织", url: "/pages/settings/users", perm: "identity:read" },
      { icon: "🙋", name: "员工与账号", url: "/pages/settings/employees" },
      { icon: "📚", name: "字典维护", url: "/pages/settings/dictionaries", perm: "masterdata:read" },
    ],
  },
  {
    name: "系统管理",
    entries: [
      { icon: "🏢", name: "本厂信息", url: "/pages/settings/profile" },
      { icon: "🌐", name: "门户内容", url: "/pages/settings/portal" },
      { icon: "🔑", name: "用户角色权限", url: "/pages/settings/identity", perm: "identity:read" },
      { icon: "🔐", name: "敏感信息授权申请", url: "/pages/notify/sensitive-requests", perm: "identity:write" },
      { icon: "⚙️", name: "功能设置", url: "/pages/settings/features", perm: "settings:read" },
      { icon: "⇅", name: "导入导出", url: "/pages/settings/import-export", perm: "settings:read" },
      { icon: "🗄️", name: "备份与同步", url: "/pages/settings/backup", perm: "settings:read" },
      { icon: "🛡️", name: "明文备份导出", url: "/pages/settings/plain-backups", perm: "backup:export" },
      { icon: "🧾", name: "审计与字段", url: "/pages/settings/audit", perm: "settings:read" },
    ],
  },
];

const visibleGroups = computed(() =>
  groups
    .map((g) => ({
      name: g.name,
      entries: g.entries.filter((e) => {
        if (e.permAny) return e.permAny.some((p) => auth.hasPerm(p));
        return !e.perm || auth.hasPerm(e.perm);
      }),
    }))
    .filter((g) => g.entries.length > 0)
);

function open(item: Entry) {
  uni.navigateTo({ url: item.url });
}

onMounted(() => {
  if (auth.hasSession) {
    if (!auth.user) auth.fetchMe();
    if (!Object.keys(auth.permissions).length) auth.fetchPermissions();
    loadKpis();
    loadBranding();
  }
});

onShow(() => {
  if (auth.hasSession && !Object.keys(auth.permissions).length) {
    auth.fetchPermissions();
  }
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 24rpx;
  box-sizing: border-box;
}
.hero {
  position: relative;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
  border-radius: 20rpx;
  padding: 36rpx 32rpx;
  margin-bottom: 24rpx;
}
.hero-logout {
  position: absolute;
  top: 24rpx;
  right: 24rpx;
  padding: 8rpx 22rpx;
  border-radius: 999rpx;
  background: rgba(255, 255, 255, 0.22);
  color: #fff;
  font-size: 22rpx;
}
.hello {
  display: block;
  color: #fff;
  font-size: 36rpx;
  font-weight: 600;
}
.sub {
  display: block;
  color: rgba(255, 255, 255, 0.75);
  font-size: 24rpx;
  margin-top: 8rpx;
}
.portal-entry {
  display: inline-flex;
  align-self: flex-start;
  margin-top: 16rpx;
  padding: 8rpx 20rpx;
  border-radius: 999rpx;
  background: rgba(255, 255, 255, 0.18);
}
.portal-entry-text {
  color: #fff;
  font-size: 22rpx;
}
.kpi-strip {
  display: flex;
  margin-top: 28rpx;
  background: rgba(255, 255, 255, 0.14);
  border-radius: 14rpx;
  padding: 20rpx 0;
}
.kpi-cell {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.kpi-num {
  color: #fff;
  font-size: 38rpx;
  font-weight: 700;
}
.kpi-label {
  color: rgba(255, 255, 255, 0.75);
  font-size: 22rpx;
  margin-top: 6rpx;
}
.group-title {
  font-size: 26rpx;
  font-weight: 700;
  color: #17255f;
  margin: 28rpx 0 16rpx;
}
.grid {
  display: flex;
  flex-wrap: wrap;
  gap: 20rpx;
}
.cell {
  width: calc((100% - 40rpx) / 3);
  background: #fff;
  border-radius: 16rpx;
  padding: 28rpx 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  box-shadow: 0 4rpx 16rpx rgba(23, 37, 95, 0.04);
}
.cell-icon {
  font-size: 44rpx;
  margin-bottom: 12rpx;
}
.cell-name {
  font-size: 24rpx;
  color: #334155;
}
.empty {
  text-align: center;
  padding: 120rpx 0;
  color: #a0aec0;
  display: flex;
  flex-direction: column;
  gap: 12rpx;
}
.empty-sub {
  font-size: 24rpx;
}
</style>
