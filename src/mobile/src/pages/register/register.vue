<template>
  <view class="page">
    <!-- 提交成功态 -->
    <view v-if="submitted" class="success-card">
      <text class="success-icon">✅</text>
      <text class="success-title">申请已提交</text>
      <text class="success-msg">{{ resultMessage }}</text>
      <view class="result-box">
        <view class="result-row">
          <text class="result-label">工厂名称</text>
          <text class="result-value">{{ result?.name }}</text>
        </view>
        <view class="result-row" v-if="result?.code">
          <text class="result-label">工厂编码</text>
          <text class="result-value">{{ result.code }}</text>
        </view>
        <view class="result-row">
          <text class="result-label">当前状态</text>
          <text class="result-value status">{{ statusLabel(result?.status) }}</text>
        </view>
      </view>
      <text class="success-tip">平台审核通过后即可登录使用，请留意审核结果。</text>
      <button class="btn primary" @click="goLogin">返回登录</button>
      <button class="btn ghost" @click="resetForm">再申请一家</button>
    </view>

    <!-- 表单态 -->
    <template v-else>
      <view class="intro">
        <text class="intro-title">制衣厂入驻申请</text>
        <text class="intro-sub">填写以下信息，平台将在 1-3 个工作日内完成审核</text>
      </view>

      <view class="card">
        <view class="field">
          <text class="label required">工厂名称</text>
          <input v-model.trim="form.name" placeholder="如：XX制衣有限公司" maxlength="64" />
        </view>
        <view class="field">
          <text class="label">工厂编码</text>
          <input v-model.trim="form.code" placeholder="选填，2-32位字母数字（如 XXFZ）" maxlength="32" />
        </view>
        <view class="row2">
          <view class="field half">
            <text class="label">所在地区</text>
            <input v-model.trim="form.region" placeholder="如：广东" maxlength="32" />
          </view>
          <view class="field half">
            <text class="label">工厂规模</text>
            <picker :range="scaleLabels" @change="onScaleChange">
              <view class="picker-value">{{ scaleLabels[scaleIndex] }}</view>
            </picker>
          </view>
        </view>
      </view>

      <view class="card">
        <view class="field">
          <text class="label required">联系人</text>
          <input v-model.trim="form.contact_name" placeholder="联系人姓名" maxlength="32" />
        </view>
        <view class="field">
          <text class="label required">手机号</text>
          <input
            v-model.trim="form.contact_phone"
            type="number"
            maxlength="11"
            placeholder="用于接收验证码和审核通知"
          />
        </view>
        <view class="field code-field">
          <text class="label required">短信验证码</text>
          <view class="code-row">
            <input v-model.trim="form.sms_code" type="number" maxlength="6" placeholder="6位验证码" class="code-input" />
            <button
              class="send-btn"
              :disabled="countdown > 0 || sending"
              @click="sendCode"
            >
              {{ countdown > 0 ? `${countdown}s` : sending ? "发送中…" : "获取验证码" }}
            </button>
          </view>
          <text v-if="demoCode" class="demo-tip">{{ demoTip }}</text>
        </view>
      </view>

      <view class="card">
        <view class="field">
          <text class="label">微信号</text>
          <input v-model.trim="form.contact_wechat" placeholder="选填" maxlength="64" />
        </view>
        <view class="field">
          <text class="label">QQ 号</text>
          <input v-model.trim="form.contact_qq" placeholder="选填" maxlength="32" />
        </view>
        <view class="field">
          <text class="label">详细地址</text>
          <input v-model.trim="form.address" placeholder="选填" maxlength="128" />
        </view>
        <view class="field">
          <text class="label">备注</text>
          <textarea v-model.trim="form.remark" placeholder="选填，可填写主营品类、设备数量等" maxlength="200" class="remark" />
        </view>
      </view>

      <view v-if="error" class="error">{{ error }}</view>
      <button class="btn primary submit" :disabled="loading" @click="submit">
        {{ loading ? "提交中…" : "提交入驻申请" }}
      </button>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, onUnmounted } from "vue";
