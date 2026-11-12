<template>
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <template v-else-if="profile">
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <text class="title">{{ profile.name }}</text>
            <text class="muted">编码 {{ profile.code }} · 简称 {{ profile.short_code }}</text>
          </view>
          <text class="tag" :class="profile.customer_type === 'trial' ? 'tag-orange' : 'tag-green'">
            {{ profile.customer_type === "trial" ? "体验版" : "正式版" }}
          </text>
        </view>
        <view v-if="profile.subscription" class="kv" style="margin-top: 16rpx">
          <text class="k">订阅</text>
          <text class="v">{{ profile.subscription.edition }} · {{ profile.subscription.status }}</text>
        </view>
        <view class="kv">
          <text class="k">账号用量</text>
          <text class="v">{{ profile.user_quota?.used || 0 }} / {{ profile.user_quota?.limit ?? "不限" }}</text>
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">联系信息</view>
        <view class="field">
          <text class="field-label">联系人</text>
          <input class="input" v-model.trim="form.contact_name" placeholder="联系人姓名" />
        </view>
        <view class="field">
          <text class="field-label">联系电话</text>
          <input class="input" v-model.trim="form.contact_phone" placeholder="联系电话" />
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">微信</text>
            <input class="input" v-model.trim="form.contact_wechat" placeholder="可选" />
          </view>
          <view class="field grow">
            <text class="field-label">QQ</text>
            <input class="input" v-model.trim="form.contact_qq" placeholder="可选" />
          </view>
        </view>
        <view class="field">
          <text class="field-label">支付宝</text>
          <input class="input" v-model.trim="form.contact_alipay" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label">所在地区</text>
          <input class="input" v-model.trim="form.region" placeholder="如 广东" />
        </view>
        <view class="field">
          <text class="field-label">厂地址</text>
          <input class="input" v-model.trim="form.address" placeholder="详细地址" />
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="form.remark" placeholder="可选" />
        </view>
        <view v-if="error" class="error-text">{{ error }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存本厂信息" }}</button>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Profile {
  code: string; name: string; short_code: string; customer_type: string;
  contact_name?: string; contact_phone?: string; contact_wechat?: string;
  contact_qq?: string; contact_alipay?: string; address?: string; region?: string;
  remark?: string; version: number;
  user_quota?: { used: number; limit: number | null };
  subscription?: { edition: string; user_limit: number | null; status: string } | null;
}

const loading = ref(false);
const saving = ref(false);
const error = ref("");
const profile = ref<Profile | null>(null);
const form = ref({
  contact_name: "", contact_phone: "", contact_wechat: "", contact_qq: "",
  contact_alipay: "", address: "", region: "", remark: "",
});

async function load() {
  loading.value = true;
  try {
    const res = await request<Profile>("/tenant/profile", { silent: true });
    profile.value = res;
    form.value = {
      contact_name: res.contact_name || "",
      contact_phone: res.contact_phone || "",
      contact_wechat: res.contact_wechat || "",
      contact_qq: res.contact_qq || "",
      contact_alipay: res.contact_alipay || "",
      address: res.address || "",
      region: res.region || "",
      remark: res.remark || "",
    };
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!profile.value) return;
  error.value = "";
  saving.value = true;
  try {
    await request("/tenant/profile", {
      method: "PUT",
      data: { ...form.value, version: profile.value.version },
    });
    uni.showToast({ title: "已保存", icon: "none" });
    load();
  } catch (e) {
    error.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

onLoad(load);
</script>

<style scoped>
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
