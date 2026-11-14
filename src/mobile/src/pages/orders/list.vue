<template>
  <BrandNavBar title="项目与工单" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">项目与工单</view>
      <button class="btn btn-solid btn-sm" @click="tab === 'projects' ? openProjectCreate() : openWorkOrderCreate()">
        ＋ 新建{{ tab === "projects" ? "项目" : "工单" }}
      </button>
    </view>

    <view class="chips">
      <view class="chip" :class="{ on: tab === 'projects' }" @click="tab = 'projects'">项目</view>
      <view class="chip" :class="{ on: tab === 'workorders' }" @click="tab = 'workorders'">工单</view>
      <view class="chip" :class="{ on: tab === 'cuttings' }" @click="tab = 'cuttings'; loadCuttings()">裁床</view>
    </view>

    <!-- 项目列表 -->
    <template v-if="tab === 'projects'">
      <view v-if="projectsLoading && !projects.length" class="empty">加载中…</view>
      <view v-else-if="!projects.length" class="empty">暂无项目</view>
      <view v-for="p in projects" :key="p.id" class="card" @click="goProject(p)">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title">{{ p.project_code }}</text>
              <text class="tag" :class="statusCls(p.status)">{{ statusText(p.status) }}</text>
            </view>
            <text class="muted">{{ p.customer_name }} · {{ p.product_name }}</text>
          </view>
          <text class="arrow">›</text>
        </view>
        <view class="row gap8" style="margin-top: 14rpx">
          <text class="muted">订单数 {{ p.order_qty }}</text>
          <text class="muted">· 已出货 {{ p.shipped_qty }}</text>
          <text class="muted">· 未出货 {{ p.unshipped_qty }}</text>
        </view>
        <text v-if="p.required_delivery_date" class="muted" style="margin-top: 8rpx">交期 {{ p.required_delivery_date }}</text>
      </view>
    </template>

    <!-- 裁床列表 -->
    <template v-else-if="tab === 'cuttings'">
      <view class="row between" style="margin-bottom: 16rpx">
        <text class="section-title" style="margin: 0">裁床单</text>
        <button class="btn btn-solid btn-sm" @click="openCuttingCreate">＋ 新建裁床单</button>
      </view>
      <view v-if="cuttingLoading && !cuttings.length" class="empty">加载中…</view>
      <view v-else-if="!cuttings.length" class="empty">暂无裁床单</view>
      <view v-for="c in cuttings" :key="c.id" class="card">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title" style="font-size: 28rpx">{{ c.cutting_no }}</text>
              <text class="tag" :class="c.status === 'confirmed' ? 'tag-green' : 'tag-orange'">{{ c.status === "confirmed" ? "已确认" : "草稿" }}</text>
            </view>
            <text class="muted">裁片 {{ c.cut_qty }}</text>
          </view>
        </view>
        <view class="actions" v-if="c.status === 'draft'">
          <button class="btn btn-solid btn-sm" @click="confirmCutting(c)">确认裁床（生成扎）</button>
          <button class="btn btn-danger btn-sm" @click="removeCutting(c)">删除</button>
        </view>
      </view>
    </template>

    <!-- 工单列表 -->
    <template v-else-if="tab === 'workorders'">
      <view class="chips" style="margin-top: 8rpx">
        <view
          v-for="s in woStatusOptions"
          :key="s.value"
          class="chip"
          :class="{ on: woStatus === s.value }"
          @click="woStatus = s.value; reloadWorkOrders()"
        >{{ s.label }}</view>
      </view>
      <view v-if="woLoading && !workOrders.length" class="empty">加载中…</view>
      <view v-else-if="!workOrders.length" class="empty">暂无工单</view>
      <view v-for="w in workOrders" :key="w.id" class="card" @click="goWorkOrder(w)">
        <view class="row between">
          <view class="col grow">
            <view class="row gap8">
              <text class="title">{{ w.work_order_code }}</text>
              <text class="tag" :class="woStatusCls(w.status)">{{ woStatusText(w.status) }}</text>
              <text v-if="w.review_status === 'approved'" class="tag tag-green">已审核</text>
            </view>
            <text class="muted">{{ w.product_name }}</text>
          </view>
          <text class="arrow">›</text>
        </view>
        <view class="progress" style="margin-top: 16rpx">
          <view class="progress-bar" :style="{ width: progressPct(w) + '%' }"></view>
        </view>
        <view class="row between" style="margin-top: 10rpx">
          <text class="muted">计划 {{ w.planned_product_qty }} · 派工 {{ w.planned_qty }} · 完成 {{ w.approved_good_qty }}</text>
          <text class="muted">{{ progressPct(w) }}%</text>
        </view>
      </view>
      <view v-if="woHasMore" class="loadmore" @click="loadMoreWorkOrders">{{ woLoading ? "加载中…" : "加载更多" }}</view>
    </template>

    <!-- 新建裁床单 -->
    <view v-if="cuttingSheet" class="sheet-mask" @click="cuttingSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建裁床单</view>
        <view class="field">
          <text class="field-label required">裁床编号</text>
          <input class="input" v-model.trim="cuttingForm.cutting_no" placeholder="如 CUT-0001" />
        </view>
        <view class="field">
          <text class="field-label required">工单</text>
          <picker :range="woPickerLabels" :value="cuttingWoIndex" @change="onCuttingWoChange">
            <view class="picker-box">{{ woPickerLabels[cuttingWoIndex] || "选择工单" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">唛架（已审核）</text>
          <picker :range="markerLabels" :value="markerIndex" @change="markerIndex = $event.detail.value">
            <view class="picker-box">{{ markerLabels[markerIndex] || "选择唛架" }}</view>
          </picker>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">拉布层数</text>
            <input class="input" v-model="cuttingForm.fabric_layers" type="number" placeholder="1" />
          </view>
          <view class="field grow">
            <text class="field-label required">裁片数</text>
            <input class="input" v-model="cuttingForm.cut_qty" type="number" />
          </view>
        </view>

        <view class="divider"></view>
        <view class="row between" style="margin-bottom: 12rpx">
          <text class="section-title" style="margin: 0">配码明细（合计须等于裁片数）</text>
          <button class="btn btn-outline btn-sm" @click="addCuttingDetail">＋ 添加行</button>
        </view>
        <view v-for="(d, i) in cuttingForm.details" :key="i" class="detail-card">
          <view class="row between" style="margin-bottom: 8rpx">
            <text class="muted">第 {{ i + 1 }} 行</text>
            <text v-if="cuttingForm.details.length > 1" class="remove" @click="cuttingForm.details.splice(i, 1)">删除</text>
          </view>
          <view class="row gap8">
            <input class="input grow" v-model.trim="d.color" placeholder="颜色" />
            <input class="input grow" v-model.trim="d.size" placeholder="尺码" />
          </view>
          <view class="row gap8" style="margin-top: 8rpx">
            <input class="input grow" v-model="d.bundle_count" type="number" placeholder="扎数" />
            <input class="input grow" v-model="d.piece_qty" type="number" placeholder="件数" />
          </view>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveCutting">{{ saving ? "保存中…" : "保存裁床单" }}</button>
      </view>
    </view>

    <!-- 新建项目 -->
    <view v-if="projectSheet" class="sheet-mask" @click="projectSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建项目</view>
        <view class="field">
          <text class="field-label required">项目编号</text>
          <input class="input" v-model.trim="projectForm.project_code" placeholder="如 PRJ-202511-001" />
        </view>
        <view class="field">
          <text class="field-label required">客户</text>
          <picker :range="customerNames" :value="customerIndex" @change="onCustomerChange">
            <view class="picker-box">{{ customerNames[customerIndex] || "请选择客户" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">产品</text>
          <picker :range="productNames" :value="productIndex" @change="onProductChange">
            <view class="picker-box">{{ productNames[productIndex] || "请选择产品" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">订单数量</text>
          <input class="input" v-model="projectForm.order_qty" type="number" placeholder="订单数量" />
        </view>
        <view class="field">
          <text class="field-label">客户单价</text>
          <input class="input" v-model="projectForm.customer_unit_price" placeholder="选填" />
        </view>
        <view class="field">
          <text class="field-label">出厂单价</text>
          <input class="input" v-model="projectForm.external_unit_price" placeholder="选填" />
        </view>
        <view class="field">
          <text class="field-label">要求交期</text>
          <picker mode="date" :value="projectForm.required_delivery_date" @change="projectForm.required_delivery_date = $event.detail.value">
            <view class="picker-box">{{ projectForm.required_delivery_date || "选择日期（可选）" }}</view>
          </picker>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveProject">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>

    <!-- 新建工单 -->
    <view v-if="workOrderSheet" class="sheet-mask" @click="workOrderSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建工单</view>
        <view class="field">
          <text class="field-label required">工单编号</text>
          <input class="input" v-model.trim="woForm.work_order_code" placeholder="如 WO-202511-001" />
        </view>
        <view class="field">
          <text class="field-label required">所属项目</text>
          <picker :range="projectNames" :value="woProjectIndex" @change="onWoProjectChange">
            <view class="picker-box">{{ projectNames[woProjectIndex] || "请选择项目" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">计划数量</text>
          <input class="input" v-model="woForm.planned_product_qty" type="number" placeholder="计划生产数量" />
        </view>
        <view class="field">
          <text class="field-label">优先级</text>
          <picker :range="priorityLabels" :value="priorityIndex" @change="priorityIndex = $event.detail.value">
            <view class="picker-box">{{ priorityLabels[priorityIndex] }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label">颜色</text>
          <input class="input" v-model.trim="woForm.color" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label">报工模式</text>
          <picker :range="reportModeLabels" :value="reportModeIndex" @change="reportModeIndex = $event.detail.value">
            <view class="picker-box">{{ reportModeLabels[reportModeIndex] }}</view>
          </picker>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveWorkOrder">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad, onShow } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Project {
  id: string;
  project_code: string;
  customer_name: string;
  product_name: string;
  order_qty: number;
  shipped_qty: number;
  unshipped_qty: number;
  status: string;
  required_delivery_date?: string;
  version: number;
}

interface WorkOrder {
  id: string;
  work_order_code: string;
  product_name: string;
  planned_product_qty: number;
  status: string;
  review_status: string;
  priority: string;
  planned_qty: number;
  approved_good_qty: number;
  progress_ratio: number;
  version: number;
}

const tab = ref<"projects" | "workorders" | "cuttings">("projects");

/* 裁床 */
interface CuttingItem {
  id: string; cutting_no: string; work_order_id: string; cut_qty: number;
  status: string; version: number;
}
interface MarkerOption { id: string; marker_no: string; color: string; review_status: string }
interface CuttingDetailForm { color: string; size: string; bundle_count: string; piece_qty: string }

const cuttings = ref<CuttingItem[]>([]);
const cuttingLoading = ref(false);
const cuttingSheet = ref(false);
const markers = ref<MarkerOption[]>([]);
const markerLabels = computed(() => markers.value.map((m) => `${m.marker_no} · ${m.color}`));
const markerIndex = ref(0);
const cuttingWoIndex = ref(0);
const cuttingForm = ref({
  cutting_no: "",
  fabric_layers: "1",
  cut_qty: "",
  details: [{ color: "", size: "", bundle_count: "1", piece_qty: "" }] as CuttingDetailForm[],
});

const projects = ref<Project[]>([]);
const projectsLoading = ref(false);

const workOrders = ref<WorkOrder[]>([]);
const woLoading = ref(false);
const woHasMore = ref(false);
const woNextCursor = ref("");
const woStatus = ref("");
const woStatusOptions = [
  { label: "全部", value: "" },
  { label: "待下发", value: "pending_release" },
  { label: "生产中", value: "in_production" },
  { label: "已暂停", value: "paused" },
  { label: "已完成", value: "completed" },
  { label: "已取消", value: "cancelled" },
];

const customers = ref<{ id: string; name: string }[]>([]);
const customerNames = computed(() => customers.value.map((c) => c.name));
const customerIndex = ref(0);
const products = ref<{ id: string; name: string }[]>([]);
const productNames = computed(() => products.value.map((p) => p.name));
const productIndex = ref(0);
const projectNames = computed(() => projects.value.map((p) => `${p.project_code} · ${p.product_name}`));

const projectSheet = ref(false);
const workOrderSheet = ref(false);
const saving = ref(false);
const formError = ref("");
const projectForm = ref({
  project_code: "",
  order_qty: "",
  customer_unit_price: "",
  external_unit_price: "",
  required_delivery_date: "",
});
const woProjectIndex = ref(0);
const priorityLabels = ["普通", "高", "紧急"];
const priorityValues = ["normal", "high", "urgent"];
const priorityIndex = ref(0);
const reportModeLabels = ["B · 自主选工序", "A · 派工受控"];
const reportModeValues = ["B", "A"];
const reportModeIndex = ref(0);
const woForm = ref({ work_order_code: "", planned_product_qty: "", color: "" });

/* 工单下拉（新建工单项目选择/裁床单选择共用） */
const woPickerLabels = computed(() => workOrders.value.map((w) => w.work_order_code));

function statusText(s: string) {
  return s === "completed" ? "已完成" : s === "cancelled" ? "已取消" : "进行中";
}
function statusCls(s: string) {
  return s === "completed" ? "tag-green" : s === "cancelled" ? "tag-gray" : "tag-blue";
}
function woStatusText(s: string) {
  const map: Record<string, string> = {
    pending_release: "待下发",
    in_production: "生产中",
    paused: "已暂停",
    completed: "已完成",
    cancelled: "已取消",
  };
  return map[s] || s || "--";
}
function woStatusCls(s: string) {
  return s === "in_production" ? "tag-blue" : s === "completed" ? "tag-green" : s === "paused" ? "tag-orange" : "tag-gray";
}
function progressPct(w: WorkOrder): number {
  if (!w.planned_product_qty) return 0;
  return Math.min(100, Math.round((w.approved_good_qty / w.planned_product_qty) * 100));
}

async function loadProjects() {
  projectsLoading.value = true;
  try {
    const res = await request<{ items: Project[] }>("/projects", { silent: true });
    projects.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    projectsLoading.value = false;
  }
}

async function loadWorkOrders(reset = true) {
  woLoading.value = true;
  try {
    const q: string[] = [];
    if (woStatus.value) q.push(`status=${woStatus.value}`);
    if (!reset && woNextCursor.value) q.push(`cursor=${woNextCursor.value}`);
    const res = await request<{ items: WorkOrder[]; next_cursor?: string; has_more?: boolean }>(
      `/work-orders${q.length ? "?" + q.join("&") : ""}`,
      { silent: true }
    );
    if (reset) workOrders.value = res.items || [];
    else workOrders.value = [...workOrders.value, ...(res.items || [])];
    woNextCursor.value = res.next_cursor || "";
    woHasMore.value = !!res.has_more;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    woLoading.value = false;
  }
}
function reloadWorkOrders() {
  loadWorkOrders(true);
}
function loadMoreWorkOrders() {
  loadWorkOrders(false);
}

async function loadOptions() {
  try {
    const [c, p] = await Promise.all([
      request<{ items: { id: string; name: string }[] }>("/customers", { silent: true }),
      request<{ items: { id: string; name: string }[] }>("/products", { silent: true }),
    ]);
    customers.value = c.items || [];
    products.value = p.items || [];
  } catch {
    /* 选项加载失败不影响列表 */
  }
}

function onCustomerChange(e: { detail: { value: number } }) {
  customerIndex.value = e.detail.value;
}
function onProductChange(e: { detail: { value: number } }) {
  productIndex.value = e.detail.value;
}
function onWoProjectChange(e: { detail: { value: number } }) {
  woProjectIndex.value = e.detail.value;
}

function openProjectCreate() {
  projectForm.value = { project_code: "", order_qty: "", customer_unit_price: "", external_unit_price: "", required_delivery_date: "" };
  customerIndex.value = 0;
  productIndex.value = 0;
  formError.value = "";
  projectSheet.value = true;
}

function openWorkOrderCreate() {
  woForm.value = { work_order_code: "", planned_product_qty: "", color: "" };
  woProjectIndex.value = 0;
  priorityIndex.value = 0;
  reportModeIndex.value = 0;
  formError.value = "";
  workOrderSheet.value = true;
}

async function saveProject() {
  formError.value = "";
  const f = projectForm.value;
  if (!f.project_code || !f.order_qty) {
    formError.value = "项目编号与订单数量必填";
    return;
  }
  const customer = customers.value[customerIndex.value];
  const product = products.value[productIndex.value];
  if (!customer || !product) {
    formError.value = "请选择客户与产品";
    return;
  }
  saving.value = true;
  try {
    await request("/projects", {
      method: "POST",
      data: {
        project_code: f.project_code,
        customer_id: customer.id,
        product_id: product.id,
        order_qty: Number(f.order_qty),
        customer_unit_price: f.customer_unit_price || "0",
        external_unit_price: f.external_unit_price || "0",
        required_delivery_date: f.required_delivery_date || undefined,
      },
      idempotency: true,
    });
    uni.showToast({ title: "项目已创建", icon: "none" });
    projectSheet.value = false;
    loadProjects();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

async function saveWorkOrder() {
  formError.value = "";
  const f = woForm.value;
  if (!f.work_order_code || !f.planned_product_qty) {
    formError.value = "工单编号与计划数量必填";
    return;
  }
  const project = projects.value[woProjectIndex.value];
  if (!project) {
    formError.value = "请选择所属项目";
    return;
  }
  saving.value = true;
  try {
    await request("/work-orders", {
      method: "POST",
      data: {
        work_order_code: f.work_order_code,
        project_id: project.id,
        planned_product_qty: Number(f.planned_product_qty),
        priority: priorityValues[priorityIndex.value],
        color: f.color || undefined,
        report_mode: reportModeValues[reportModeIndex.value],
      },
      idempotency: true,
    });
    uni.showToast({ title: "工单已创建", icon: "none" });
    workOrderSheet.value = false;
    loadWorkOrders(true);
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function goProject(p: Project) {
  uni.navigateTo({ url: `/pages/orders/project?id=${p.id}` });
}
function goWorkOrder(w: WorkOrder) {
  uni.navigateTo({ url: `/pages/orders/workorder?id=${w.id}` });
}

/* ===== 裁床 ===== */

async function loadCuttings() {
  cuttingLoading.value = true;
  try {
    const res = await request<{ items: CuttingItem[] }>("/cutting-orders", { silent: true });
    cuttings.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    cuttingLoading.value = false;
  }
}

async function loadMarkers(workOrderId: string) {
  try {
    const res = await request<{ items: MarkerOption[] }>(
      `/markers?work_order_id=${workOrderId}&review_status=approved`, { silent: true }
    );
    markers.value = res.items || [];
  } catch {
    markers.value = [];
  }
}

function onCuttingWoChange(e: { detail: { value: number } }) {
  cuttingWoIndex.value = e.detail.value;
  markerIndex.value = 0;
  const wo = workOrders.value[cuttingWoIndex.value];
  if (wo) loadMarkers(wo.id);
}

function addCuttingDetail() {
  cuttingForm.value.details.push({ color: "", size: "", bundle_count: "1", piece_qty: "" });
}

function openCuttingCreate() {
  cuttingForm.value = {
    cutting_no: "",
    fabric_layers: "1",
    cut_qty: "",
    details: [{ color: "", size: "", bundle_count: "1", piece_qty: "" }],
  };
  cuttingWoIndex.value = 0;
  markerIndex.value = 0;
  formError.value = "";
  const wo = workOrders.value[0];
  if (wo) loadMarkers(wo.id);
  cuttingSheet.value = true;
}

async function saveCutting() {
  formError.value = "";
  const f = cuttingForm.value;
  const wo = workOrders.value[cuttingWoIndex.value];
  const marker = markers.value[markerIndex.value];
  if (!f.cutting_no) { formError.value = "请填写裁床编号"; return; }
  if (!wo || !marker) { formError.value = "请选择工单与唛架"; return; }
  const details = f.details
    .filter((d) => d.color && d.size && Number(d.piece_qty) > 0)
    .map((d, i) => ({
      color: d.color,
      size: d.size,
      bundle_count: Number(d.bundle_count) || 1,
      piece_qty: Number(d.piece_qty),
      sort_no: i + 1,
    }));
  if (!details.length) { formError.value = "请至少添加一行有效配码明细"; return; }
  const sum = details.reduce((s, d) => s + d.piece_qty, 0);
  if (sum !== Number(f.cut_qty)) {
    formError.value = `裁片数 ${f.cut_qty} 与配码明细合计 ${sum} 不一致`;
    return;
  }
  saving.value = true;
  try {
    await request("/cutting-orders", {
      method: "POST",
      data: {
        cutting_no: f.cutting_no,
        work_order_id: wo.id,
        marker_id: marker.id,
        cutting_date: Date.now() * 1000,
        fabric_layers: Number(f.fabric_layers) || 1,
        cut_qty: Number(f.cut_qty),
        details,
      },
      idempotency: true,
    });
    uni.showToast({ title: "裁床单已创建", icon: "none" });
    cuttingSheet.value = false;
    loadCuttings();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function confirmCutting(c: CuttingItem) {
  uni.showModal({
    title: "确认裁床",
    content: `确认裁床「${c.cutting_no}」后将生成扎与菲票，确定确认吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        const result = await request<{ bundle_count: number }>(`/cutting-orders/${c.id}/confirm`, {
          method: "POST",
          data: { version: c.version },
        });
        uni.showToast({ title: `已确认，生成 ${result.bundle_count} 扎`, icon: "none" });
        loadCuttings();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "确认失败", icon: "none" });
      }
    },
  });
}

function removeCutting(c: CuttingItem) {
  uni.showModal({
    title: "删除裁床单",
    content: "仅草稿裁床单可删除，确定删除吗？",
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/cutting-orders/${c.id}`, { method: "DELETE" });
        uni.showToast({ title: "已删除", icon: "none" });
        loadCuttings();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "删除失败", icon: "none" });
      }
    },
  });
}

onLoad(() => {
  loadProjects();
  loadWorkOrders(true);
  loadOptions();
});
onShow(() => {
  if (tab.value === "workorders") loadWorkOrders(true);
});
</script>

<style scoped>
.arrow {
  font-size: 40rpx;
  color: #cbd5e1;
}
.progress {
  height: 12rpx;
  background: #f1f5f9;
  border-radius: 999rpx;
  overflow: hidden;
}
.progress-bar {
  height: 100%;
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  border-radius: 999rpx;
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
.detail-card {
  background: #f8fafc;
  border-radius: 12rpx;
  padding: 16rpx;
  margin-bottom: 12rpx;
}
.remove {
  color: #b91c1c;
  font-size: 24rpx;
}
</style>
