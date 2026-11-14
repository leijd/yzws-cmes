<template>
  <BrandNavBar title="基础数据" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">{{ cfg.title }}</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建</button>
    </view>
    <view class="search-bar">
      <input class="input" v-model.trim="keyword" placeholder="名称/编码搜索" @confirm="reload" />
      <button class="btn btn-outline" style="width: 160rpx; height: 80rpx; line-height: 80rpx" @click="reload">查询</button>
    </view>
    <view class="chips">
      <view v-for="s in statusOptions" :key="s.value" class="chip" :class="{ on: status === s.value }" @click="status = s.value; reload()">{{ s.label }}</view>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无数据</view>

    <view v-for="it in items" :key="it.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ it.name }}</text>
            <text class="tag" :class="it.status === 'active' ? 'tag-green' : 'tag-gray'">{{ it.status === "active" ? "启用" : "停用" }}</text>
          </view>
          <text class="muted">{{ it.code }}{{ subtitle(it) }}</text>
        </view>
      </view>
      <view class="actions">
        <button class="btn btn-outline btn-sm" @click="openEdit(it)">编辑</button>
        <button v-if="it.status === 'active'" class="btn btn-danger btn-sm" @click="disable(it)">停用</button>
      </view>
    </view>
    <view v-if="hasMore" class="loadmore" @click="loadMore">{{ loading ? "加载中…" : "加载更多" }}</view>

    <!-- 新建/编辑弹层 -->
    <view v-if="sheet" class="sheet-mask" @click="sheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">{{ form.id ? "编辑" : "新建" }}{{ cfg.title }}</view>
        <view v-for="f in cfg.fields" :key="f.key" class="field">
          <text class="field-label" :class="{ required: f.required && !form.id }">{{ f.label }}</text>
          <picker v-if="f.type === 'picker'" :range="f.options" :value="pickerIndex(f.key)" @change="onPickerChange(f.key, $event.detail.value)">
            <view class="picker-box">{{ pickerLabel(f.key) }}</view>
          </picker>
          <input v-else class="input" v-model.trim="form[f.key]" :placeholder="f.placeholder || ''" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface FieldDef {
  key: string;
  label: string;
  required?: boolean;
  placeholder?: string;
  type?: "text" | "picker";
  options?: string[];
  optionValues?: string[];
}

interface TypeConfig {
  title: string;
  path: string;
  fields: FieldDef[];
  subtitleKeys: string[];
}

const configs: Record<string, TypeConfig> = {
  customers: {
    title: "客户",
    path: "/customers",
    subtitleKeys: ["contact_name", "phone"],
    fields: [
      { key: "code", label: "编码（留空自动生成）" },
      { key: "name", label: "客户名称", required: true },
      { key: "contact_name", label: "联系人" },
      { key: "phone", label: "联系电话" },
      { key: "address", label: "地址" },
      { key: "remark", label: "备注" },
    ],
  },
  suppliers: {
    title: "供应商",
    path: "/suppliers",
    subtitleKeys: ["contact_name", "phone"],
    fields: [
      { key: "code", label: "编码（留空自动生成）" },
      { key: "name", label: "供应商名称", required: true },
      { key: "contact_name", label: "联系人" },
      { key: "phone", label: "联系电话" },
      { key: "address", label: "地址" },
      { key: "remark", label: "备注" },
    ],
  },
  materials: {
    title: "物料",
    path: "/materials",
    subtitleKeys: ["specification", "unit"],
    fields: [
      { key: "code", label: "编码（留空自动生成）" },
      { key: "name", label: "物料名称", required: true },
      { key: "category", label: "类别" },
      { key: "specification", label: "规格" },
      { key: "unit", label: "单位", required: true, placeholder: "如 米/件/公斤" },
      { key: "safety_stock", label: "安全库存", placeholder: "0" },
    ],
  },
  warehouses: {
    title: "仓库",
    path: "/warehouses",
    subtitleKeys: ["warehouse_type"],
    fields: [
      { key: "code", label: "编码（留空自动生成）" },
      { key: "name", label: "仓库名称", required: true },
      {
        key: "warehouse_type", label: "仓库类型", required: true, type: "picker",
        options: ["原料仓", "半成品仓", "成品仓", "辅料仓", "现场仓"],
        optionValues: ["material", "semi_finished", "finished", "auxiliary", "site"],
      },
    ],
  },
};

