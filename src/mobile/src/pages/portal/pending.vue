<template>
  <view class="page">
    <view class="card status-card">
      <view class="brand">
        <view class="brand-mark">智</view>
        <text class="brand-name">一智算 · 免费体验</text>
      </view>
      <text class="status-icon">{{ rejected ? "✕" : approved ? "✓" : "⏳" }}</text>
      <text class="status-title">{{ statusTitle }}</text>
      <text class="status-desc">{{ statusDesc }}</text>

      <view class="steps">
        <view class="step done"><text class="step-dot">✓</text><text>提交体验注册申请</text></view>
        <view class="step" :class="{ now: !approved && !rejected }">
          <text class="step-dot">{{ rejected ? "✕" : approved ? "✓" : "⏳" }}</text>
          <text>{{ rejected ? "平台管理员审核未通过" : `平台管理员审核${expedited ? "（已加急，优先处理）" : ""}` }}</text>
        </view>
        <view v-if="!rejected" class="step" :class="{ now: approved }"><text class="step-dot">3</text><text>审核通过 → 进入工作台</text></view>
      </view>

      <button v-if="approved" class="btn solid" @click="enter">开始体验，进入工作台</button>
      <template v-else-if="rejected">
        <button class="btn solid" @click="goTrial">重新提交注册申请</button>
      </template>
      <template v-else>
        <button v-if="!expedited" class="btn solid" :disabled="submitting" @click="expedite">
          {{ submitting ? "正在提交…" : "🔥 加急申请" }}
        </button>
        <view v-else class="expedited">🔥 已加急，平台管理员将优先处理</view>
        <button class="btn ghost" @click="infoOpen = !infoOpen">
          {{ infoOpen ? "收起注册信息" : "查看/修改注册信息" }}
        </button>
      </template>

      <text v-if="rejected" class="tip">注册申请被驳回后该体验厂不会启用，也不会出现在手机登录的制衣厂列表中；可修正资料后重新提交。</text>
      <text v-else class="tip">审批通过后回到本页点击「开始体验」即可进入一智算工作台；本页可随时查看审批状态。</text>
    </view>

    <!-- 注册信息表单 -->
    <view v-if="infoOpen" class="card info-card">
      <text class="card-title">注册信息</text>
      <view v-if="loading" class="loading">正在加载注册信息…</view>
      <template v-else>
        <view class="field">
          <text class="label required">制衣厂名称</text>
          <input v-model.trim="info.name" maxlength="60" placeholder="与营业执照一致的厂名" />
        </view>
        <view class="field">
          <text class="label">所在国 / 地区</text>
          <input v-model.trim="info.region" maxlength="40" placeholder="如 中国广东 / 越南" />
        </view>
        <view class="field">
          <text class="label">厂规模</text>
          <picker :range="scaleLabels" :value="scaleIndex" @change="onScaleChange">
            <view class="picker-value">{{ scaleLabels[scaleIndex] }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="label">联系人</text>
          <input v-model.trim="info.contact_name" maxlength="30" />
        </view>
        <view class="field">
          <text class="label">联系电话（体验用户登录手机号，不可修改）</text>
          <input :value="maskedPhone" disabled />
        </view>
        <view class="field">
          <text class="label">微信号</text>
          <input v-model.trim="info.contact_wechat" maxlength="64" placeholder="选填" />
        </view>
        <view class="field">
          <text class="label">QQ</text>
          <input v-model.trim="info.contact_qq" maxlength="32" placeholder="选填" />
        </view>
        <view class="field">
          <text class="label">支付宝账号</text>
          <input v-model.trim="info.contact_alipay" maxlength="64" placeholder="选填" />
        </view>
        <view class="field">
          <text class="label">厂地址</text>
          <input v-model.trim="info.address" maxlength="120" />
        </view>
        <view class="field">
          <text class="label">备注</text>
          <textarea v-model.trim="info.remark" maxlength="120" placeholder="主营品类、车间与产线数量等（可选）" class="remark" />
        </view>
        <view v-if="infoError" class="error">{{ infoError }}</view>
        <button class="btn solid" :disabled="saving || !info.name" @click="saveInfo">
          {{ saving ? "正在保存…" : "保存修改" }}
        </button>
        <text class="small">平台管理员审批时将看到最新内容；联系电话不可修改（以体验用户登录手机号为准）。</text>
      </template>
    </view>

    <view v-if="!session" class="card">
      <text class="empty">暂无体验会话，请先完成体验注册</text>
      <button class="btn ghost" @click="goPortal">返回平台门户</button>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request, setToken } from "../../utils/request";
