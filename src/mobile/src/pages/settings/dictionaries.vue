<template>
  <BrandNavBar title="字典维护" />
  <view class="page">
    <view class="chips">
      <view
        v-for="t in types"
        :key="t.type"
        class="chip"
        :class="{ on: currentType === t.type }"
        @click="selectType(t.type)"
      >{{ t.type }}（{{ t.total }}）</view>
    </view>

    <view class="row between" style="margin-bottom: 16rpx">
      <text class="section-title" style="margin: 0">{{ currentType || "字典项" }}</text>
      <button class="btn btn-solid btn-sm" :disabled="!currentType" @click="openCreate">＋ 添加项</button>
    </view>

    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">该类型暂无字典项</view>

    <view v-for="it in items" :key="it.id" class="card row between">
      <view class="grow">
        <view class="row gap8">
          <text class="title" style="font-size: 28rpx">{{ it.name }}</text>
          <text class="tag" :class="it.status === 'active' ? 'tag-green' : 'tag-gray'">{{ it.status === "active" ? "启用" : "停用" }}</text>
        </view>
        <text class="muted">{{ it.code }} · 排序 {{ it.sort_no }}</text>
      </view>
      <button class="btn btn-outline btn-sm" @click="openEdit(it)">编辑</button>
    </view>

    <view v-if="sheet" class="sheet-mask" @click="sheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">{{ form.id ? "编辑字典项" : "添加字典项" }}</view>
        <view v-if="!form.id" class="field">
          <text class="field-label">编码（留空自动生成）</text>
          <input class="input" v-model.trim="form.code" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label required">名称</text>
          <input class="input" v-model.trim="form.name" placeholder="字典项名称" />
        </view>
        <view class="field">
          <text class="field-label">排序号</text>
          <input class="input" v-model="form.sort_no" type="number" placeholder="0" />
        </view>
        <view v-if="form.id" class="field">
          <text class="field-label">状态</text>
          <view class="chips">
            <view class="chip" :class="{ on: form.status === 'active' }" @click="form.status = 'active'">启用</view>
            <view class="chip" :class="{ on: form.status === 'disabled' }" @click="form.status = 'disabled'">停用</view>
          </view>
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

interface DictType { type: string; total: number; active: number }
interface DictItem { id: string; code: string; name: string; parent_id?: string; sort_no: number; status: string; version: number }

const types = ref<DictType[]>([]);
const currentType = ref("");
const items = ref<DictItem[]>([]);
const loading = ref(false);
const saving = ref(false);

const sheet = ref(false);
const formError = ref("");
const form = ref({ id: "", code: "", name: "", sort_no: "0", status: "active", version: 0 });

async function loadTypes() {
  try {
    const res = await request<{ items: DictType[] }>("/dictionaries/types", { silent: true });
    types.value = res.items || [];
    if (types.value.length && !currentType.value) selectType(types.value[0].type);
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  }
}

async function loadItems() {
  if (!currentType.value) return;
  loading.value = true;
  try {
    const res = await request<{ items: DictItem[] }>(`/dictionaries/${currentType.value}/items`, { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function selectType(t: string) {
  currentType.value = t;
  loadItems();
}

function openCreate() {
  form.value = { id: "", code: "", name: "", sort_no: "0", status: "active", version: 0 };
  formError.value = "";
  sheet.value = true;
}

function openEdit(it: DictItem) {
  form.value = { id: it.id, code: it.code, name: it.name, sort_no: String(it.sort_no), status: it.status, version: it.version };
  formError.value = "";
  sheet.value = true;
}

async function save() {
  formError.value = "";
  if (!form.value.name) { formError.value = "请填写名称"; return; }
  saving.value = true;
  try {
    if (form.value.id) {
      await request(`/dictionaries/items/${form.value.id}`, {
        method: "PATCH",
        data: {
          version: form.value.version,
          name: form.value.name,
          sort_no: Number(form.value.sort_no || 0),
          status: form.value.status,
        },
      });
    } else {
      await request(`/dictionaries/${currentType.value}/items`, {
        method: "POST",
        data: {
          code: form.value.code || undefined,
          name: form.value.name,
          sort_no: Number(form.value.sort_no || 0),
        },
        idempotency: true,
      });
    }
    uni.showToast({ title: "已保存", icon: "none" });
    sheet.value = false;
    loadTypes();
    loadItems();
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

onLoad(loadTypes);
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
