<template>
  <view class="page">
    <view class="intro">
      <text class="intro-title">制衣厂注册</text>
      <text class="intro-sub">选择注册类型；已提交的申请在未审批前可继续修改</text>
    </view>

    <!-- 手机号验证：查询本人名下所有注册申请 -->
    <view class="card">
      <text class="section-title">手机号验证</text>
      <view class="field"><text class="label required">手机号</text><input v-model.trim="phone" class="input" type="number" maxlength="11" placeholder="11 位手机号" /></view>
      <view class="field">
        <text class="label required">短信验证码</text>
        <view class="row">
          <input v-model.trim="smsCode" class="input grow" type="number" maxlength="6" placeholder="6 位验证码" />
          <button class="btn btn-outline btn-sm" :disabled="sending" @click="sendCode">{{ sending ? "发送中" : countdown > 0 ? countdown + "s" : "获取验证码" }}</button>
        </view>
      </view>
      <text v-if="demoCode" class="muted">演示验证码：{{ demoCode }}</text>
      <text v-if="error" class="error-text">{{ error }}</text>
      <button class="btn btn-solid btn-block" :disabled="looking" @click="lookup">{{ looking ? "查询中…" : "查询我的注册申请" }}</button>
    </view>

    <!-- 多家申请切换，默认打开第一家 -->
    <view v-if="applications.length" class="card">
      <text class="section-title">已有注册申请（{{ applications.length }}）</text>
      <view class="chips">
        <view v-for="a in applications" :key="a.id" class="chip" :class="{ on: currentId === a.id }" @click="selectApplication(a)">{{ a.name || a.code }}</view>
      </view>
      <text class="muted">{{ currentCode }} · {{ current.customer_type === "trial" ? "体验" : "付费" }} · {{ statusLabel(current.registration_status) }}</text>
      <text v-if="current.registration_status !== 'pending'" class="muted">该申请已审批处理，信息不可再修改。</text>
      <text v-else class="muted">待审批，可直接在下方修改并保存。</text>
    </view>

    <!-- 注册信息：新建时可选类型，修改时回填 -->
    <view class="card">
      <text class="section-title">{{ isEditing ? "修改注册信息" : "填写注册信息" }}</text>
      <view v-if="!isEditing" class="chips">
        <view class="chip" :class="{ on: applyType === 'trial' }" @click="applyType = 'trial'">体验注册</view>
        <view class="chip" :class="{ on: applyType === 'paid' }" @click="applyType = 'paid'">付费注册</view>
      </view>
      <view v-else class="muted">注册类型：{{ current.customer_type === "trial" ? "体验" : "付费" }}（提交后不可更改）</view>

      <view class="field"><text class="label required">制衣厂名称</text><input v-model.trim="form.name" class="input" maxlength="64" placeholder="如：XX 制衣有限公司" /></view>
      <view v-if="!isEditing && applyType === 'paid'" class="field"><text class="label">厂编码</text><input v-model.trim="form.code" class="input" placeholder="留空自动生成" /></view>
      <view class="field"><text class="label">所在地区</text><input v-model.trim="form.region" class="input" placeholder="如：广西南宁" /></view>
      <view class="field">
        <text class="label">厂规模</text>
        <picker :range="scaleLabels" @change="onScale"><view class="input picker-value">{{ scaleText }}</view></picker>
      </view>
      <view class="field"><text class="label required">联系人</text><input v-model.trim="form.contact_name" class="input" placeholder="姓名" /></view>
      <view class="field"><text class="label">微信号</text><input v-model.trim="form.contact_wechat" class="input" placeholder="选填" /></view>
      <view class="field"><text class="label">QQ</text><input v-model.trim="form.contact_qq" class="input" placeholder="选填" /></view>
      <view class="field"><text class="label">支付宝</text><input v-model.trim="form.contact_alipay" class="input" placeholder="选填" /></view>
      <view class="field"><text class="label">地址</text><input v-model.trim="form.address" class="input" placeholder="选填" /></view>
      <view class="field"><text class="label">备注</text><input v-model.trim="form.remark" class="input" placeholder="选填" /></view>

      <button class="btn btn-solid btn-block" :disabled="saving" @click="submit">{{ saving ? "保存中…" : isEditing ? "保存修改" : "提交注册" }}</button>
      <button v-if="isEditing" class="btn btn-outline btn-block" @click="newApplication">新建另一家制衣厂</button>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Application {
  id: string; code: string; name: string; short_code: string
  contact_name: string; contact_phone: string; contact_wechat: string; contact_qq: string
  contact_alipay: string; address: string; region: string; scale: string; remark: string
  registration_status: string; customer_type: string; status: string; created_at: number
}

