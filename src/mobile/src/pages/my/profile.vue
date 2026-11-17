<template>
  <BrandNavBar title="我的资料" />
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <template v-else-if="!item">
      <view class="empty">当前账号未关联员工档案</view>
    </template>
    <template v-else>
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <text class="title">{{ item.name || "未设置姓名" }}</text>
            <text class="muted">工号 {{ item.employee_no || "--" }}</text>
          </view>
          <text class="tag" :class="statusCls">{{ statusText }}</text>
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">登录验证</view>
        <text class="muted">短信验证码为基础验证，始终开启且不可关闭；其他方式可自行开通或关闭。</text>
        <view class="row between" style="margin-top: 16rpx">
          <view class="col grow"><text class="title" style="font-size: 26rpx">短信验证码</text><text class="muted">登录时必选，固定开启</text></view>
          <switch :checked="true" disabled color="#2563eb" />
        </view>
        <view class="row between" style="margin-top: 16rpx">
          <view class="col grow"><text class="title" style="font-size: 26rpx">密码验证</text><text class="muted">开通后短信之外还需账号密码</text></view>
          <switch :checked="authMethods.password" :disabled="authSaving" color="#2563eb" @change="toggleAuth('password', $event)" />
        </view>
        <view class="row between" style="margin-top: 16rpx">
          <view class="col grow"><text class="title" style="font-size: 26rpx">微信设备快捷登录</text><text class="muted">关闭时自动解绑已绑定设备</text></view>
          <switch :checked="authMethods.wechat" :disabled="authSaving" color="#2563eb" @change="toggleAuth('wechat', $event)" />
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">联系信息</view>
        <view class="field">
          <text class="field-label required">手机号</text>
          <input class="input" v-model.trim="form.phone" maxlength="11" type="number" placeholder="11 位手机号" />
        </view>
        <view class="field">
          <text class="field-label">紧急联系人</text>
          <input class="input" v-model.trim="form.emergency_contact" placeholder="姓名" />
        </view>
        <view class="field">
          <text class="field-label">紧急联系电话</text>
          <input class="input" v-model.trim="form.emergency_phone" maxlength="11" type="number" placeholder="11 位手机号" />
        </view>
        <view class="field">
          <text class="field-label">住址</text>
          <input class="input" v-model.trim="form.address" placeholder="现居住地址" />
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">身份信息</view>
        <view class="field">
          <text class="field-label">身份证号</text>
          <input class="input" v-model.trim="form.id_card_no" maxlength="18" placeholder="18 位身份证号（选填）" />
        </view>
        <view class="field">
          <text class="field-label">银行卡号</text>
          <input class="input" v-model.trim="form.bank_card" placeholder="选填" />
        </view>
        <view class="field">
          <text class="field-label">开户银行</text>
          <input class="input" v-model.trim="form.bank_name" placeholder="选填" />
        </view>
        <view class="field">
          <text class="field-label">民族</text>
          <input class="input" v-model.trim="form.nation" placeholder="选填" />
        </view>
        <view class="field">
          <text class="field-label">籍贯</text>
          <input class="input" v-model.trim="form.native_place" placeholder="选填" />
        </view>
      </view>

      <view v-if="error" class="error-text">{{ error }}</view>
      <button class="btn btn-solid btn-block" :disabled="saving" @click="save">
        {{ saving ? "保存中…" : "保存修改" }}
      </button>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

const item = ref<Record<string, unknown> | null>(null);
const loading = ref(false);
const saving = ref(false);
const error = ref("");

const form = ref({
  phone: "",
  emergency_contact: "",
  emergency_phone: "",
  address: "",
  id_card_no: "",
  bank_card: "",
  bank_name: "",
  nation: "",
  native_place: "",
});

const statusText = computed(() => (item.value?.status === "active" ? "在职" : "停用"));
const statusCls = computed(() => (item.value?.status === "active" ? "tag-green" : "tag-gray"));

async function load() {
  loading.value = true;
  try {
    const res = await request<Record<string, unknown>>("/my/profile", { silent: true });
    item.value = res;
    form.value = {
      phone: String(res.phone || ""),
      emergency_contact: String(res.emergency_contact || ""),
      emergency_phone: String(res.emergency_phone || ""),
      address: String(res.address || ""),
      id_card_no: String(res.id_card_no || ""),
      bank_card: String(res.bank_card || ""),
      bank_name: String(res.bank_name || ""),
      nation: String(res.nation || ""),
      native_place: String(res.native_place || ""),
    };
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function save() {
  error.value = "";
  if (form.value.phone && !/^1[3-9]\d{9}$/.test(form.value.phone)) {
    error.value = "联系电话须为 11 位手机号";
    return;
  }
  if (form.value.id_card_no && !/^\d{17}[\dXx]$/.test(form.value.id_card_no)) {
    error.value = "身份证号须为 18 位";
    return;
  }
  saving.value = true;
  try {
    await request("/my/profile", {
      method: "PATCH",
      data: { ...form.value },
    });
    uni.showToast({ title: "已保存", icon: "none" });
  } catch (e) {
    error.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

/* 登录验证方式：短信恒开，密码/微信由本人开关 */
const authMethods = ref({ sms: true, password: false, wechat: false });
const authSaving = ref(false);
async function loadAuthMethods() {
  try {
    const r = await request<{ methods: { sms: boolean; password: boolean; wechat: boolean } }>("/me/auth-methods", { silent: true });
    authMethods.value = { ...r.methods, sms: true };
  } catch { /* 无账号时忽略 */ }
}
async function toggleAuth(key: "password" | "wechat", ev: any) {
  if (authSaving.value) return;
  const value = !!ev?.detail?.value;
  authSaving.value = true;
  try {
    const r = await request<{ methods: { sms: boolean; password: boolean; wechat: boolean } }>("/me/auth-methods", {
      method: "PUT",
      data: { [key]: value },
    });
    authMethods.value = { ...r.methods, sms: true };
    uni.showToast({ title: value ? "已开通" : "已关闭", icon: "none" });
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "设置失败", icon: "none" });
    await loadAuthMethods();
  } finally {
    authSaving.value = false;
  }
}

onLoad(() => { void load(); void loadAuthMethods(); });
</script>

<style scoped>
.error-text {
  color: #d92d20;
  font-size: 24rpx;
  margin: 16rpx 0;
}
</style>
