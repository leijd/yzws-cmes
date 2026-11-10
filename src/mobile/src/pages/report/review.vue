<template>
  <view class="page">
    <view v-if="!loading && !items.length" class="empty">没有待审批的报工</view>

    <view v-for="item in items" :key="item.id" class="card">
      <view class="row between">
        <text class="process">{{ item.process_name }}</text>
        <text class="qty">合格 {{ item.good_qty }} / 次品 {{ item.defect_qty }}</text>
      </view>
      <view class="row">
        <text class="label">员工</text>
        <text class="value">{{ item.employee_name }}</text>
      </view>
      <view class="row">
        <text class="label">时间</text>
        <text class="value">{{ formatDateTime(item.occurred_at) }}</text>
      </view>
      <view v-if="item.piecework_amount" class="row">
        <text class="label">计件金额</text>
        <text class="value amount">¥ {{ item.piecework_amount }}</text>
      </view>
      <view class="actions">
        <button class="reject" size="mini" @click="reject(item.id)">驳回</button>
        <button class="approve" size="mini" @click="approve(item.id)">通过</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { formatDateTime } from "../../utils/format";

interface ReportItem {
  id: string;
  process_name: string;
  employee_name: string;
  good_qty: number;
  defect_qty: number;
  occurred_at: number;
  piecework_amount?: string | null;
}

const items = ref<ReportItem[]>([]);
const loading = ref(false);

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: ReportItem[] }>("/report-reviews", {
      silent: true,
    });
    items.value = res.items || [];
  } catch {
    items.value = [];
  } finally {
    loading.value = false;
  }
}

async function approve(id: string) {
  try {
    await request(`/report-reviews/${id}/approve`, {
      method: "POST",
      idempotency: true,
      data: {},
    });
    uni.showToast({ title: "已通过", icon: "success" });
    load();
  } catch {
    // 错误已 toast
  }
}

function reject(id: string) {
  uni.showModal({
    title: "驳回报工",
    editable: true,
    placeholderText: "请输入驳回原因（必填）",
    success: async (res) => {
      if (!res.confirm) return;
      const reason = (res.content || "").trim();
      if (!reason) {
        uni.showToast({ title: "驳回原因必填", icon: "none" });
        return;
      }
      try {
        await request(`/report-reviews/${id}/reject`, {
          method: "POST",
          idempotency: true,
          data: { reason },
        });
        uni.showToast({ title: "已驳回", icon: "success" });
        load();
      } catch {
        // 错误已 toast
      }
    },
  });
}

onShow(() => {
  load();
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 24rpx 32rpx;
  box-sizing: border-box;
}
.empty {
  text-align: center;
  color: #a0aec0;
  font-size: 28rpx;
  padding: 120rpx 0;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 24rpx 32rpx;
  margin-bottom: 20rpx;
}
.row {
  display: flex;
  padding: 6rpx 0;
  font-size: 26rpx;
}
.between {
  justify-content: space-between;
  border-bottom: 1px solid #edf2f7;
  padding-bottom: 12rpx;
  margin-bottom: 10rpx;
}
.process {
  font-size: 30rpx;
  font-weight: 600;
  color: #2d3748;
}
.qty {
  font-size: 26rpx;
  color: #718096;
}
.label {
  color: #718096;
  width: 150rpx;
}
.value {
  color: #2d3748;
}
.amount {
  color: #38a169;
  font-weight: 600;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 20rpx;
  margin-top: 16rpx;
}
.approve {
  background: #38a169;
  color: #fff;
}
.reject {
  background: #fff;
  color: #e53e3e;
  border: 1px solid #feb2b2;
}
</style>