const phone = ref("");
const smsCode = ref("");
const demoCode = ref("");
const sending = ref(false);
const looking = ref(false);
const saving = ref(false);
const countdown = ref(0);
const error = ref("");
const applyType = ref<"trial" | "paid">("trial");
const applications = ref<Application[]>([]);
const currentId = ref("");
const updateToken = ref("");
const isEditing = ref(false);

const scaleOptions = [
  { label: "请选择", value: "" }, { label: "微型", value: "micro" },
  { label: "小型", value: "small" }, { label: "中型", value: "medium" }, { label: "大型", value: "large" },
];
const scaleLabels = scaleOptions.map((s) => s.label);
const scaleText = computed(() => scaleOptions.find((s) => s.value === form.scale)?.label || "请选择");
const form = reactive({
  name: "", code: "", region: "", scale: "", contact_name: "",
  contact_wechat: "", contact_qq: "", contact_alipay: "", address: "", remark: "",
});
const current = computed<Application>(() => applications.value.find((a) => a.id === currentId.value) || ({} as Application));
const currentCode = computed(() => current.value.code || "—");
function statusLabel(status: string) {
  return ({ pending: "待审批", approved: "已启用", rejected: "已驳回" } as Record<string, string>)[status] || status || "—";
}
function toast(title: string) { uni.showToast({ title, icon: "none" }); }
function onScale(e: any) { form.scale = scaleOptions[Number(e.detail.value)]?.value || ""; }

let timer: ReturnType<typeof setInterval> | undefined;
function startCountdown(seconds: number) {
  countdown.value = seconds;
  if (timer) clearInterval(timer);
  timer = setInterval(() => {
    countdown.value -= 1;
    if (countdown.value <= 0 && timer) clearInterval(timer);
  }, 1000);
}
onUnmounted(() => { if (timer) clearInterval(timer); });

async function sendCode() {
  error.value = "";
  if (!/^1\d{10}$/.test(phone.value)) { error.value = "请填写正确的 11 位手机号"; return; }
  sending.value = true;
  try {
    const res = await request<{ demo_code?: string; expires_in?: number }>("/public/tenant-register/send-code", {
      method: "POST", data: { phone: phone.value }, silent: true, skipAuthRedirect: true,
    });
    demoCode.value = res.demo_code || "";
    startCountdown(res.expires_in || 60);
    toast("验证码已发送");
  } catch (e) { error.value = (e as Error).message || "发送失败"; } finally { sending.value = false; }
}

async function lookup() {
  error.value = "";
  if (!/^1\d{10}$/.test(phone.value)) { error.value = "请填写正确的 11 位手机号"; return; }
  if (!smsCode.value) { error.value = "请填写短信验证码"; return; }
  looking.value = true;
  try {
    const res = await request<{ items: Application[]; update_token: string }>("/public/registration-lookup", {
      method: "POST", data: { phone: phone.value, sms_code: smsCode.value }, silent: true, skipAuthRedirect: true,
    });
    applications.value = res.items || [];
    updateToken.value = res.update_token || "";
    if (applications.value.length) selectApplication(applications.value[0]);
    else { isEditing.value = false; toast("暂无注册申请，请填写信息提交"); }
  } catch (e) { error.value = (e as Error).message || "查询失败"; } finally { looking.value = false; }
}