import { auth } from "../../store/auth";

interface TrialSession {
  entry_token: string;
  tenant_code: string;
  name: string;
  phone?: string;
}

interface TrialInfo {
  code: string;
  name: string;
  region: string;
  scale: string;
  contact_name: string;
  contact_phone: string;
  contact_wechat: string;
  contact_qq: string;
  contact_alipay: string;
  address: string;
  remark: string;
  registration_status: string;
  expedited: boolean;
}

const SESSION_KEY = "clothp_trial_entry";
const scaleLabels = ["请选择（可选）", "微型（10 人以下）", "小型（10-50 人）", "中型（50-200 人）", "大型（200 人以上）"];
const scaleValues = ["", "micro", "small", "medium", "large"];

const session = ref<TrialSession | null>(null);
const infoOpen = ref(false);
const loading = ref(false);
const saving = ref(false);
const submitting = ref(false);
const infoError = ref("");
const approved = ref(false);
const rejected = ref(false);
const expedited = ref(false);
const scaleIndex = ref(0);
const info = ref({
  name: "",
  region: "",
  scale: "",
  contact_name: "",
  contact_phone: "",
  contact_wechat: "",
  contact_qq: "",
  contact_alipay: "",
  address: "",
  remark: "",
});

function mask(p: string): string {
  return p && p.length === 11 ? `${p.slice(0, 3)}****${p.slice(7)}` : p;
}
const maskedPhone = computed(() => mask(info.value.contact_phone || session.value?.phone || ""));
const statusTitle = computed(() =>
  rejected.value ? "体验注册申请未通过" : approved.value ? "体验厂已通过审核" : "体验注册已提交，正在审批"
);
const statusDesc = computed(() => {
  const base = `体验制衣厂 ${session.value?.name || ""}（${session.value?.tenant_code || ""}）`;
  if (rejected.value) return `${base}的注册申请已被平台管理员驳回，该厂不会启用，也不会出现在手机登录的制衣厂列表中。`;
  if (approved.value) return `${base}已通过审核，以手机号 ${maskedPhone.value} 作为该厂管理员账户开通，并开始计算 30 天体验期。`;
  return `${base}已提交，平台管理员审核通过后，将以手机号 ${maskedPhone.value} 作为该厂管理员账户开通，并开始计算 30 天体验期。`;
});

function onScaleChange(e: { detail: { value: number } }) {
  scaleIndex.value = e.detail.value;
  info.value.scale = scaleValues[e.detail.value];
}

async function loadInfo() {
  const s = session.value;
  if (!s?.entry_token || !s?.tenant_code) return;
  loading.value = true;
  try {
    const r = await request<TrialInfo>(
      `/public/trial-info?entry_token=${encodeURIComponent(s.entry_token)}&tenant_code=${encodeURIComponent(s.tenant_code)}`,
      { silent: true, skipAuthRedirect: true }
    );
    info.value = {
      name: r.name || "",
      region: r.region || "",
      scale: r.scale || "",
      contact_name: r.contact_name || "",
      contact_phone: r.contact_phone || "",
      contact_wechat: r.contact_wechat || "",
      contact_qq: r.contact_qq || "",
      contact_alipay: r.contact_alipay || "",
      address: r.address || "",
      remark: r.remark || "",
    };
    approved.value = r.registration_status === "approved";
    rejected.value = r.registration_status === "rejected";
    expedited.value = !!r.expedited;
    if (rejected.value) infoOpen.value = false;
    scaleIndex.value = Math.max(0, scaleValues.indexOf(r.scale || ""));
    session.value = { ...s, name: r.name || s.name };
  } catch (e) {
    infoError.value = (e as Error).message || "注册信息加载失败";
  } finally {
    loading.value = false;
  }
}

