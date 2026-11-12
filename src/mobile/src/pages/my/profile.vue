<template>
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

onLoad(load);
</script>

<style scoped>
.error-text {
  color: #d92d20;
  font-size: 24rpx;
  margin: 16rpx 0;
}
</style>