import { request } from "../../utils/request";

interface RegisterResult {
  id: string;
  code: string;
  name: string;
  status: string;
  message: string;
}

interface SendCodeResult {
  expires_in: number;
  sms_sent: boolean;
  demo_code?: string;
  message?: string;
}

const scaleLabels = ["微型（10人以下）", "小型（10-50人）", "中型（50-200人）", "大型（200人以上）"];
const scaleValues = ["micro", "small", "medium", "large"];

const form = ref({
  name: "",
  code: "",
  region: "",
  scale: "small",
  contact_name: "",
  contact_phone: "",
  contact_wechat: "",
  contact_qq: "",
  sms_code: "",
  address: "",
  remark: "",
});

const scaleIndex = ref(1);
const sending = ref(false);
const countdown = ref(0);
const loading = ref(false);
const error = ref("");
const submitted = ref(false);
const result = ref<RegisterResult | null>(null);
const resultMessage = ref("");
const demoCode = ref("");
let timer: ReturnType<typeof setInterval> | null = null;

function onScaleChange(e: { detail: { value: number } }) {
  scaleIndex.value = e.detail.value;
  form.value.scale = scaleValues[e.detail.value];
}

function statusLabel(s?: string): string {
  switch (s) {
    case "pending":
      return "待平台审核";
    case "approved":
      return "审核通过";
    case "rejected":
      return "审核未通过";
    default:
      return s || "待平台审核";
  }
}

function demoTip(): string {
  return demoCode.value
    ? `当前为演示模式，验证码：${demoCode.value}`
    : "";
}

async function sendCode() {
  error.value = "";
  const phone = form.value.contact_phone;
  if (!/^1\d{10}$/.test(phone)) {
    error.value = "请先填写正确的 11 位手机号";
    return;
  }
  sending.value = true;
  try {
    const res = await request<SendCodeResult>("/public/tenant-register/send-code", {
      method: "POST",
      data: { phone },
      silent: true,
      skipAuthRedirect: true,
    });
    if (res.demo_code) {
      demoCode.value = res.demo_code;
      form.value.sms_code = res.demo_code;
    }
    countdown.value = 60;
    if (timer) clearInterval(timer);
    timer = setInterval(() => {
      countdown.value -= 1;
      if (countdown.value <= 0) {
        countdown.value = 0;
        if (timer) clearInterval(timer);
        timer = null;
      }
    }, 1000);
    uni.showToast({ title: "验证码已发送", icon: "none" });
  } catch (e) {
    error.value = (e as Error).message || "验证码发送失败";
  } finally {
    sending.value = false;
  }
}

