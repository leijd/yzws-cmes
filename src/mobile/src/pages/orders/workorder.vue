<template>
  <BrandNavBar title="工单详情" />
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!item" class="empty">工单不存在</view>
    <template v-else>
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title">{{ item.work_order_code }}</text>
              <text class="tag" :class="statusCls">{{ statusText }}</text>
            </view>
            <text class="muted">产品 {{ item.product_name || item.product_id || "--" }}</text>
          </view>
        </view>
        <view class="row gap8" style="margin-top: 16rpx">
          <text v-if="item.review_status === 'approved'" class="tag tag-green">已审核</text>
          <text v-else-if="item.review_status === 'rejected'" class="tag tag-red">已驳回</text>
          <text v-else-if="item.review_status === 'pending'" class="tag tag-orange">待审核</text>
          <text class="tag" :class="item.priority === 'urgent' ? 'tag-red' : item.priority === 'high' ? 'tag-orange' : 'tag-gray'">
            {{ priorityText }}
          </text>
          <text class="tag tag-purple">{{ item.report_mode === "A" ? "A 派工受控" : "B 自主选工序" }}</text>
        </view>
      </view>

      <view class="card">
        <view class="section-title" style="margin-top: 0">工单信息</view>
        <view class="kv"><text class="k">计划数量</text><text class="v strong">{{ item.planned_product_qty }}</text></view>
        <view class="kv"><text class="k">颜色</text><text class="v">{{ item.color || "--" }}</text></view>
        <view class="kv"><text class="k">计划开始</text><text class="v">{{ fmtDate(item.planned_start_at) }}</text></view>
        <view class="kv"><text class="k">计划完成</text><text class="v">{{ fmtDate(item.planned_end_at) }}</text></view>
        <view class="kv"><text class="k">包装要求</text><text class="v">{{ item.packaging_requirement || "--" }}</text></view>
        <view class="kv"><text class="k">特殊要求</text><text class="v">{{ item.special_requirement || "--" }}</text></view>
      </view>

      <view class="card" v-if="item.review_status === 'pending' && canReview">
        <view class="section-title" style="margin-top: 0">工单审核</view>
        <view class="field">
          <text class="field-label">审核意见（驳回必填）</text>
          <input class="input" v-model.trim="reviewReason" placeholder="审核意见" />
        </view>
        <view class="actions">
          <button class="btn btn-solid btn-sm" :disabled="saving" @click="review('approve')">审核通过</button>
          <button class="btn btn-danger btn-sm" :disabled="saving" @click="review('reject')">驳回</button>
        </view>
      </view>

      <view class="actions" style="margin-top: 24rpx">
        <button v-if="item.status === 'pending_release' && item.review_status === 'approved' && canRelease" class="btn btn-solid btn-sm" @click="release">下发工单</button>
        <button v-if="item.status === 'in_production' && canWrite" class="btn btn-outline btn-sm" @click="action('pause')">暂停</button>
        <button v-if="item.status === 'paused' && canWrite" class="btn btn-solid btn-sm" @click="action('resume')">恢复生产</button>
        <button v-if="(item.status === 'in_production' || item.status === 'paused') && canWrite" class="btn btn-solid btn-sm" @click="action('complete')">完工</button>
        <button v-if="item.status !== 'completed' && item.status !== 'cancelled' && canWrite" class="btn btn-danger btn-sm" @click="action('cancel')">取消</button>
        <button v-if="item.status === 'pending_release' && canWrite" class="btn btn-danger btn-sm" @click="remove">删除工单</button>
      </view>

      <view class="card" style="margin-top: 24rpx">
        <button class="btn btn-outline btn-block" style="margin-top: 0" @click="goTasks">查看本单任务与派工</button>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { auth } from "../../store/auth";
import { fmtDate } from "../../utils/format";

interface WorkOrder {
  id: string;
  work_order_code: string;
  product_id: string;
  product_name?: string;
  planned_product_qty: number;
  status: string;
  review_status?: string;
  priority: string;
  report_mode: string;
  color?: string;
  planned_start_at?: number;
  planned_end_at?: number;
  packaging_requirement?: string;
  special_requirement?: string;
  version: number;
}

const id = ref("");
const loading = ref(false);
const saving = ref(false);
const item = ref<WorkOrder | null>(null);
const reviewReason = ref("");

const canReview = computed(() => auth.hasPerm("work_order:review"));
const canRelease = computed(() => auth.hasPerm("work_order:release"));
const canWrite = computed(() => auth.hasPerm("work_order:write"));

const statusText = computed(() => {
  const s = item.value?.status || "";
  const map: Record<string, string> = {
    pending_release: "待下发",
    in_production: "生产中",
    paused: "已暂停",
    completed: "已完成",
    cancelled: "已取消",
  };
  return map[s] || s || "--";
});
const statusCls = computed(() => {
  const s = item.value?.status || "";
  return s === "in_production" ? "tag-blue" : s === "completed" ? "tag-green" : s === "paused" ? "tag-orange" : "tag-gray";
});
const priorityText = computed(() => {
  const p = item.value?.priority || "normal";
  return p === "urgent" ? "紧急" : p === "high" ? "高" : "普通";
});

async function load() {
  loading.value = true;
  try {
    const res = await request<WorkOrder>(`/work-orders/${id.value}`, { silent: true });
    item.value = res;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function review(decision: "approve" | "reject") {
  if (!item.value) return;
  if (decision === "reject" && !reviewReason.value) {
    uni.showToast({ title: "驳回必须填写原因", icon: "none" });
    return;
  }
  saving.value = true;
  try {
    await request(`/work-orders/${id.value}/review`, {
      method: "POST",
      data: { version: item.value.version, decision, reason: reviewReason.value || undefined },
    });
    uni.showToast({ title: decision === "approve" ? "已审核通过" : "已驳回", icon: "none" });
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "操作失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

async function release() {
  if (!item.value) return;
  uni.showModal({
    title: "下发工单",
    content: "下发后生成工序任务并锁定裁床与报工模式，确定下发吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/work-orders/${id.value}/release`, {
          method: "POST",
          data: { version: item.value!.version },
          idempotency: true,
        });
        uni.showToast({ title: "已下发", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "下发失败", icon: "none" });
      }
    },
  });
}

function action(act: "pause" | "resume" | "complete" | "cancel") {
  if (!item.value) return;
  const textMap: Record<string, string> = { pause: "暂停", resume: "恢复生产", complete: "完工", cancel: "取消" };
  uni.showModal({
    title: textMap[act],
    content: `确定${textMap[act]}工单「${item.value.work_order_code}」吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/work-orders/${id.value}/${act}`, {
          method: "POST",
          data: { version: item.value!.version },
        });
        uni.showToast({ title: "操作成功", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "操作失败", icon: "none" });
      }
    },
  });
}

function remove() {
  if (!item.value) return;
  uni.showModal({
    title: "删除工单",
    content: "仅未下发的工单可删除，确定删除吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/work-orders/${id.value}`, { method: "DELETE" });
        uni.showToast({ title: "已删除", icon: "none" });
        uni.navigateBack();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "删除失败", icon: "none" });
      }
    },
  });
}

function goTasks() {
  uni.navigateTo({ url: `/pages/tasks/list?work_order_id=${id.value}&work_order_code=${encodeURIComponent(item.value?.work_order_code || "")}` });
}

onLoad((query) => {
  id.value = ((query as Record<string, string>) || {}).id || "";
  if (auth.hasSession && !Object.keys(auth.permissions).length) auth.fetchPermissions();
  load();
});
</script>
