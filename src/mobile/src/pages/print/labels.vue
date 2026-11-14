<template>
  <BrandNavBar title="扎标签" />
  <view class="page">
    <view class="card">
      <view class="field" style="padding-top: 0">
        <text class="field-label required">选择工单</text>
        <picker :range="workOrderLabels" :value="woIndex" @change="onWoChange">
          <view class="picker-box">{{ workOrderLabels[woIndex] || "选择工单后加载扎标签" }}</view>
        </picker>
      </view>
    </view>

    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!items.length && loaded" class="empty">该工单暂无扎标签（需先完成裁床确认生成扎与菲票）</view>

    <view v-for="l in items" :key="l.bundle_id" class="card label-card">
      <view class="label-head">
        <view class="col">
          <text class="label-no">{{ l.bundle_no }} 扎</text>
          <text class="label-sub">{{ l.work_order_code }} · {{ l.product_name }}</text>
        </view>
        <image v-if="l.qr_image" :src="l.qr_image" mode="aspectFit" class="label-qr" @click="previewQr(l)" />
      </view>
      <view class="label-grid">
        <view class="label-cell"><text class="lk">颜色</text><text class="lv">{{ l.color || "--" }}</text></view>
        <view class="label-cell"><text class="lk">尺码</text><text class="lv">{{ l.size || "--" }}</text></view>
        <view class="label-cell"><text class="lk">件数</text><text class="lv">{{ l.qty }}</text></view>
        <view class="label-cell"><text class="lk">床号</text><text class="lv">{{ l.bed_no || "--" }}</text></view>
        <view class="label-cell"><text class="lk">唛架号</text><text class="lv">{{ l.marker_no || "--" }}</text></view>
        <view class="label-cell"><text class="lk">裁床号</text><text class="lv">{{ l.cutting_no || "--" }}</text></view>
      </view>
      <view v-if="l.parts && l.parts.length" class="parts">
        <text class="parts-title">部位（{{ l.parts.length }}）</text>
        <view class="parts-row">
          <text v-for="p in l.parts" :key="p.part_code" class="part-chip">{{ p.part_name }} ×{{ p.qty }}</text>
        </view>
      </view>
      <text class="muted" style="margin-top: 12rpx">公开码 {{ l.public_code || "--" }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface LabelPart { part_code: string; part_name: string; qty: number; ticket_id: string; public_code: string }
interface BundleLabel {
  bundle_id: string; bundle_no: string; ticket_id: string; public_code: string;
  work_order_code: string; product_name: string; color: string; size: string; qty: number;
  bed_no: string; marker_no: string; cutting_no: string;
  parts: LabelPart[]; qr_image: string;
}

interface WorkOrderOption { id: string; work_order_code: string }

const workOrders = ref<WorkOrderOption[]>([]);
const workOrderLabels = computed(() => workOrders.value.map((w) => w.work_order_code));
const woIndex = ref(0);
const items = ref<BundleLabel[]>([]);
const loading = ref(false);
const loaded = ref(false);

async function loadLabels() {
  const wo = workOrders.value[woIndex.value];
  if (!wo) return;
  loading.value = true;
  try {
    const res = await request<{ items: BundleLabel[] }>(`/bundle-labels?work_order_id=${wo.id}`, { silent: true });
    items.value = res.items || [];
    loaded.value = true;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function onWoChange(e: { detail: { value: number } }) {
  woIndex.value = e.detail.value;
  loadLabels();
}

function previewQr(l: BundleLabel) {
  if (l.qr_image) {
    uni.previewImage({ urls: [l.qr_image] });
  }
}

onLoad(async () => {
  try {
    const res = await request<{ items: WorkOrderOption[] }>("/work-orders", { silent: true });
    workOrders.value = res.items || [];
    if (workOrders.value.length) loadLabels();
  } catch {
    /* 工单列表加载失败 */
  }
});
</script>

<style scoped>
.label-card {
  border: 1px solid #e2e8f0;
}
.label-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px dashed #e2e8f0;
  padding-bottom: 20rpx;
  margin-bottom: 20rpx;
}
.label-no {
  font-size: 38rpx;
  font-weight: 800;
  color: #17255f;
}
.label-sub {
  font-size: 22rpx;
  color: #94a3b8;
  margin-top: 6rpx;
}
.label-qr {
  width: 128rpx;
  height: 128rpx;
  border-radius: 8rpx;
  background: #fff;
}
.label-grid {
  display: flex;
  flex-wrap: wrap;
}
.label-cell {
  width: 33.33%;
  padding: 10rpx 0;
}
.lk {
  display: block;
  font-size: 20rpx;
  color: #94a3b8;
}
.lv {
  display: block;
  font-size: 28rpx;
  color: #1e293b;
  margin-top: 4rpx;
}
.parts {
  border-top: 1px dashed #e2e8f0;
  margin-top: 20rpx;
  padding-top: 16rpx;
}
.parts-title {
  font-size: 22rpx;
  color: #64748b;
}
.parts-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12rpx;
  margin-top: 12rpx;
}
.part-chip {
  font-size: 22rpx;
  background: #f1f5f9;
  color: #475569;
  border-radius: 999rpx;
  padding: 8rpx 20rpx;
}
</style>
