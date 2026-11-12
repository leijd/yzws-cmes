<template>
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!item" class="empty">唛架不存在</view>
    <template v-else>
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title">{{ item.marker_no }}</text>
              <text class="tag" :class="reviewCls">{{ reviewText }}</text>
            </view>
            <text class="muted">{{ item.product_name }} · {{ item.color }}</text>
          </view>
        </view>
        <view class="kv" style="margin-top: 16rpx"><text class="k">面料</text><text class="v">{{ item.material_name }}（{{ item.material_code }}）</text></view>
        <view class="kv"><text class="k">每唛件数</text><text class="v">{{ item.pieces_per_marker }}</text></view>
        <view class="kv"><text class="k">拉布层数</text><text class="v">{{ item.fabric_layers }}</text></view>
        <view class="kv"><text class="k">裁片总数</text><text class="v strong">{{ item.cut_qty }}</text></view>
        <view class="kv"><text class="k">唛架长度</text><text class="v">{{ item.marker_length_m }} 米</text></view>
        <view class="kv"><text class="k">预计用料</text><text class="v">{{ item.estimated_fabric_m || "--" }} 米</text></view>
        <view class="kv"><text class="k">利用率</text><text class="v">{{ utilizationText }}</text></view>
        <view class="kv"><text class="k">裁床单引用</text><text class="v">{{ item.reference_count || 0 }} 次</text></view>
        <view v-if="item.review_reason" class="kv"><text class="k">审核意见</text><text class="v" style="color: #b91c1c">{{ item.review_reason }}</text></view>
      </view>

      <view v-if="ratios.length" class="section-title" style="margin-top: 32rpx">尺码配比</view>
      <view class="card" v-if="ratios.length">
        <view v-for="r in ratios" :key="r.size" class="row between" style="padding: 12rpx 0">
          <text class="title" style="font-size: 28rpx">{{ r.size }}</text>
          <text class="muted">每唛 {{ r.ratio_qty }} 件</text>
        </view>
      </view>

      <view class="actions" style="margin-top: 24rpx">
        <button v-if="item.review_status !== 'approved'" class="btn btn-solid btn-sm" @click="submitReview">提交审核</button>
        <button v-if="item.review_status === 'pending' && canReview" class="btn btn-solid btn-sm" @click="openReview('approve')">审核通过</button>
        <button v-if="item.review_status === 'pending' && canReview" class="btn btn-danger btn-sm" @click="openReview('reject')">驳回</button>
        <button v-if="item.review_status === 'pending'" class="btn btn-danger btn-sm" @click="remove">删除</button>
      </view>
      <text class="muted" style="display: block; margin-top: 16rpx">唛架图上传与排版编辑请使用网页版。</text>

      <!-- 审核 -->
      <view v-if="reviewSheet" class="sheet-mask" @click="reviewSheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">{{ reviewDecision === "approve" ? "审核通过" : "驳回唛架" }}</view>
          <view class="field">
            <text class="field-label" :class="{ required: reviewDecision === 'reject' }">审核意见</text>
            <input class="input" v-model.trim="reviewReason" placeholder="驳回时必填" />
          </view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="saveReview">确认</button>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { auth } from "../../store/auth";

interface Marker {
  id: string; marker_no: string; product_name: string; color: string;
  material_code: string; material_name: string; pieces_per_marker: number;
  fabric_layers: number; cut_qty: number; marker_length_m: string;
  estimated_fabric_m?: string; utilization_rate?: string;
  review_status: string; review_reason?: string; reference_count?: number; version: number;
}

interface RatioItem { size: string; ratio_qty: number; sort_no: number }

const id = ref("");
const loading = ref(false);
const saving = ref(false);
const item = ref<Marker | null>(null);
const ratios = ref<RatioItem[]>([]);

const canReview = computed(() => auth.hasPerm("marker:review"));
const reviewSheet = ref(false);
const reviewDecision = ref<"approve" | "reject">("approve");
const reviewReason = ref("");

const reviewText = computed(() => {
  const s = item.value?.review_status || "";
  return s === "approved" ? "已通过" : s === "rejected" ? "已驳回" : "待审核";
});
const reviewCls = computed(() => {
  const s = item.value?.review_status || "";
  return s === "approved" ? "tag-green" : s === "rejected" ? "tag-red" : "tag-orange";
});
const utilizationText = computed(() => {
  const r = item.value?.utilization_rate;
  if (!r) return "--";
  const n = Number(r);
  return Number.isFinite(n) ? `${Math.round(n * 100)}%` : r;
});

async function load() {
  loading.value = true;
  try {
    const res = await request<Marker & { ratios?: RatioItem[] }>(`/markers/${id.value}`, { silent: true });
    item.value = res;
    ratios.value = res.ratios || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function submitReview() {
  if (!item.value) return;
  uni.showModal({
    title: "提交审核",
    content: "提交后唛架进入待审核状态，确定提交吗？（需已上传唛架图）",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/markers/${id.value}/submit-review`, {
          method: "POST",
          data: { version: item.value!.version },
        });
        uni.showToast({ title: "已提交审核", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "提交失败", icon: "none" });
      }
    },
  });
}

function openReview(decision: "approve" | "reject") {
  reviewDecision.value = decision;
  reviewReason.value = "";
  reviewSheet.value = true;
}

async function saveReview() {
  if (!item.value) return;
  if (reviewDecision.value === "reject" && !reviewReason.value) {
    uni.showToast({ title: "驳回必须填写意见", icon: "none" });
    return;
  }
  saving.value = true;
  try {
    await request(`/markers/${id.value}/review`, {
      method: "POST",
      data: {
        version: item.value.version,
        decision: reviewDecision.value,
        reason: reviewReason.value || undefined,
      },
    });
    uni.showToast({ title: "审核完成", icon: "none" });
    reviewSheet.value = false;
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "审核失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

function remove() {
  if (!item.value) return;
  uni.showModal({
    title: "删除唛架",
    content: "仅待审核的唛架可删除，确定删除吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/markers/${id.value}`, { method: "DELETE" });
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
  if (auth.hasSession && !Object.keys(auth.permissions).length) auth.fetchPermissions();
  load();
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
</style>