async function submit() {
  error.value = "";
  const f = form.value;
  if (!f.name || f.name.length < 2) {
    error.value = "请填写工厂名称（至少 2 个字）";
    return;
  }
  if (!f.contact_name) {
    error.value = "请填写联系人姓名";
    return;
  }
  if (!/^1\d{10}$/.test(f.contact_phone)) {
    error.value = "请填写正确的 11 位手机号";
    return;
  }
  if (!f.sms_code) {
    error.value = "请填写短信验证码";
    return;
  }
  if (f.code && !/^[A-Za-z0-9_-]{2,32}$/.test(f.code)) {
    error.value = "工厂编码需为 2-32 位字母、数字、下划线或短横线";
    return;
  }
  loading.value = true;
  try {
    const res = await request<RegisterResult>("/public/tenant-register", {
      method: "POST",
      data: { ...f },
      silent: true,
      skipAuthRedirect: true,
      idempotency: true,
    });
    result.value = res;
    resultMessage.value = res.message || "注册申请已提交，等待平台审核";
    submitted.value = true;
    // 记录最近一次申请，便于后续查看
    uni.setStorageSync("clothp_last_registration", {
      name: res.name,
      code: res.code,
      status: res.status,
      phone: f.contact_phone,
      time: Date.now(),
    });
  } catch (e) {
    error.value = (e as Error).message || "提交失败，请稍后重试";
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  submitted.value = false;
  result.value = null;
  form.value.sms_code = "";
  demoCode.value = "";
}

function goLogin() {
  uni.reLaunch({ url: "/pages/portal/platform?tab=login&method=account" });
}

onUnmounted(() => {
  if (timer) clearInterval(timer);
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 32rpx;
  box-sizing: border-box;
}
.intro {
  margin-bottom: 32rpx;
}
.intro-title {
  display: block;
  font-size: 40rpx;
  font-weight: 700;
  color: #2d3748;
}
.intro-sub {
  display: block;
  font-size: 24rpx;
  color: #718096;
  margin-top: 12rpx;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 16rpx 28rpx;
  margin-bottom: 24rpx;
}
.field {
  padding: 20rpx 0;
  border-bottom: 1px solid #edf2f7;
}
.field:last-child {
  border-bottom: none;
}
.label {
  display: block;
  font-size: 24rpx;
  color: #718096;
  margin-bottom: 12rpx;
}
.label.required::before {
  content: "*";
  color: #e53e3e;
  margin-right: 6rpx;
}
.row2 {
  display: flex;
  gap: 24rpx;
}
.half {
  flex: 1;
}
.field input {
  min-height: 56rpx;
  line-height: 44rpx;
  font-size: 28rpx;
  color: #2d3748;
}
.picker-value {
  font-size: 28rpx;
  line-height: 44rpx;
  min-height: 56rpx;
  color: #2d3748;
  box-sizing: border-box;
}
.code-row {
  display: flex;
  align-items: center;
  gap: 16rpx;
}
.code-input {
  flex: 1;
  border: 1px solid #e2e8f0;
  border-radius: 10rpx;
  padding: 0 24rpx;
  min-height: 80rpx;
  line-height: 44rpx;
  box-sizing: border-box;
}
.send-btn {
  flex-shrink: 0;
  margin: 0;
  font-size: 24rpx;
  background: #ebf4ff;
  color: #2b6cb0;
  border-radius: 10rpx;
  padding: 0 24rpx;
  height: 80rpx;
  line-height: 80rpx;
}
.send-btn[disabled] {
  opacity: 0.5;
}
.demo-tip {
  display: block;
  font-size: 22rpx;
  color: #dd6b20;
  margin-top: 12rpx;
}
.remark {
  width: 100%;
  height: 140rpx;
  font-size: 28rpx;
}
.error {
  color: #e53e3e;
  font-size: 26rpx;
  margin-bottom: 24rpx;
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
  margin-top: 20rpx;
}
.submit {
  margin-top: 8rpx;
}
.submit[disabled] {
  opacity: 0.6;
}
.success-card {
  background: #fff;
  border-radius: 20rpx;
  padding: 64rpx 40rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.success-icon {
  font-size: 80rpx;
  margin-bottom: 24rpx;
}
.success-title {
  font-size: 40rpx;
  font-weight: 700;
  color: #2d3748;
}
.success-msg {
  font-size: 26rpx;
  color: #718096;
  margin-top: 16rpx;
  text-align: center;
}
.result-box {
  width: 100%;
  background: #f7fafc;
  border-radius: 12rpx;
  padding: 16rpx 24rpx;
  margin-top: 32rpx;
}
.result-row {
  display: flex;
  justify-content: space-between;
  padding: 16rpx 0;
}
.result-label {
  font-size: 26rpx;
  color: #718096;
}
.result-value {
  font-size: 26rpx;
  color: #2d3748;
  font-weight: 500;
}
.result-value.status {
  color: #dd6b20;
}
.success-tip {
  font-size: 24rpx;
  color: #a0aec0;
  margin-top: 24rpx;
  text-align: center;
}
.success-card .btn.primary {
  width: 100%;
  margin-top: 48rpx;
}
.success-card .btn.ghost {
  width: 100%;
}
</style>
