<template>
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!summary" class="empty">项目不存在</view>
    <template v-else>
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <text class="title">订单汇总</text>
            <text class="muted">客户单价 {{ money(summary.customer_unit_price) }} · 出厂单价 {{ money(summary.external_unit_price) }}</text>
          </view>
        </view>
        <view class="row" style="margin-top: 24rpx; gap: 24rpx">
          <view class="sum-cell">
            <text class="sum-num">{{ summary.order_qty }}</text>
            <text class="sum-label">订单数</text>
          </view>
          <view class="sum-cell">
            <text class="sum-num">{{ summary.shipped_qty }}</text>
            <text class="sum-label">已出货</text>
          </view>
          <view class="sum-cell">
            <text class="sum-num">{{ summary.unshipped_qty }}</text>
            <text class="sum-label">未出货</text>
          </view>
        </view>
        <view class="kv" style="margin-top: 24rpx">
          <text class="k">已出货金额（客户价）</text>
          <text class="v strong">{{ money(summary.customer_amount) }}</text>
        </view>
        <view class="kv">
          <text class="k">已出货金额（出厂价）</text>
          <text class="v strong">{{ money(summary.factory_amount) }}</text>
        </view>
      </view>

      <view class="actions" style="margin-top: 24rpx">
        <button class="btn btn-outline btn-sm" @click="openEdit">调整订单数/交期/单价</button>
        <button class="btn btn-danger btn-sm" @click="remove">删除项目</button>
      </view>

      <view v-if="editSheet" class="sheet-mask" @click="editSheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">调整项目</view>
          <view class="field">
            <text class="field-label">订单数量（不能小于已出货/已排产数）</text>
            <input class="input" v-model="form.order_qty" type="number" placeholder="订单数量" />
          </view>
          <view class="field">
            <text class="field-label">要求交期</text>
            <picker mode="date" :value="form.required_delivery_date" @change="form.required_delivery_date = $event.detail.value">
              <view class="picker-box">{{ form.required_delivery_date || "选择日期（可选）" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label">客户单价</text>
            <input class="input" v-model="form.customer_unit_price" placeholder="可选" />
          </view>
          <view class="field">
            <text class="field-label">出厂单价</text>
            <input class="input" v-model="form.external_unit_price" placeholder="可选" />
          </view>
          <view class="field">
            <text class="field-label">调整原因</text>
            <input class="input" v-model.trim="form.reason" placeholder="可选，将记入审计" />
          </view>
          <view v-if="error" class="error-text">{{ error }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="saveEdit">{{ saving ? "保存中…" : "保存" }}</button>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Summary {
  order_qty: number;
  shipped_qty: number;
  unshipped_qty: number;
  customer_amount: string;
  factory_amount: string;
  customer_unit_price: string;
  external_unit_price: string;
}

const id = ref("");
const loading = ref(false);
const summary = ref<Summary | null>(null);
const version = ref(0);
const saving = ref(false);
const editSheet = ref(false);
const error = ref("");
const form = ref({
  order_qty: "",
  required_delivery_date: "",
  customer_unit_price: "",
  external_unit_price: "",
  reason: "",
});

function money(v: string | undefined): string {
  const n = Number(v);
  return Number.isFinite(n) ? n.toFixed(2) : "--";
}

async function load() {
  loading.value = true;
  try {
    const res = await request<Summary>(`/projects/${id.value}/summary`, { silent: true });
    summary.value = res;
    // 列表接口返回 version，这里从 detail 不带，通过单独查询？summary 不带 version，改从列表缓存？直接用 PATCH 前读取项目列表匹配的 version。
    const list = await request<{ items: { id: string; version: number }[] }>("/projects", { silent: true });
    const found = (list.items || []).find((p) => p.id === id.value);
    version.value = found?.version || 1;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function openEdit() {
  if (!summary.value) return;
  form.value = {
    order_qty: String(summary.value.order_qty),
    required_delivery_date: "",
    customer_unit_price: summary.value.customer_unit_price,
    external_unit_price: summary.value.external_unit_price,
    reason: "",
  };
  error.value = "";
  editSheet.value = true;
}

async function saveEdit() {
  error.value = "";
  saving.value = true;
  try {
    await request(`/projects/${id.value}`, {
      method: "PATCH",
      data: {
        order_qty: form.value.order_qty ? Number(form.value.order_qty) : undefined,
        required_delivery_date: form.value.required_delivery_date || undefined,
        customer_unit_price: form.value.customer_unit_price || undefined,
        external_unit_price: form.value.external_unit_price || undefined,
        version: version.value,
        reason: form.value.reason || undefined,
      },
    });
    uni.showToast({ title: "已保存", icon: "none" });
    editSheet.value = false;
    load();
  } catch (e) {
    error.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function remove() {
  uni.showModal({
    title: "删除项目",
    content: "确定删除该项目吗？已流转到工单或出货环节的项目不能删除。",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/projects/${id.value}`, { method: "DELETE" });
        uni.showToast({ title: "已删除", icon: "none" });
        uni.navigateBack();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "删除失败", icon: "none" });
      }
    },
  });
}

onLoad((query) => {
  id.value = ((query as Record<string, string>) || {}).id || "";
  load();
});
</script>

<style scoped>
.sum-cell {
  flex: 1;
  background: #f8fafc;
  border-radius: 12rpx;
  padding: 20rpx 0;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.sum-num {
  font-size: 36rpx;
  font-weight: 700;
  color: #1d4ed8;
}
.sum-label {
  font-size: 22rpx;
  color: #94a3b8;
  margin-top: 6rpx;
}
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
