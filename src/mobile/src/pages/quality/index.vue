<template>
  <BrandNavBar title="质检与库存" />
  <view class="page">
    <view class="chips">
      <view v-for="t in tabs" :key="t.value" class="chip" :class="{ on: tab === t.value }" @click="switchTab(t.value)">{{ t.label }}</view>
    </view>

    <!-- 尾检 -->
    <template v-if="tab === 'final'">
      <view class="row between" style="margin-bottom: 16rpx">
        <text class="section-title" style="margin: 0">尾检记录</text>
        <button class="btn btn-solid btn-sm" @click="finalSheet = true">＋ 新建尾检</button>
      </view>
      <view v-if="loading && !finalItems.length" class="empty">加载中…</view>
      <view v-else-if="!finalItems.length" class="empty">暂无尾检记录</view>
      <view v-for="f in finalItems" :key="f.id" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">{{ f.inspection_no }}</text>
              <text class="tag" :class="f.status === 'confirmed' ? 'tag-green' : 'tag-orange'">{{ f.status === "confirmed" ? "已确认" : "待确认" }}</text>
            </view>
            <text class="muted">检 {{ f.inspected_qty }} · 合格 {{ f.passed_qty }} · 返工 {{ f.rework_qty }} · 报废 {{ f.scrapped_qty }}</text>
          </view>
        </view>
        <view class="actions" v-if="f.status === 'pending'">
          <button class="btn btn-solid btn-sm" @click="confirmFinal(f)">确认</button>
        </view>
      </view>
    </template>

    <!-- 成品入库 -->
    <template v-else-if="tab === 'receipts'">
      <view class="row between" style="margin-bottom: 16rpx">
        <text class="section-title" style="margin: 0">成品入库单</text>
        <button class="btn btn-solid btn-sm" @click="openReceiptCreate">＋ 新建入库单</button>
      </view>
      <view v-if="loading && !receipts.length" class="empty">加载中…</view>
      <view v-else-if="!receipts.length" class="empty">暂无入库单</view>
      <view v-for="r in receipts" :key="r.id" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">{{ r.receipt_no }}</text>
              <text class="tag" :class="receiptStatusCls(r.status)">{{ receiptStatusText(r.status) }}</text>
            </view>
            <text class="muted">数量 {{ r.qty }}</text>
          </view>
        </view>
        <view class="actions">
          <button v-if="r.status === 'draft'" class="btn btn-outline btn-sm" @click="receiptAction(r, 'submit')">提交</button>
          <button v-if="r.status === 'pending_quality'" class="btn btn-outline btn-sm" @click="receiptAction(r, 'confirm-quality')">质检确认</button>
          <button v-if="r.status === 'pending_review'" class="btn btn-solid btn-sm" @click="receiptAction(r, 'approve')">入库审批</button>
        </view>
      </view>
    </template>

    <!-- 出货 -->
    <template v-else-if="tab === 'shipments'">
      <view class="row between" style="margin-bottom: 16rpx">
        <text class="section-title" style="margin: 0">出货单</text>
        <view class="row gap8">
          <button class="btn btn-outline btn-sm" @click="warehouseSheet = true">＋ 仓库</button>
          <button class="btn btn-solid btn-sm" @click="openShipmentCreate">＋ 新建出货单</button>
        </view>
      </view>
      <view v-if="loading && !shipments.length" class="empty">加载中…</view>
      <view v-else-if="!shipments.length" class="empty">暂无出货单</view>
      <view v-for="s in shipments" :key="s.id" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">{{ s.shipment_no }}</text>
              <text class="tag" :class="s.status === 'confirmed' ? 'tag-green' : 'tag-orange'">{{ s.status === "confirmed" ? "已确认" : "草稿" }}</text>
            </view>
            <text class="muted">数量 {{ s.qty }}</text>
          </view>
        </view>
        <view class="actions">
          <button v-if="s.status === 'draft'" class="btn btn-solid btn-sm" @click="confirmShipment(s)">确认出货</button>
          <button v-if="s.status === 'confirmed'" class="btn btn-outline btn-sm" @click="openReturn(s)">退货</button>
        </view>
      </view>
    </template>

    <!-- 退货记录 -->
    <template v-else-if="tab === 'returns'">
      <view class="row between" style="margin-bottom: 16rpx">
        <text class="section-title" style="margin: 0">退货记录</text>
      </view>
      <view v-if="loading && !returns.length" class="empty">加载中…</view>
      <view v-else-if="!returns.length" class="empty">暂无退货记录</view>
      <view v-for="r in returns" :key="r.return_no" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">{{ r.return_no }}</text>
              <text class="tag" :class="r.status === 'posted' ? 'tag-green' : 'tag-orange'">{{ r.status === "posted" ? "已入账" : r.status }}</text>
            </view>
            <text class="muted">出货单 {{ r.shipment_no }} · {{ r.bundle_no }} 扎 · {{ r.color }} {{ r.size }} · 数量 {{ r.qty }}</text>
          </view>
        </view>
        <text v-if="r.reason" class="muted" style="margin-top: 8rpx">原因：{{ r.reason }}</text>
      </view>
    </template>

    <!-- 扎包查询 -->
    <template v-else-if="tab === 'bundles'">
      <view class="search-bar">
        <input class="input" v-model.trim="bundleKeyword" placeholder="扎号 / 颜色 / 尺码 搜索" @confirm="filterBundles" />
        <button class="btn btn-outline" style="width: 160rpx; height: 80rpx; line-height: 80rpx" @click="filterBundles">查询</button>
      </view>
      <view v-if="loading && !bundles.length" class="empty">加载中…</view>
      <view v-else-if="!filteredBundles.length" class="empty">暂无扎包</view>
      <view v-for="b in filteredBundles" :key="b.id" class="card row between">
        <view class="grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ b.bundle_no }} 扎</text>
            <text class="tag tag-blue">{{ b.status }}</text>
          </view>
          <text class="muted">{{ b.color }} {{ b.size }} · 床号 {{ b.bed_no || "--" }}</text>
        </view>
        <text class="num" style="font-size: 34rpx">{{ b.current_qty }}</text>
      </view>
    </template>

    <!-- 库存汇总 -->
    <template v-else>
      <view v-if="loading && !inventory.length" class="empty">加载中…</view>
      <view v-else-if="!inventory.length" class="empty">暂无库存数据</view>
      <view v-if="alertCount" class="card" style="border: 1px solid #fecaca">
        <text class="title" style="color: #b91c1c; font-size: 28rpx">⚠ {{ alertCount }} 项库存预警（缺料/低库存）</text>
      </view>
      <view v-for="v in inventory" :key="v.material_id + v.warehouse_id" class="card">
        <view class="row between">
          <view class="grow">
            <text class="title" style="font-size: 28rpx">{{ v.material_name }}</text>
            <text class="muted">{{ v.material_code }} · {{ v.specification || "无规格" }} · {{ v.warehouse_name }}</text>
          </view>
          <view class="col" style="align-items: flex-end">
            <text class="num" style="font-size: 34rpx" :class="{ 'text-red': v.alert_status !== 'normal' }">{{ Number(v.qty) }}</text>
            <text class="muted">{{ v.unit }}</text>
          </view>
        </view>
        <view class="row between" style="margin-top: 12rpx">
          <text class="muted">安全库存 {{ v.safety_stock }}</text>
          <text class="tag" :class="v.alert_status === 'out_of_stock' ? 'tag-red' : v.alert_status === 'low' ? 'tag-orange' : 'tag-green'">
            {{ v.alert_status === "out_of_stock" ? "缺料" : v.alert_status === "low" ? "低库存" : "正常" }}
          </text>
        </view>
      </view>
    </template>

    <!-- 新建尾检弹层 -->
    <view v-if="finalSheet" class="sheet-mask" @click="finalSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建尾检</view>
        <view class="field">
          <text class="field-label required">尾检单号</text>
          <input class="input" v-model.trim="finalForm.inspection_no" placeholder="如 FI-0001" />
        </view>
        <view class="field">
          <text class="field-label required">工单</text>
          <picker :range="workOrderLabels" :value="finalWoIndex" @change="finalWoIndex = $event.detail.value">
            <view class="picker-box">{{ workOrderLabels[finalWoIndex] || "选择工单" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">扎</text>
          <picker :range="bundleLabelsOfWo" :value="finalBundleIndex" @change="finalBundleIndex = $event.detail.value">
            <view class="picker-box">{{ bundleLabelsOfWo[finalBundleIndex] || "选择扎" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">检验数</text>
          <input class="input" v-model="finalForm.inspected_qty" type="number" />
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label required">合格数</text>
            <input class="input" v-model="finalForm.passed_qty" type="number" />
          </view>
          <view class="field grow">
            <text class="field-label">返工数</text>
            <input class="input" v-model="finalForm.rework_qty" type="number" />
          </view>
          <view class="field grow">
            <text class="field-label">报废数</text>
            <input class="input" v-model="finalForm.scrapped_qty" type="number" />
          </view>
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="finalForm.remark" placeholder="可选" />
        </view>
        <view v-if="finalError" class="error-text">{{ finalError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveFinal">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>
    <!-- 退货弹层 -->
    <view v-if="returnSheet" class="sheet-mask" @click="returnSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">出货退货 · {{ returnTarget?.shipment_no }}</view>
        <view v-if="returnLinesLoading" class="empty">加载中…</view>
        <template v-else>
          <view class="field">
            <text class="field-label required">出货扎</text>
            <picker :range="returnLineLabels" :value="returnLineIndex" @change="returnLineIndex = $event.detail.value">
              <view class="picker-box">{{ returnLineLabels[returnLineIndex] || "选择出货扎" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label required">退货数量</text>
            <input class="input" v-model="returnQty" type="number" />
          </view>
          <view class="field">
            <text class="field-label required">退货原因</text>
            <input class="input" v-model.trim="returnReason" placeholder="如 客户验货不合格" />
          </view>
          <view v-if="returnError" class="error-text">{{ returnError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="submitReturn">{{ saving ? "提交中…" : "提交退货" }}</button>
        </template>
      </view>
    </view>

    <!-- 新建入库单 -->
    <view v-if="receiptSheet" class="sheet-mask" @click="receiptSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建成品入库单</view>
        <view class="field">
          <text class="field-label required">入库单号</text>
          <input class="input" v-model.trim="receiptForm.receipt_no" placeholder="如 FR-0001" />
        </view>
        <view class="field">
          <text class="field-label required">工单</text>
          <picker :range="workOrderLabels" :value="receiptWoIndex" @change="receiptWoIndex = $event.detail.value">
            <view class="picker-box">{{ workOrderLabels[receiptWoIndex] || "选择工单" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">仓库</text>
          <picker :range="warehouseNames" :value="warehouseIndex" @change="warehouseIndex = $event.detail.value">
            <view class="picker-box">{{ warehouseNames[warehouseIndex] || "选择仓库" }}</view>
          </picker>
        </view>
        <view class="divider"></view>
        <view class="row between" style="margin-bottom: 12rpx">
          <text class="section-title" style="margin: 0">入库明细</text>
          <button class="btn btn-outline btn-sm" @click="addReceiptLine">＋ 添加行</button>
        </view>
        <view v-for="(line, i) in receiptForm.lines" :key="i" class="line-card">
          <view class="row between" style="margin-bottom: 8rpx">
            <text class="muted">第 {{ i + 1 }} 行</text>
            <text v-if="receiptForm.lines.length > 1" class="remove" @click="receiptForm.lines.splice(i, 1)">删除</text>
          </view>
          <view class="field">
            <text class="field-label required">扎（选已确认尾检）</text>
            <picker :range="inspectionLabels" :value="line.inspectionIndex" @change="line.inspectionIndex = $event.detail.value">
              <view class="picker-box">{{ inspectionLabels[line.inspectionIndex] || "选择尾检记录" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label required">入库数量</text>
            <input class="input" v-model="line.qty" type="number" />
          </view>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveReceipt">{{ saving ? "保存中…" : "保存入库单" }}</button>
      </view>
    </view>

    <!-- 新建出货单 -->
    <view v-if="shipmentSheet" class="sheet-mask" @click="shipmentSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建出货单</view>
        <view class="field">
          <text class="field-label required">出货单号</text>
          <input class="input" v-model.trim="shipmentForm.shipment_no" placeholder="如 SH-0001" />
        </view>
        <view class="field">
          <text class="field-label required">项目</text>
          <picker :range="projectLabels" :value="projectIndex" @change="projectIndex = $event.detail.value">
            <view class="picker-box">{{ projectLabels[projectIndex] || "选择项目" }}</view>
          </picker>
        </view>
        <view class="divider"></view>
        <view class="row between" style="margin-bottom: 12rpx">
          <text class="section-title" style="margin: 0">出货明细</text>
          <button class="btn btn-outline btn-sm" @click="addShipmentLine">＋ 添加行</button>
        </view>
        <view v-for="(line, i) in shipmentForm.lines" :key="i" class="line-card">
          <view class="row between" style="margin-bottom: 8rpx">
            <text class="muted">第 {{ i + 1 }} 行</text>
            <text v-if="shipmentForm.lines.length > 1" class="remove" @click="shipmentForm.lines.splice(i, 1)">删除</text>
          </view>
          <view class="field">
            <text class="field-label required">工单</text>
            <picker :range="workOrderLabels" :value="line.woIndex" @change="line.woIndex = $event.detail.value">
              <view class="picker-box">{{ workOrderLabels[line.woIndex] || "选择工单" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label required">扎</text>
            <picker :range="bundleLabelsFor(line.woIndex)" :value="line.bundleIndex" @change="line.bundleIndex = $event.detail.value">
              <view class="picker-box">{{ bundleLabelsFor(line.woIndex)[line.bundleIndex] || "选择扎" }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label required">数量</text>
            <input class="input" v-model="line.qty" type="number" />
          </view>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveShipment">{{ saving ? "保存中…" : "保存出货单" }}</button>
      </view>
    </view>

    <!-- 新建仓库 -->
    <view v-if="warehouseSheet" class="sheet-mask" @click="warehouseSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建成品仓</view>
        <view class="field">
          <text class="field-label required">仓库编码</text>
          <input class="input" v-model.trim="warehouseForm.code" placeholder="如 WH-F01" />
        </view>
        <view class="field">
          <text class="field-label required">仓库名称</text>
          <input class="input" v-model.trim="warehouseForm.name" placeholder="如 成品仓 1 号" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveWarehouse">{{ saving ? "保存中…" : "保存仓库" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad, onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface FinalItem {
  id: string; inspection_no: string; work_order_id: string; bundle_id: string;
  inspected_qty: number; passed_qty: number; rework_qty: number; scrapped_qty: number;
  status: string; version: number;
}
interface ReceiptItem { id: string; receipt_no: string; status: string; version: number; qty: number }
interface ShipmentItem { id: string; shipment_no: string; status: string; version: number; qty: number }
interface BundleItem { id: string; work_order_id: string; bundle_no: string; bed_no: string; color: string; size: string; current_qty: number; status: string }
interface InventoryItem {
  material_id: string; material_code: string; material_name: string; specification?: string; unit: string;
  safety_stock: string; warehouse_id: string; warehouse_name: string; qty: string; alert_status: string;
}
interface WorkOrderOption { id: string; work_order_code: string }

const tabs = [
  { label: "尾检", value: "final" },
  { label: "成品入库", value: "receipts" },
  { label: "出货", value: "shipments" },
  { label: "退货记录", value: "returns" },
  { label: "扎包查询", value: "bundles" },
  { label: "库存汇总", value: "inventory" },
];
const tab = ref("final");
const loading = ref(false);
const saving = ref(false);

/* 退货 */
interface ReturnItem {
  return_no: string; shipment_no: string; project_code: string; work_order_code: string;
  bundle_no: string; color: string; size: string; qty: number; reason?: string; status: string;
}
interface ShipmentLine { bundle_id: string; bundle_no: string; color: string; size: string; returnable_qty: number }
const returns = ref<ReturnItem[]>([]);
const returnSheet = ref(false);
const returnTarget = ref<ShipmentItem | null>(null);
const returnLines = ref<ShipmentLine[]>([]);
const returnLinesLoading = ref(false);
const returnLineIndex = ref(0);
const returnQty = ref("");
const returnReason = ref("");
const returnError = ref("");
const returnLineLabels = computed(() =>
  returnLines.value.map((l) => `${l.bundle_no} 扎 · ${l.color} ${l.size} · 可退 ${l.returnable_qty}`)
);

/* 选项数据 */
interface WarehouseOption { id: string; name: string }
interface ProjectOption { id: string; project_code: string }
const warehouses = ref<WarehouseOption[]>([]);
const warehouseNames = computed(() => warehouses.value.map((w) => w.name));
const warehouseIndex = ref(0);
const projects = ref<ProjectOption[]>([]);
const projectLabels = computed(() => projects.value.map((p) => p.project_code));
const projectIndex = ref(0);

/* 新建入库 */
interface ReceiptLineForm { inspectionIndex: number; qty: string }
const receiptSheet = ref(false);
const receiptWoIndex = ref(0);
const receiptForm = ref({ receipt_no: "", lines: [{ inspectionIndex: 0, qty: "" }] as ReceiptLineForm[] });
const confirmedInspections = computed(() => finalItems.value.filter((f) => f.status === "confirmed"));
const inspectionLabels = computed(() =>
  confirmedInspections.value.map((f) => `${f.inspection_no} · 合格 ${f.passed_qty}`)
);

/* 新建出货 */
interface ShipmentLineForm { woIndex: number; bundleIndex: number; qty: string }
const shipmentSheet = ref(false);
const shipmentForm = ref({ shipment_no: "", lines: [{ woIndex: 0, bundleIndex: 0, qty: "" }] as ShipmentLineForm[] });

/* 新建仓库 */
const warehouseSheet = ref(false);
const warehouseForm = ref({ code: "", name: "" });

const formError = ref("");

const finalItems = ref<FinalItem[]>([]);
const receipts = ref<ReceiptItem[]>([]);
const shipments = ref<ShipmentItem[]>([]);
const bundles = ref<BundleItem[]>([]);
const inventory = ref<InventoryItem[]>([]);
const alertCount = ref(0);

const workOrders = ref<WorkOrderOption[]>([]);
const workOrderLabels = computed(() => workOrders.value.map((w) => w.work_order_code));
const finalWoIndex = ref(0);
const finalBundleIndex = ref(0);
const bundleLabelsOfWo = computed(() =>
  bundles.value.filter((b) => b.work_order_id === workOrders.value[finalWoIndex.value]?.id).map((b) => `${b.bundle_no} 扎 · ${b.color} ${b.size} · ${b.current_qty} 件`)
);

const bundleKeyword = ref("");
const filteredBundles = computed(() => {
  if (!bundleKeyword.value) return bundles.value;
  const k = bundleKeyword.value.toLowerCase();
  return bundles.value.filter((b) => `${b.bundle_no}${b.color}${b.size}`.toLowerCase().includes(k));
});

const finalSheet = ref(false);
const finalError = ref("");
const finalForm = ref({ inspection_no: "", inspected_qty: "", passed_qty: "", rework_qty: "0", scrapped_qty: "0", remark: "" });

function receiptStatusText(s: string) {
  const map: Record<string, string> = { draft: "草稿", pending_quality: "待质检", pending_review: "待审批", approved: "已入库", reversed: "已反冲" };
  return map[s] || s || "--";
}
function receiptStatusCls(s: string) {
  return s === "approved" ? "tag-green" : s === "reversed" ? "tag-gray" : s === "draft" ? "tag-gray" : "tag-orange";
}

function switchTab(v: string) {
  tab.value = v;
  load();
}

async function load() {
  loading.value = true;
  try {
    if (tab.value === "final") {
      const res = await request<{ items: FinalItem[] }>("/final-inspections", { silent: true });
      finalItems.value = res.items || [];
    } else if (tab.value === "receipts") {
      const res = await request<{ items: ReceiptItem[] }>("/finished-receipts", { silent: true });
      receipts.value = res.items || [];
    } else if (tab.value === "shipments") {
      const res = await request<{ items: ShipmentItem[] }>("/shipments", { silent: true });
      shipments.value = res.items || [];
    } else if (tab.value === "returns") {
      const res = await request<{ items: ReturnItem[] }>("/shipment-returns", { silent: true });
      returns.value = res.items || [];
    } else if (tab.value === "bundles") {
      const res = await request<{ items: BundleItem[] }>("/bundles", { silent: true });
      bundles.value = res.items || [];
    } else {
      const res = await request<{ items: InventoryItem[]; alert_count: number }>("/inventory/summary?inventory_type=material", { silent: true });
      inventory.value = res.items || [];
      alertCount.value = res.alert_count || 0;
    }
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function filterBundles() { /* 触发 computed 重新计算 */ }

async function confirmFinal(f: FinalItem) {
  try {
    await request(`/final-inspections/${f.id}/confirm`, { method: "POST", data: { version: f.version } });
    uni.showToast({ title: "已确认", icon: "none" });
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "确认失败", icon: "none" });
  }
}

function receiptAction(r: ReceiptItem, act: "submit" | "confirm-quality" | "approve") {
  const textMap: Record<string, string> = { submit: "提交", "confirm-quality": "质检确认", approve: "入库审批" };
  uni.showModal({
    title: textMap[act],
    content: `确定${textMap[act]}入库单「${r.receipt_no}」吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/finished-receipts/${r.id}/${act}`, { method: "POST", data: { version: r.version } });
        uni.showToast({ title: "操作成功", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "操作失败", icon: "none" });
      }
    },
  });
}

function confirmShipment(s: ShipmentItem) {
  uni.showModal({
    title: "确认出货",
    content: `确定出货「${s.shipment_no}」吗？确认后扣减成品库存。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/shipments/${s.id}/confirm`, { method: "POST", data: { version: s.version } });
        uni.showToast({ title: "已确认", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "确认失败", icon: "none" });
      }
    },
  });
}

async function saveFinal() {
  finalError.value = "";
  const f = finalForm.value;
  if (!f.inspection_no) { finalError.value = "请填写尾检单号"; return; }
  const wo = workOrders.value[finalWoIndex.value];
  const bundle = bundles.value.filter((b) => b.work_order_id === wo?.id)[finalBundleIndex.value];
  if (!wo || !bundle) { finalError.value = "请选择工单与扎"; return; }
  const inspected = Number(f.inspected_qty);
  const passed = Number(f.passed_qty);
  const rework = Number(f.rework_qty || 0);
  const scrapped = Number(f.scrapped_qty || 0);
  if (!inspected || inspected !== passed + rework + scrapped) {
    finalError.value = "检验数必须等于合格 + 返工 + 报废";
    return;
  }
  saving.value = true;
  try {
    await request("/final-inspections", {
      method: "POST",
      data: {
        inspection_no: f.inspection_no,
        work_order_id: wo.id,
        bundle_id: bundle.id,
        inspected_qty: inspected,
        passed_qty: passed,
        rework_qty: rework,
        scrapped_qty: scrapped,
        remark: f.remark || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "尾检已创建", icon: "none" });
    finalSheet.value = false;
    load();
  } catch (e) {
    finalError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

onLoad(() => {
  load();
  Promise.all([
    request<{ items: WorkOrderOption[] }>("/work-orders", { silent: true }).then((r) => (workOrders.value = r.items || [])),
    request<{ items: BundleItem[] }>("/bundles", { silent: true }).then((r) => (bundles.value = r.items || [])),
    request<{ items: WarehouseOption[] }>("/warehouses", { silent: true }).then((r) => (warehouses.value = r.items || [])),
    request<{ items: ProjectOption[] }>("/projects", { silent: true }).then((r) => (projects.value = r.items || [])),
    request<{ items: FinalItem[] }>("/final-inspections", { silent: true }).then((r) => (finalItems.value = r.items || [])),
  ]).catch(() => {});
});
/* ===== 退货 ===== */

async function openReturn(s: ShipmentItem) {
  returnTarget.value = s;
  returnSheet.value = true;
  returnLinesLoading.value = true;
  returnLineIndex.value = 0;
  returnQty.value = "";
  returnReason.value = "";
  returnError.value = "";
  try {
    const res = await request<{ lines: ShipmentLine[] }>(`/shipments/${s.id}`, { silent: true });
    returnLines.value = (res.lines || []).filter((l) => l.returnable_qty > 0);
  } catch (e) {
    returnError.value = (e as Error).message || "加载出货明细失败";
  } finally {
    returnLinesLoading.value = false;
  }
}

async function submitReturn() {
  returnError.value = "";
  const s = returnTarget.value;
  const line = returnLines.value[returnLineIndex.value];
  if (!s || !line) { returnError.value = "请选择出货扎"; return; }
  const qty = Number(returnQty.value);
  if (!qty || qty <= 0) { returnError.value = "请填写退货数量"; return; }
  if (qty > line.returnable_qty) {
    returnError.value = `退货数量不能超过可退余额 ${line.returnable_qty}`;
    return;
  }
  if (!returnReason.value) { returnError.value = "请填写退货原因"; return; }
  saving.value = true;
  try {
    await request(`/shipments/${s.id}/returns`, {
      method: "POST",
      data: { bundle_id: line.bundle_id, qty, reason: returnReason.value },
      idempotency: true,
    });
    uni.showToast({ title: "退货已提交", icon: "none" });
    returnSheet.value = false;
    load();
  } catch (e) {
    returnError.value = (e as Error).message || "退货失败";
  } finally {
    saving.value = false;
  }
}

/* ===== 新建入库 ===== */

function addReceiptLine() {
  receiptForm.value.lines.push({ inspectionIndex: 0, qty: "" });
}

function openReceiptCreate() {
  receiptForm.value = { receipt_no: "", lines: [{ inspectionIndex: 0, qty: "" }] };
  receiptWoIndex.value = 0;
  warehouseIndex.value = 0;
  formError.value = "";
  receiptSheet.value = true;
}

async function saveReceipt() {
  formError.value = "";
  const f = receiptForm.value;
  const wo = workOrders.value[receiptWoIndex.value];
  const wh = warehouses.value[warehouseIndex.value];
  if (!f.receipt_no) { formError.value = "请填写入库单号"; return; }
  if (!wo || !wh) { formError.value = "请选择工单与仓库"; return; }
  const lines = f.lines.map((l) => {
    const inspection = confirmedInspections.value[l.inspectionIndex];
    return { bundle_id: inspection?.bundle_id || "", inspection_id: inspection?.id || "", qty: Number(l.qty) };
  });
  for (const [i, l] of lines.entries()) {
    if (!l.bundle_id || !l.inspection_id) { formError.value = `第 ${i + 1} 行请选择尾检记录`; return; }
    if (!l.qty || l.qty <= 0) { formError.value = `第 ${i + 1} 行入库数量必须大于 0`; return; }
  }
  saving.value = true;
  try {
    await request("/finished-receipts", {
      method: "POST",
      data: { receipt_no: f.receipt_no, work_order_id: wo.id, warehouse_id: wh.id, lines },
      idempotency: true,
    });
    uni.showToast({ title: "入库单已创建", icon: "none" });
    receiptSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

/* ===== 新建出货 ===== */

function addShipmentLine() {
  shipmentForm.value.lines.push({ woIndex: 0, bundleIndex: 0, qty: "" });
}

function bundleLabelsFor(woIdx: number): string[] {
  const wo = workOrders.value[woIdx];
  return bundles.value.filter((b) => b.work_order_id === wo?.id).map((b) => `${b.bundle_no} 扎 · ${b.color} ${b.size}`);
}

function openShipmentCreate() {
  shipmentForm.value = { shipment_no: "", lines: [{ woIndex: 0, bundleIndex: 0, qty: "" }] };
  projectIndex.value = 0;
  formError.value = "";
  shipmentSheet.value = true;
}

async function saveShipment() {
  formError.value = "";
  const f = shipmentForm.value;
  const project = projects.value[projectIndex.value];
  if (!f.shipment_no) { formError.value = "请填写出货单号"; return; }
  if (!project) { formError.value = "请选择项目"; return; }
  const lines = f.lines.map((l) => {
    const wo = workOrders.value[l.woIndex];
    const bundle = bundles.value.filter((b) => b.work_order_id === wo?.id)[l.bundleIndex];
    return {
      work_order_id: wo?.id || "",
      bundle_id: bundle?.id || "",
      color: bundle?.color || "",
      size: bundle?.size || "",
      qty: Number(l.qty),
    };
  });
  for (const [i, l] of lines.entries()) {
    if (!l.work_order_id || !l.bundle_id) { formError.value = `第 ${i + 1} 行请选择工单与扎`; return; }
    if (!l.qty || l.qty <= 0) { formError.value = `第 ${i + 1} 行数量必须大于 0`; return; }
  }
  saving.value = true;
  try {
    await request("/shipments", {
      method: "POST",
      data: { shipment_no: f.shipment_no, project_id: project.id, lines },
      idempotency: true,
    });
    uni.showToast({ title: "出货单已创建", icon: "none" });
    shipmentSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

/* ===== 新建仓库 ===== */

async function saveWarehouse() {
  formError.value = "";
  if (!warehouseForm.value.code || !warehouseForm.value.name) {
    formError.value = "仓库编码与名称必填";
    return;
  }
  saving.value = true;
  try {
    await request("/warehouses", {
      method: "POST",
      data: { code: warehouseForm.value.code, name: warehouseForm.value.name, warehouse_type: "finished" },
      idempotency: true,
    });
    uni.showToast({ title: "仓库已创建", icon: "none" });
    warehouseSheet.value = false;
    warehouseForm.value = { code: "", name: "" };
    const res = await request<{ items: WarehouseOption[] }>("/warehouses", { silent: true });
    warehouses.value = res.items || [];
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

onShow(() => { /* 返回时刷新 */ });
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
.text-red { color: #b91c1c !important; }
</style>
