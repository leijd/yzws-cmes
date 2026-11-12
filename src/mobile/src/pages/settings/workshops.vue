<template>
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">车间管理</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建车间</button>
    </view>
    <view class="chips">
      <view
        v-for="s in statusOptions"
        :key="s.value"
        class="chip"
        :class="{ on: status === s.value }"
        @click="status = s.value; reload()"
      >{{ s.label }}</view>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无车间，点击右上角新建</view>

    <view v-for="w in items" :key="w.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title">{{ w.name }}</text>
            <text class="tag" :class="statusCls(w.status)">{{ statusText(w.status) }}</text>
          </view>
          <text class="muted">编码 {{ w.code }} · {{ natureText(w.workshop_nature) }}</text>
        </view>
      </view>
      <view class="row gap8" style="margin-top: 16rpx">
        <text class="muted">产线 {{ w.line_count }} · 在职 {{ w.employee_count }} 人</text>
        <text v-if="w.outsourced_partner" class="muted">· 外包方 {{ w.outsourced_partner }}</text>
      </view>
      <view class="actions">
        <button class="btn btn-outline btn-sm" @click="openEdit(w)">编辑</button>
        <button class="btn btn-outline btn-sm" @click="goLines(w)">产线管理</button>
        <button v-if="w.status === 'active'" class="btn btn-danger btn-sm" @click="disable(w)">停用</button>
      </view>
    </view>

    <!-- 新建/编辑弹层 -->
    <view v-if="editing" class="sheet-mask" @click="editing = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">{{ form.id ? "编辑车间" : "新建车间" }}</view>
        <view class="field">
          <text class="field-label required">车间名称</text>
          <input class="input" v-model.trim="form.name" placeholder="如 缝制一车间" />
        </view>
        <view v-if="!form.id" class="field">
          <text class="field-label">编码（留空自动生成）</text>
          <input class="input" v-model.trim="form.code" placeholder="如 CJ-00001" />
        </view>
        <view class="field">
          <text class="field-label">车间性质</text>
          <picker :range="natureLabels" :value="natureIndex" @change="onNatureChange">
            <view class="picker-box">{{ natureLabels[natureIndex] }}</view>
          </picker>
        </view>
        <view v-if="form.workshop_nature === 'outsourced'" class="field">
          <text class="field-label required">外包方名称</text>
          <input class="input" v-model.trim="form.outsourced_partner" placeholder="外包合作方" />
        </view>
        <view class="field">
          <text class="field-label">产能说明</text>
          <input class="input" v-model.trim="form.capacity_note" placeholder="如 日产能 800 件（可选）" />
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="form.remark" placeholder="可选" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="save">
          {{ saving ? "保存中…" : "保存" }}
        </button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Workshop {
  id: string;
  code: string;
  name: string;
  status: string;
  workshop_nature: string;
  outsourced_partner?: string;
  capacity_note?: string;
  remark?: string;
  version: number;
  line_count: number;
  employee_count: number;
}

const items = ref<Workshop[]>([]);
const loading = ref(false);
const status = ref("");
const statusOptions = [
  { label: "全部", value: "" },
  { label: "启用", value: "active" },
  { label: "停用", value: "disabled" },
];

const editing = ref(false);
const saving = ref(false);
const formError = ref("");
const natureLabels = ["自产车间", "外包车间"];
const natureValues = ["own", "outsourced"];
const natureIndex = ref(0);
const form = ref({
  id: "",
  code: "",
  name: "",
  workshop_nature: "own",
  outsourced_partner: "",
  capacity_note: "",
  remark: "",
  version: 0,
});

function statusText(s: string) {
  return s === "active" ? "启用" : "停用";
}
function statusCls(s: string) {
  return s === "active" ? "tag-green" : "tag-gray";
}
function natureText(s: string) {
  return s === "outsourced" ? "外包车间" : "自产车间";
}

function onNatureChange(e: { detail: { value: number } }) {
  natureIndex.value = e.detail.value;
  form.value.workshop_nature = natureValues[e.detail.value] || "own";
}

async function reload() {
  loading.value = true;
  try {
    const q = status.value ? `?status=${status.value}` : "";
    const res = await request<{ items: Workshop[] }>(`/workshops${q}`, { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  form.value = { id: "", code: "", name: "", workshop_nature: "own", outsourced_partner: "", capacity_note: "", remark: "", version: 0 };
  natureIndex.value = 0;
  formError.value = "";
  editing.value = true;
}

function openEdit(w: Workshop) {
  form.value = {
    id: w.id,
    code: w.code,
    name: w.name,
    workshop_nature: w.workshop_nature || "own",
    outsourced_partner: w.outsourced_partner || "",
    capacity_note: w.capacity_note || "",
    remark: w.remark || "",
    version: w.version,
  };
  natureIndex.value = natureValues.indexOf(form.value.workshop_nature);
  formError.value = "";
  editing.value = true;
}

async function save() {
  formError.value = "";
  if (!form.value.name) {
    formError.value = "请填写车间名称";
    return;
  }
  if (form.value.workshop_nature === "outsourced" && !form.value.outsourced_partner) {
    formError.value = "外包车间必须填写外包方名称";
    return;
  }
  saving.value = true;
  try {
    if (form.value.id) {
      await request(`/workshops/${form.value.id}`, {
        method: "PATCH",
        data: {
          name: form.value.name,
          workshop_nature: form.value.workshop_nature,
          outsourced_partner: form.value.outsourced_partner,
          capacity_note: form.value.capacity_note,
          remark: form.value.remark,
          version: form.value.version,
        },
      });
    } else {
      await request("/workshops", {
        method: "POST",
        data: {
          code: form.value.code || undefined,
          name: form.value.name,
          workshop_nature: form.value.workshop_nature,
          outsourced_partner: form.value.outsourced_partner,
          capacity_note: form.value.capacity_note,
          remark: form.value.remark,
        },
        idempotency: true,
      });
    }
    uni.showToast({ title: "已保存", icon: "none" });
    editing.value = false;
    reload();
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function disable(w: Workshop) {
  uni.showModal({
    title: "停用车间",
    content: `确定停用「${w.name}」吗？车间下有启用产线时将无法停用。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/workshops/${w.id}/disable`, { method: "POST" });
        uni.showToast({ title: "已停用", icon: "none" });
        reload();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "停用失败", icon: "none" });
      }
    },
  });
}

function goLines(w: Workshop) {
  uni.navigateTo({ url: `/pages/settings/lines?workshop_id=${w.id}&workshop_name=${encodeURIComponent(w.name)}` });
}

onLoad(reload);
</script>

<style scoped>
.sheet-mask {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  z-index: 50;
  display: flex;
  align-items: flex-end;
}
.sheet {
  background: #fff;
  border-radius: 24rpx 24rpx 0 0;
  padding: 32rpx;
  width: 100%;
  max-height: 86vh;
  overflow-y: auto;
  box-sizing: border-box;
}
.error-text {
  color: #d92d20;
  font-size: 24rpx;
  margin-top: 12rpx;
}
</style>