async function saveInfo() {
  const s = session.value;
  if (!s?.entry_token || !s?.tenant_code) {
    infoError.value = "体验凭证已失效，请重新登录";
    return;
  }
  if (!info.value.name) {
    infoError.value = "请填写制衣厂名称";
    return;
  }
  saving.value = true;
  infoError.value = "";
  try {
    await request("/public/trial-update", {
      method: "POST",
      data: {
        entry_token: s.entry_token,
        tenant_code: s.tenant_code,
        name: info.value.name,
        region: info.value.region,
        scale: info.value.scale,
        contact_name: info.value.contact_name,
        contact_wechat: info.value.contact_wechat,
        contact_qq: info.value.contact_qq,
        contact_alipay: info.value.contact_alipay,
        address: info.value.address,
        remark: info.value.remark,
      },
      silent: true,
      skipAuthRedirect: true,
    });
    uni.showToast({ title: "注册信息已更新", icon: "none" });
    infoOpen.value = false;
    await loadInfo();
  } catch (e) {
    infoError.value = (e as Error).message || "保存失败，请稍后重试";
  } finally {
    saving.value = false;
  }
}

async function expedite() {
  const s = session.value;
  if (!s?.entry_token || !s?.tenant_code) return;
  submitting.value = true;
  try {
    await request("/public/trial-expedite", {
      method: "POST",
      data: { entry_token: s.entry_token, tenant_code: s.tenant_code },
      silent: true,
      skipAuthRedirect: true,
    });
    expedited.value = true;
    uni.showToast({ title: "加急申请已提交", icon: "none" });
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加急申请失败", icon: "none" });
  } finally {
    submitting.value = false;
  }
}

async function enter() {
  const s = session.value;
  if (!s?.entry_token) return;
  uni.showLoading({ title: "进入工作台…", mask: true });
  try {
    const res = await request<{ access_token: string; entry_token?: string }>("/public/trial-enter", {
      method: "POST",
      data: { entry_token: s.entry_token, node_id: "mobile-app" },
      silent: true,
      skipAuthRedirect: true,
    });
    setToken(res.access_token);
    if (res.entry_token) {
      uni.setStorageSync(SESSION_KEY, { ...s, entry_token: res.entry_token });
    }
    await Promise.all([auth.fetchMe(), auth.fetchPermissions()]);
    /* 与网页版一致：先进入「制衣厂门户」，再由门户进工作台 */
    uni.reLaunch({ url: "/pages/portal/home" });
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "进入失败", icon: "none" });
  } finally {
    uni.hideLoading();
  }
}

function goPortal() {
  uni.reLaunch({ url: "/pages/portal/platform" });
}
function goTrial() {
  uni.removeStorageSync(SESSION_KEY);
  uni.reLaunch({ url: "/pages/portal/trial" });
}

