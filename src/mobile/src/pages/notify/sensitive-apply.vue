<template>
  <BrandNavBar title="申请查看敏感信息" />
  <view class="page">
    <view class="card">
      <view class="card-title">申请查看敏感信息</view>
      <text class="muted">向租户管理员发出查看敏感信息授权码申请，审批通过后授权码会发到您的手机；当前未连接真实短信环境时，授权码直接显示在本页。</text>
      <view class="kv"><text class="k">目标员工</text><text class="v">{{ employeeName || employeeId }}</text></view>
      <view class="kv"><text class="k">申请字段</text><text class="v">{{ fieldLabel || "员工全部敏感信息" }}</text></view>
    </view>

    <view class="card">
      <view class="field">
        <text class="field-label">接收授权码短信的手机号</text>
        <input class="input" v-model.trim="phone" type="number" maxlength="11" placeholder="留空自动使用本人档案手机号" />
      </view>
      <view class="field">
        <text class="field-label">申请事由（选填）</text>
        <input class="input" v-model.trim="reasonText" placeholder="如 需核对员工联系电话" />
      </view>
      <button class="btn btn-solid btn-block" :disabled="busy" @click="submit">{{ busy ? "提交中…" : "向租户管理员申请授权码" }}</button>
    </view>

    <view v-if="errorText" class="card error-card">{{ errorText }}</view>
    <view v-if="current" class="card">
      <view class="card-title">申请状态</view>
      <view class="kv"><text class="k">当前状态</text><text class="v">{{ statusLabels[current.status] || current.status }}</text></view>
      <view class="kv"><text class="k">申请时间</text><text class="v">{{ formatDate(current.created_at) }}</text></view>
      <text v-if="current.status === 'pending'" class="muted">已提交，等待租户管理员在提醒详情中审批（自动刷新）…</text>
      <text v-if="current.status === 'rejected'" class="muted">申请被拒绝，可修改信息后重新提交。</text>
      <text v-if="current.code_expired" class="muted">上次签发的授权码已过期，请重新发起申请。</text>
      <text v-if="current.sms_sent && !current.code_expired" class="muted">授权码已短信发送到您的手机，请在下方输入。</text>
      <template v-if="current.demo_code_visible">
        <text class="muted" style="margin-top: 16rpx">演示模式授权码（当前未连接真实短信环境）：</text>
        <text class="code" selectable>{{ current.demo_code }}</text>
        <button class="btn btn-outline btn-sm" @click="copyCode">复制授权码</button>
        <button class="btn btn-solid btn-block" @click="grantCode = current.demo_code; reveal()">使用此授权码解锁查看</button>
      </template>
      <button v-if="current.status === 'pending'" class="btn btn-outline btn-sm" @click="refresh">立即刷新</button>
    </view>

    <view class="card">
      <view class="card-title">输入授权码解锁查看</view>
      <input class="input" v-model.trim="grantCode" placeholder="输入 12 位授权码" />
      <button class="btn btn-solid btn-block" :disabled="revealing" @click="reveal">{{ revealing ? "验证中…" : "解锁查看" }}</button>
      <text class="muted">验证通过后该员工全部加密信息显示明文，5 分钟后自动收回。</text>
      <template v-if="revealedFields.length">
        <view v-for="f in revealedFields" :key="f.key" class="kv">
          <text class="k">{{ f.label }}</text><text class="v" selectable>{{ f.value }}</text>
        </view>
      </template>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad, onUnload } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { formatDate } from "../../utils/format";

const employeeId = ref("");
const employeeName = ref("");
const fieldLabel = ref("");
const fieldKey = ref("");
const phone = ref("");
const reasonText = ref("");
const grantCode = ref("");
const current = ref<any>(null);
const busy = ref(false);
const revealing = ref(false);
const errorText = ref("");
const revealedFields = ref<{ key: string; label: string; value: string }[]>([]);
let timer: ReturnType<typeof setInterval> | undefined;

const statusLabels: Record<string, string> = { pending: "待审批", approved: "已允许", rejected: "已拒绝" };
const fieldLabels: Record<string, string> = {
  birth_date: "出生日期", phone: "联系电话", id_card_no: "身份证号", native_place: "籍贯",
  address: "现住址", bank_card: "银行卡号", bank_name: "开户行",
  emergency_contact: "紧急联系人", emergency_phone: "紧急联系电话",
};