function fillForm(a: Application) {
  Object.assign(form, {
    name: a.name || "", code: "", region: a.region || "", scale: a.scale || "",
    contact_name: a.contact_name || "", contact_wechat: a.contact_wechat || "",
    contact_qq: a.contact_qq || "", contact_alipay: a.contact_alipay || "",
    address: a.address || "", remark: a.remark || "",
  });
}
function selectApplication(a: Application) {
  currentId.value = a.id;
  applyType.value = a.customer_type === "trial" ? "trial" : "paid";
  isEditing.value = true;
  fillForm(a);
}
function newApplication() {
  currentId.value = "";
  isEditing.value = false;
  applyType.value = "trial";
  Object.assign(form, { name: "", code: "", region: "", scale: "", contact_name: "", contact_wechat: "", contact_qq: "", contact_alipay: "", address: "", remark: "" });
}

async function submit() {
  error.value = "";
  if (!form.name || form.name.length < 2) { error.value = "制衣厂名称至少 2 个字符"; return; }
  if (!form.contact_name) { error.value = "请填写联系人"; return; }
  saving.value = true;
  try {
    if (isEditing.value) {
      if (current.value.registration_status !== "pending") { error.value = "该申请已审批，不可修改"; return; }
      await request("/public/registration-update", {
        method: "POST",
        data: { update_token: updateToken.value, id: currentId.value, ...form },
        silent: true, skipAuthRedirect: true,
      });
      applications.value = applications.value.map((a) => a.id === currentId.value ? { ...a, ...form } : a);
      toast("注册信息已更新");
    } else {
      if (!smsCode.value) { error.value = "请获取并填写短信验证码"; return; }
      const body = { ...form, contact_phone: phone.value, sms_code: smsCode.value };
      const path = applyType.value === "trial" ? "/public/trial-register" : "/public/tenant-register";
      await request(path, { method: "POST", data: body, silent: true, skipAuthRedirect: true });
      smsCode.value = "";
      toast("注册申请已提交，可在未审批前查询并修改");
    }
  } catch (e) { error.value = (e as Error).message || "保存失败"; } finally { saving.value = false; }
}

onLoad((query) => {
  const q = (query as Record<string, string>) || {};
  if (q.phone) phone.value = q.phone;
});
</script>

<style scoped>
.page { padding: 24rpx; }
.intro { margin-bottom: 24rpx; }
.intro-title { display: block; font-size: 36rpx; font-weight: 700; color: #17255f; }
.intro-sub { display: block; font-size: 24rpx; color: #64748b; margin-top: 8rpx; }
.card { background: #fff; border-radius: 16rpx; padding: 24rpx; margin-bottom: 20rpx; }
.section-title { display: block; font-size: 28rpx; font-weight: 700; color: #17255f; margin-bottom: 16rpx; }
.field { margin-bottom: 16rpx; }
.label { display: block; font-size: 24rpx; color: #41537e; margin-bottom: 8rpx; }
.label.required::after { content: " *"; color: #d92d20; }
.input { width: 100%; box-sizing: border-box; border: 1rpx solid #c7dcff; border-radius: 10rpx; padding: 16rpx; font-size: 26rpx; color: #17255f; background: #fff; }
.row { display: flex; gap: 12rpx; align-items: center; }
.grow { flex: 1; min-width: 0; }
.picker-value { color: #17255f; }
.muted { display: block; font-size: 26rpx; color: #64748b; margin: 8rpx 0; }
.error-text { display: block; color: #d92d20; font-size: 24rpx; margin: 10rpx 0; }
.chips { display: flex; flex-wrap: wrap; gap: 10rpx; margin-bottom: 12rpx; }
.chip { border: 1rpx solid #e2e8f0; border-radius: 999rpx; padding: 8rpx 18rpx; font-size: 26rpx; color: #475569; background: #fff; }
.chip.on { background: #2563eb; border-color: #2563eb; color: #fff; }
.btn-block { width: 100%; margin-top: 12rpx; }
</style>
