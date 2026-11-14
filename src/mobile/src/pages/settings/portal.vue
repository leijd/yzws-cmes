<template>
  <BrandNavBar title="门户内容" />
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <template v-else-if="content">
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <text class="title">门户内容</text>
            <text class="muted">对外门户展示的欢迎语、logo 与板块</text>
          </view>
        </view>
        <view v-if="content.logo_preview_url" class="logo-preview">
          <image :src="logoFullUrl" mode="aspectFit" class="logo-img" />
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">品牌</view>
        <view class="field">
          <text class="field-label">系统名称（默认「一智算」）</text>
          <input class="input" v-model.trim="form.system_name" placeholder="如 XX制衣 MES" />
        </view>
        <view class="field">
          <text class="field-label">logo 网址（留空用已上传 logo 或平台默认）</text>
          <input class="input" v-model.trim="form.logo_url" placeholder="https://... 或 /path" />
        </view>
        <view class="actions" style="margin-top: 12rpx">
          <button class="btn btn-outline btn-sm" @click="uploadLogo">上传 logo（PNG/JPG/GIF ≤500KB）</button>
          <button v-if="content.has_uploaded_logo || form.logo_url" class="btn btn-danger btn-sm" @click="removeLogo">移除 logo</button>
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">欢迎语</view>
        <view class="field">
          <text class="field-label">欢迎标题（默认：{{ content.default_title }}）</text>
          <input class="input" v-model.trim="form.welcome_title" placeholder="≤60 字" />
        </view>
        <view class="field">
          <text class="field-label">欢迎语（默认：{{ content.default_text }}）</text>
          <textarea class="input" style="height: 160rpx" v-model.trim="form.welcome_text" placeholder="≤400 字" />
        </view>
        <view class="field">
          <text class="field-label">门户风格</text>
          <view class="chips">
            <view v-for="s in styleOptions" :key="s.value" class="chip" :class="{ on: form.style_code === s.value }" @click="form.style_code = s.value">{{ s.label }}</view>
          </view>
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">门户板块</view>
        <view v-for="s in form.sections" :key="s.key" class="row between" style="padding: 14rpx 0">
          <view class="grow">
            <text class="title" style="font-size: 26rpx">{{ s.title }}</text>
            <text class="muted">{{ s.description }}</text>
          </view>
          <switch :checked="s.enabled" @change="onSectionToggle(s, $event)" color="#2563eb" />
        </view>
      </view>

      <view v-if="error" class="error-text">{{ error }}</view>
      <button class="btn btn-solid btn-block" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存门户内容" }}</button>
      <button class="btn btn-ghost btn-block" style="margin-top: 16rpx" @click="resetDefault">恢复平台默认</button>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { API_BASE_URL } from "../../config";

interface PortalSection { key: string; title: string; description: string; enabled: boolean }
interface PortalContent {
  tenant_name: string; edition: string;
  logo_url: string; logo_preview_url: string; has_uploaded_logo: boolean;
  system_name: string; default_system_name: string;
  welcome_title: string; welcome_text: string;
  default_title: string; default_text: string;
  style_code: string; sections: PortalSection[]; version: number;
}

const loading = ref(false);
const saving = ref(false);
const error = ref("");
const content = ref<PortalContent | null>(null);
const styleOptions = [
  { label: "经典蓝", value: "classic" },
  { label: "深色", value: "dark" },
  { label: "清新", value: "fresh" },
];
const form = ref({
  logo_url: "", system_name: "", welcome_title: "", welcome_text: "",
  style_code: "classic", sections: [] as PortalSection[],
});

const logoFullUrl = computed(() => {
  const url = content.value?.logo_preview_url || "";
  if (!url) return "";
  return /^https?:\/\//.test(url) ? url : API_BASE_URL + (url.startsWith("/") ? url : "/" + url);
});

function onSectionToggle(s: PortalSection, e: unknown) {
  s.enabled = (e as { detail: { value: boolean } }).detail.value;
}

async function load() {
  loading.value = true;
  try {
    const res = await request<PortalContent>("/tenant-portal/content", { silent: true });
    content.value = res;
    form.value = {
      logo_url: res.logo_url || "",
      system_name: res.system_name || "",
      welcome_title: res.welcome_title || "",
      welcome_text: res.welcome_text || "",
      style_code: res.style_code || "classic",
      sections: res.sections || [],
    };
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function save() {
  error.value = "";
  saving.value = true;
  try {
    await request("/tenant-portal/content", {
      method: "PUT",
      data: {
        logo_url: form.value.logo_url,
        system_name: form.value.system_name,
        welcome_title: form.value.welcome_title,
        welcome_text: form.value.welcome_text,
        style_code: form.value.style_code,
        sections: form.value.sections,
      },
    });
    uni.showToast({ title: "已保存，门户页下次打开生效", icon: "none" });
    load();
  } catch (e) {
    error.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function resetDefault() {
  uni.showModal({
    title: "恢复平台默认",
    content: "将清空自定义欢迎语、风格与板块，并移除 logo，确定恢复吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request("/tenant-portal/content", {
          method: "PUT",
          data: { reset_sections: true, reset_logo: true, logo_url: "", system_name: "", welcome_title: "", welcome_text: "", style_code: "classic" },
        });
        uni.showToast({ title: "已恢复默认", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "恢复失败", icon: "none" });
      }
    },
  });
}

function uploadLogo() {
  uni.chooseImage({
    count: 1,
    /* 用原图：App 端「压缩」可能把 PNG/GIF 转码成 JPEG，导致服务端按文件头校验被拒 */
    sizeType: ["original"],
    success: (res) => {
      const path = res.tempFilePaths[0];
      const files = (res.tempFiles as unknown as { size?: number }[]) || [];
      const file = files[0];
      if (file && file.size && file.size > 500 * 1024) {
        uni.showToast({ title: "logo 不能超过 500KB", icon: "none" });
        return;
      }
      uni.uploadFile({
        url: API_BASE_URL + "/api/v1/tenant-portal/logo",
        filePath: path,
        name: "file",
        header: { Authorization: "Bearer " + (uni.getStorageSync("clothp_access_token") || "") },
        success: (uploadRes) => {
          if (uploadRes.statusCode >= 200 && uploadRes.statusCode < 300) {
            uni.showToast({ title: "logo 已上传", icon: "none" });
            load();
            return;
          }
          /* 显示后端的具体原因（格式/大小/权限），不要只报一个笼统的「上传失败」 */
          let msg = `上传失败 (${uploadRes.statusCode})`;
          try {
            const body = JSON.parse(uploadRes.data as string) as { message?: string };
            if (body?.message) msg = body.message;
          } catch {
            /* 响应不是 JSON 时保留状态码提示 */
          }
          uni.showToast({ title: msg, icon: "none" });
        },
        fail: (err) => uni.showToast({ title: err.errMsg || "上传失败，请检查网络", icon: "none" }),
      });
    },
  });
}

function removeLogo() {
  uni.showModal({
    title: "移除 logo",
    content: "确定移除企业 logo，恢复平台默认 logo 吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request("/tenant-portal/logo", { method: "DELETE" });
        uni.showToast({ title: "已移除", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "移除失败", icon: "none" });
      }
    },
  });
}

onLoad(load);
</script>

<style scoped>
.logo-preview { display: flex; justify-content: center; margin-top: 20rpx; }
.logo-img { width: 160rpx; height: 160rpx; border-radius: 16rpx; background: #f8fafc; }
.error-text { color: #d92d20; font-size: 24rpx; margin: 16rpx 0; }
</style>
