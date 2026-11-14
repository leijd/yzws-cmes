<template>
  <BrandNavBar title="产品详情" />
  <view class="page">
    <view v-if="loading" class="empty">加载中…</view>
    <view v-else-if="!product" class="empty">产品不存在</view>
    <template v-else>
      <view class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title">{{ product.name }}</text>
              <text class="tag" :class="product.status === 'active' ? 'tag-green' : 'tag-gray'">{{ product.status === "active" ? "上架" : "草稿/隐藏" }}</text>
            </view>
            <text class="muted">{{ product.product_code }} · {{ product.spec || "无规格" }} · {{ product.unit }} · v{{ product.version }}</text>
          </view>
        </view>
        <view class="row between" style="margin-top: 16rpx">
          <text class="muted">售价 {{ product.sale_price || "--" }} · 理论工价 {{ theoreticalPrice }}</text>
        </view>
      </view>

      <view class="row between" style="margin: 24rpx 0 16rpx">
        <text class="section-title" style="margin: 0">工序配置（{{ processes.length }} 道）</text>
        <button class="btn btn-solid btn-sm" @click="openProcessEdit">配置工序</button>
      </view>
      <view v-if="!processes.length" class="empty">尚未配置工序，工单无法下发</view>
      <view v-for="p in processes" :key="p.id" class="card">
        <view class="row between">
          <view class="grow">
            <text class="title" style="font-size: 28rpx">{{ p.sequence_no }}. {{ p.name }}</text>
            <text class="muted">{{ p.process_code }} · 工价 {{ p.unit_price }} · 配比 {{ p.report_ratio }}{{ p.is_critical ? " · 关键工序" : "" }}</text>
          </view>
        </view>
      </view>

      <view class="actions" style="margin-top: 24rpx">
        <button class="btn btn-outline btn-sm" @click="toggleVisibility">{{ product.status === "active" ? "隐藏产品" : "上架产品" }}</button>
        <button class="btn btn-danger btn-sm" @click="remove">删除产品</button>
      </view>

      <!-- 工序编辑 -->
      <view v-if="processSheet" class="sheet-mask" @click="processSheet = false">
        <view class="sheet" @click.stop>
          <view class="row between" style="margin-bottom: 24rpx">
            <text class="title">配置工序</text>
            <button class="btn btn-outline btn-sm" @click="addRoute">＋ 添加行</button>
          </view>
          <view v-for="(route, i) in routeForm" :key="i" class="route-card">
            <view class="row between" style="margin-bottom: 12rpx">
              <text class="muted">第 {{ i + 1 }} 道</text>
              <text v-if="routeForm.length > 1" class="remove" @click="routeForm.splice(i, 1)">删除</text>
            </view>
            <view class="field">
              <text class="field-label required">工序</text>
              <picker :range="processDefNames" :value="route.processIndex" @change="route.processIndex = $event.detail.value">
                <view class="picker-box">{{ processDefNames[route.processIndex] || "选择工序" }}</view>
              </picker>
            </view>
            <view class="row gap8">
              <view class="field grow">
                <text class="field-label required">工价</text>
                <input class="input" v-model="route.unit_price" placeholder="0.00" />
              </view>
              <view class="field grow">
                <text class="field-label required">配比</text>
                <input class="input" v-model="route.report_ratio" placeholder="1" />
              </view>
            </view>
            <view class="field">
              <text class="field-label">标准工时（秒）</text>
              <input class="input" v-model="route.standard_work_seconds" type="number" placeholder="0" />
            </view>
          </view>
          <view v-if="formError" class="error-text">{{ formError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="saveProcesses">{{ saving ? "保存中…" : "保存工序配置" }}</button>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Product {
  id: string; product_code: string; name: string; spec?: string; unit: string;
  status: string; version: number; sale_price?: string;
}
interface Process {
  id: string; process_id: string; process_code: string; name: string; sequence_no: number;
  unit_price: string; report_ratio: string; is_critical: boolean;
}
interface ProcessDef {
  id: string; process_code: string; name: string; default_unit_price: string; default_report_ratio: string;
}
interface RouteForm {
  processIndex: number;
  unit_price: string;
  report_ratio: string;
  standard_work_seconds: string;
}

const id = ref("");
const loading = ref(false);
const saving = ref(false);
const product = ref<Product | null>(null);
const processes = ref<Process[]>([]);
const theoreticalPrice = ref("0");
const processDefs = ref<ProcessDef[]>([]);
const processDefNames = ref<string[]>([]);

const processSheet = ref(false);
const formError = ref("");
const routeForm = ref<RouteForm[]>([]);

async function load() {
  loading.value = true;
  try {
    const [p, routes] = await Promise.all([
      request<Product>(`/products/${id.value}`, { silent: true }),
      request<{ items: Process[]; theoretical_price: string }>(`/products/${id.value}/processes`, { silent: true }),
    ]);
    product.value = p;
    processes.value = routes.items || [];
    theoreticalPrice.value = routes.theoretical_price || "0";
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadProcessDefs() {
  try {
    const res = await request<{ items: ProcessDef[] }>("/process-definitions", { silent: true });
    processDefs.value = res.items || [];
    processDefNames.value = processDefs.value.map((d) => `${d.process_code} ${d.name}`);
  } catch {
    processDefs.value = [];
  }
}

function openProcessEdit() {
  routeForm.value = processes.value.map((p) => ({
    processIndex: Math.max(0, processDefs.value.findIndex((d) => d.id === p.process_id)),
    unit_price: p.unit_price,
    report_ratio: p.report_ratio,
    standard_work_seconds: "0",
  }));
  if (!routeForm.value.length) addRoute();
  formError.value = "";
  processSheet.value = true;
}

function addRoute() {
  routeForm.value.push({ processIndex: 0, unit_price: "", report_ratio: "1", standard_work_seconds: "0" });
}

async function saveProcesses() {
  formError.value = "";
  if (!product.value) return;
  const items = routeForm.value.map((route, i) => {
    const def = processDefs.value[route.processIndex];
    return {
      process_id: def?.id || "",
      sequence_no: i + 1,
      unit_price: route.unit_price || "0",
      report_ratio: route.report_ratio || "1",
      standard_work_seconds: Number(route.standard_work_seconds || 0),
    };
  });
  for (const [i, item] of items.entries()) {
    if (!item.process_id) { formError.value = `第 ${i + 1} 道请选择工序`; return; }
  }
  saving.value = true;
  try {
    await request(`/products/${id.value}/processes`, {
      method: "PUT",
      data: { version: product.value.version, items },
    });
    uni.showToast({ title: "工序已保存", icon: "none" });
    processSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function toggleVisibility() {
  if (!product.value) return;
  const target = product.value.status !== "active";
  uni.showModal({
    title: target ? "上架产品" : "隐藏产品",
    content: `确定${target ? "上架" : "隐藏"}产品「${product.value.name}」吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/products/${id.value}/visibility`, {
          method: "POST",
          data: { version: product.value!.version, visible: target },
        });
        uni.showToast({ title: "已更新", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "操作失败", icon: "none" });
      }
    },
  });
}

function remove() {
  if (!product.value) return;
  uni.showModal({
    title: "删除产品",
    content: "已投入生产的产品不能删除，确定删除吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/products/${id.value}`, { method: "DELETE" });
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
  loadProcessDefs().then(load);
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 90vh; overflow-y: auto; box-sizing: border-box; }
.route-card { background: #f8fafc; border-radius: 12rpx; padding: 20rpx; margin-bottom: 16rpx; }
.remove { color: #b91c1c; font-size: 24rpx; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