async function refresh() {
  if (!employeeId.value) return;
  try {
    const res = await request<{ items: any[] }>(
      `/sensitive-view-requests/mine?employee_id=${encodeURIComponent(employeeId.value)}`,
      { silent: true }
    );
    current.value = res.items?.[0] || null;
  } catch {
    // 轮询失败不打断页面
  }
}

async function submit() {
  if (!employeeId.value) { errorText.value = "缺少员工档案信息"; return; }
  busy.value = true;
  errorText.value = "";
  try {
    const res = await request<{ request: any }>("/sensitive-view-requests", {
      method: "POST",
      data: {
        employee_id: employeeId.value,
        field_key: fieldKey.value,
        field_label: fieldLabel.value,
        reason: reasonText.value,
        phone: phone.value,
      },
    });
    current.value = res.request;
    uni.showToast({ title: "申请已发送", icon: "none" });
  } catch (e) {
    errorText.value = (e as Error).message || "提交失败";
  } finally {
    busy.value = false;
  }
}

function copyCode() {
  if (!current.value?.demo_code) return;
  uni.setClipboardData({
    data: String(current.value.demo_code),
    success: () => uni.showToast({ title: "已复制", icon: "none" }),
  });
}

async function reveal() {
  const code = grantCode.value.trim();
  if (!code) { errorText.value = "请输入授权码"; return; }
  revealing.value = true;
  errorText.value = "";
  try {
    const token = await request<{ reveal_token: string }>(`/employees/${encodeURIComponent(employeeId.value)}/reveal`, {
      method: "POST",
      data: { grant_code: code },
    });
    const detail = await request<any>(
      `/employees/${encodeURIComponent(employeeId.value)}?reveal_token=${encodeURIComponent(token.reveal_token)}`,
      { silent: true }
    );
    revealedFields.value = Object.keys(fieldLabels).map((key) => ({
      key,
      label: fieldLabels[key],
      value: String(detail[key] ?? "—"),
    }));
    uni.showToast({ title: "已解锁 5 分钟", icon: "none" });
  } catch (e) {
    errorText.value = (e as Error).message || "解锁失败";
  } finally {
    revealing.value = false;
  }
}

onLoad((options: any) => {
  employeeId.value = decodeURIComponent(options?.employee_id || "");
  employeeName.value = decodeURIComponent(options?.employee_name || "");
  fieldLabel.value = decodeURIComponent(options?.field_label || "");
  fieldKey.value = decodeURIComponent(options?.field_key || "");
  void refresh();
  timer = setInterval(() => { void refresh(); }, 3000);
});
onUnload(() => { if (timer) clearInterval(timer); });
</script>

<style scoped>
.page { min-height: 100vh; background: #f5f7fa; padding: 24rpx 32rpx; box-sizing: border-box; }
.card { background: #fff; border-radius: 16rpx; padding: 24rpx 32rpx; margin-bottom: 20rpx; }
.card-title { font-size: 30rpx; font-weight: 600; color: #2d3748; margin-bottom: 12rpx; }
.muted { display: block; font-size: 24rpx; color: #94a3b8; line-height: 1.7; margin-top: 8rpx; }
.field { margin: 16rpx 0; }
.field-label { display: block; font-size: 26rpx; color: #475569; margin-bottom: 10rpx; }
.input { border: 1px solid #e2e8f0; border-radius: 10rpx; background: #fff; padding: 0 20rpx; height: 76rpx; font-size: 26rpx; }
.kv { display: flex; padding: 12rpx 0; border-bottom: 1px solid #f1f5f9; font-size: 26rpx; }
.k { width: 200rpx; color: #94a3b8; }
.v { flex: 1; color: #2d3748; }
.btn { border-radius: 10rpx; font-size: 28rpx; height: 80rpx; line-height: 80rpx; }
.btn-block { display: block; width: 100%; margin-top: 20rpx; }
.btn-sm { height: 64rpx; line-height: 64rpx; font-size: 24rpx; margin-top: 16rpx; }
.btn-solid { background: #2563eb; color: #fff; }
.btn-outline { background: #fff; color: #475569; border: 1px solid #e2e8f0; }
.code { display: block; font-size: 40rpx; font-weight: 700; letter-spacing: 4rpx; margin-top: 12rpx; color: #065f46; }
.error-card { background: #fef2f2; color: #b91c1c; font-size: 26rpx; }
</style>
