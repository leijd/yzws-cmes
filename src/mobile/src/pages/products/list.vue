<template>
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">产品与工序</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建产品</button>
    </view>
    <view class="search-bar">
      <input class="input" v-model.trim="keyword" placeholder="名称/编码搜索" @confirm="reload" />
      <button class="btn btn-outline" style="width: 160rpx; height: 80rpx; line-height: 80rpx" @click="reload">查询</button>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无产品</view>

    <view v-for="p in items" :key="p.id" class="card" @click="goDetail(p)">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ p.name }}</text>
            <text class="tag" :class="p.status === 'active' ? 'tag-green' : 'tag-gray'">{{ p.status === "active" ? "上架" : "草稿/隐藏" }}</text>
          </view>
          <text class="muted">{{ p.product_code }} · {{ p.spec || "无规格" }} · {{ p.unit }}</text>
        </view>
        <text class="arrow">›</text>
      </view>
      <view class="row between" style="margin-top: 14rpx">
        <text class="muted">工序 {{ p.process_count }} 道 · 理论工价 {{ p.theoretical_price }}</text>
        <text class="muted">v{{ p.version }}</text>
      </view>
    </view>
    <view v-if="hasMore" class="loadmore" @click="loadMore">{{ loading ? "加载中…" : "加载更多" }}</view>

    <!-- 新建产品 -->
    <view v-if="sheet" class="sheet-mask" @click="sheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建产品</view>
        <view class="field">
          <text class="field-label">产品编码（留空自动生成）</text>
          <input class="input" v-model.trim="form.product_code" placeholder="如 CP-00001" />
        </view>
        <view class="field">
          <text class="field-label required">产品名称</text>
          <input class="input" v-model.trim="form.name" placeholder="如 男士 T 恤" />
        </view>
        <view class="field">
          <text class="field-label">规格</text>
          <input class="input" v-model.trim="form.spec" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label required">单位</text>
          <input class="input" v-model.trim="form.unit" placeholder="如 件/打" />
        </view>
        <view class="field">
          <text class="field-label">售价</text>
          <input class="input" v-model="form.sale_price" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="form.remark" placeholder="可选" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Product {
  id: string; product_code: string; name: string; spec?: string; unit: string;
  status: string; version: number; process_count: number; theoretical_price: string;
}

const items = ref<Product[]>([]);
const loading = ref(false);
const hasMore = ref(false);
const nextCursor = ref("");
const keyword = ref("");

const sheet = ref(false);
const saving = ref(false);
const formError = ref("");
const form = ref({ product_code: "", name: "", spec: "", unit: "件", sale_price: "", remark: "" });

async function load(reset = true) {
  loading.value = true;
  try {
    const q: string[] = [];
    if (keyword.value) q.push(`keyword=${encodeURIComponent(keyword.value)}`);
    if (!reset && nextCursor.value) q.push(`cursor=${nextCursor.value}`);
    const res = await request<{ items: Product[]; next_cursor?: string; has_more?: boolean }>(
      `/products${q.length ? "?" + q.join("&") : ""}`,
      { silent: true }
    );
    if (reset) items.value = res.items || [];
    else items.value = [...items.value, ...(res.items || [])];
    nextCursor.value = res.next_cursor || "";
    hasMore.value = !!res.has_more;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}
function reload() {
  load(true);
}
function loadMore() {
  load(false);
}

function openCreate() {
  form.value = { product_code: "", name: "", spec: "", unit: "件", sale_price: "", remark: "" };
  formError.value = "";
  sheet.value = true;
}

async function save() {
  formError.value = "";
  const f = form.value;
  if (!f.name || !f.unit) { formError.value = "产品名称与单位必填"; return; }
  saving.value = true;
  try {
    await request("/products", {
      method: "POST",
      data: {
        product_code: f.product_code || undefined,
        name: f.name,
        spec: f.spec || undefined,
        unit: f.unit,
        sale_price: f.sale_price || undefined,
        remark: f.remark || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "产品已创建", icon: "none" });
    sheet.value = false;
    reload();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function goDetail(p: Product) {
  uni.navigateTo({ url: `/pages/products/detail?id=${p.id}` });
}

onLoad(reload);
</script>

<style scoped>
.arrow { font-size: 40rpx; color: #cbd5e1; }
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