const type = ref<keyof typeof configs>("materials");
const cfg = computed(() => configs[type.value]);

interface MasterItem {
  id: string;
  code: string;
  name: string;
  status: string;
  version: number;
  [key: string]: unknown;
}

interface MasterForm {
  id: string;
  version: number;
  [key: string]: unknown;
}

const items = ref<MasterItem[]>([]);
const loading = ref(false);
const hasMore = ref(false);
const nextCursor = ref("");
const keyword = ref("");
const status = ref("");
const statusOptions = [
  { label: "全部", value: "" },
  { label: "启用", value: "active" },
  { label: "停用", value: "disabled" },
];

const sheet = ref(false);
const saving = ref(false);
const formError = ref("");
const form = ref<MasterForm>({ id: "", version: 0 });
const pickerIndexes = ref<Record<string, number>>({});

function subtitle(it: MasterItem): string {
  const parts = cfg.value.subtitleKeys.map((k) => it[k]).filter(Boolean);
  return parts.length ? " · " + parts.join(" · ") : "";
}

function pickerIndex(key: string): number {
  return pickerIndexes.value[key] ?? 0;
}
function pickerLabel(key: string): string {
  const f = cfg.value.fields.find((x) => x.key === key);
  if (!f?.options) return "";
  const idx = pickerIndex(key);
  return f.options[idx] || f.options[0];
}
function onPickerChange(key: string, idx: number) {
  pickerIndexes.value = { ...pickerIndexes.value, [key]: idx };
  const f = cfg.value.fields.find((x) => x.key === key);
  if (f?.optionValues) form.value[key] = f.optionValues[idx] || "";
}

async function load(reset = true) {
  loading.value = true;
  try {
    const q: string[] = [];
    if (keyword.value) q.push(`keyword=${encodeURIComponent(keyword.value)}`);
    if (status.value) q.push(`status=${status.value}`);
    if (!reset && nextCursor.value) q.push(`cursor=${nextCursor.value}`);
    const res = await request<{ items: MasterItem[]; next_cursor?: string; has_more?: boolean }>(
      `${cfg.value.path}${q.length ? "?" + q.join("&") : ""}`,
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
  form.value = { id: "", version: 0 };
  pickerIndexes.value = {};
  formError.value = "";
  sheet.value = true;
}

function openEdit(it: MasterItem) {
  const f: MasterForm = { id: String(it.id), version: Number(it.version || 1) };
  for (const field of cfg.value.fields) {
    f[field.key] = String(it[field.key] || "");
  }
  form.value = f;
  pickerIndexes.value = {};
  for (const field of cfg.value.fields) {
    if (field.type === "picker" && field.optionValues) {
      const idx = field.optionValues.indexOf(f[field.key] as string);
      pickerIndexes.value[field.key] = idx >= 0 ? idx : 0;
    }
  }
  formError.value = "";
  sheet.value = true;
}

async function save() {
  formError.value = "";
  for (const f of cfg.value.fields) {
    if (f.required && !form.value.id && !form.value[f.key]) {
      formError.value = `请填写${f.label}`;
      return;
    }
  }
  saving.value = true;
  try {
    const data: Record<string, unknown> = {};
    for (const f of cfg.value.fields) {
      if (form.value[f.key] !== "") data[f.key] = form.value[f.key];
    }
    if (form.value.id) {
      data.version = form.value.version;
      await request(`${cfg.value.path}/${form.value.id}`, { method: "PATCH", data });
    } else {
      await request(cfg.value.path, { method: "POST", data, idempotency: true });
    }
    uni.showToast({ title: "已保存", icon: "none" });
    sheet.value = false;
    reload();
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function disable(it: Record<string, unknown>) {
  uni.showModal({
    title: "停用",
    content: `确定停用「${it.name}」吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`${cfg.value.path}/${it.id}/disable`, {
          method: "POST",
          data: { version: Number(it.version || 1) },
        });
        uni.showToast({ title: "已停用", icon: "none" });
        reload();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "停用失败", icon: "none" });
      }
    },
  });
}

onLoad((query) => {
  const q = (query as Record<string, string>) || {};
  if (q.type && configs[q.type]) type.value = q.type as keyof typeof configs;
  uni.setNavigationBarTitle({ title: cfg.value.title + "管理" });
  reload();
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