onShow(() => {
  session.value = uni.getStorageSync(SESSION_KEY) || null;
  if (session.value) loadInfo();
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: linear-gradient(160deg, #eaf3ff 0%, #f5f9ff 45%, #eef8fc 100%);
  padding: 32rpx;
  box-sizing: border-box;
}
.card {
  background: #fff;
  border: 1px solid #e3ecfb;
  border-radius: 20rpx;
  box-shadow: 0 12rpx 32rpx rgba(23, 37, 95, 0.1);
  padding: 40rpx 32rpx;
  margin-bottom: 24rpx;
}
.status-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}
.brand {
  display: flex;
  align-items: center;
  gap: 12rpx;
  margin-bottom: 24rpx;
}
.brand-mark {
  width: 60rpx;
  height: 60rpx;
  border-radius: 16rpx;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
  color: #fff;
  font-size: 28rpx;
  display: flex;
  align-items: center;
  justify-content: center;
}
.brand-name {
  font-size: 26rpx;
  color: #41537e;
  font-weight: 600;
}
.status-icon {
  width: 128rpx;
  height: 128rpx;
  border-radius: 999rpx;
  background: #eff6ff;
  border: 2px solid #bfdbfe;
  font-size: 56rpx;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 8rpx 0 20rpx;
}
.status-title {
  font-size: 36rpx;
  font-weight: 700;
  color: #17255f;
}
.status-desc {
  font-size: 26rpx;
  line-height: 1.8;
  color: #475569;
  margin-top: 16rpx;
}
.steps {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 16rpx;
  margin: 32rpx 0;
  text-align: left;
}
.step {
  display: flex;
  align-items: center;
  gap: 16rpx;
  font-size: 26rpx;
  color: #94a3b8;
}
.step-dot {
  width: 48rpx;
  height: 48rpx;
  border-radius: 999rpx;
  background: #f1f5f9;
  color: #94a3b8;
  font-size: 22rpx;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.step.done {
  color: #059669;
}
.step.done .step-dot {
  background: #d1fae5;
  color: #059669;
}
.step.now {
  color: #1d4ed8;
  font-weight: 600;
}
.step.now .step-dot {
  background: #dbeafe;
  color: #1d4ed8;
}
.expedited {
  background: #fef3c7;
  color: #b45309;
  font-size: 24rpx;
  font-weight: 600;
  border-radius: 999rpx;
  padding: 16rpx 32rpx;
  margin: 16rpx 0;
}
.tip {
  font-size: 22rpx;
  color: #94a3b8;
  line-height: 1.7;
  margin-top: 28rpx;
}
.btn {
  width: 100%;
  border-radius: 999rpx;
  font-size: 28rpx;
  height: 84rpx;
  line-height: 84rpx;
  margin-top: 16rpx;
}
.btn.solid {
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  color: #fff;
  box-shadow: 0 8rpx 20rpx rgba(37, 99, 235, 0.25);
}
.btn.solid[disabled] {
  opacity: 0.6;
}
.btn.ghost {
  background: #fff;
  color: #475569;
  border: 1px solid #cbd5e1;
}
.info-card {
  text-align: left;
}
.card-title {
  display: block;
  font-size: 30rpx;
  font-weight: 700;
  color: #17255f;
  border-bottom: 1px dashed #dbeafe;
  padding-bottom: 20rpx;
}
.loading,
.empty {
  display: block;
  text-align: center;
  font-size: 24rpx;
  color: #94a3b8;
  padding: 24rpx 0;
}
.field {
  padding: 16rpx 0;
}
.label {
  display: block;
  font-size: 24rpx;
  color: #475569;
  margin-bottom: 10rpx;
}
.label.required::after {
  content: " *";
  color: #d92d20;
}
input,
.remark {
  border: 1px solid #cbd5e1;
  border-radius: 8rpx;
  padding: 16rpx 20rpx;
  font-size: 26rpx;
  line-height: 44rpx;
  min-height: 80rpx;
  color: #0f172a;
  background: #fff;
  width: 100%;
  box-sizing: border-box;
}
.remark {
  height: 140rpx;
}
.picker-value {
  border: 1px solid #cbd5e1;
  border-radius: 8rpx;
  padding: 16rpx 20rpx;
  font-size: 26rpx;
  line-height: 44rpx;
  min-height: 80rpx;
  color: #0f172a;
  background: #fff;
  box-sizing: border-box;
}
.error {
  color: #d92d20;
  font-size: 24rpx;
  margin: 12rpx 0;
}
.small {
  display: block;
  font-size: 22rpx;
  color: #94a3b8;
  margin-top: 16rpx;
  line-height: 1.6;
}
</style>
