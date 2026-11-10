<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import BundleLabelCard from '../components/BundleLabelCard.vue'
import FilterSelect from '../components/FilterSelect.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'

interface Label {
  bundle_id: string
  ticket_id: string
  public_code: string
  tenant_code: string
  tenant_name: string
  project_code: string
  brand: string
  customer_name: string
  work_order_code: string
  product_code: string
  product_name: string
  spec: string
  style: string
  color: string
  size: string
  qty: number
  bundle_no: string
  bed_no: string
  marker_no: string
  fabric_layers: number
  cutting_no: string
  cutting_date: number
  cutter_name: string
  parts: { part_code: string; part_name: string; qty: number; ticket_id: string; public_code: string }[]
  qr_payload: string
  qr_image: string
  generated_at: number
}

const orders = ref<any[]>([])
const labels = ref<Label[]>([])
const orderID = ref('')
const paper = ref<'58' | '80'>('58')
const loading = ref(false)
const error = ref('')
const notice = ref('')
const preview = ref<Label | null>(null)
const printList = ref<Label[]>([])
const selected = reactive<Record<string, boolean>>({})

const orderOptions = computed(() => orders.value.map((order: any) => ({
  value: order.id,
  label: `${order.work_order_code} · ${order.product_name || ''} · ${order.status || ''}`,
})))
const selectedLabels = computed(() => labels.value.filter(label => selected[label.bundle_id]))
const allSelected = computed(() => labels.value.length > 0 && selectedLabels.value.length === labels.value.length)

let pageStyle: HTMLStyleElement | null = null

async function loadOrders() {
  try {
    orders.value = (await api<{ items: any[] }>('/work-orders')).items
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function loadLabels() {
  labels.value = []
  Object.keys(selected).forEach(key => delete selected[key])
  if (!orderID.value) return
  loading.value = true
  error.value = ''
  try {
    labels.value = (await api<{ items: Label[] }>(`/bundle-labels?work_order_id=${encodeURIComponent(orderID.value)}`)).items
    if (!labels.value.length) notice.value = '该工单暂无扎标签，请先完成裁床确认'
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

function toggleAll() {
  const target = !allSelected.value
  labels.value.forEach(label => { selected[label.bundle_id] = target })
}

function applyPageStyle(heightMm = 0) {
  if (!pageStyle) {
    pageStyle = document.createElement('style')
    pageStyle.id = 'bundle-label-page-style'
    document.head.appendChild(pageStyle)
  }
  const size = heightMm > 0 ? `size: ${paper.value}mm ${Math.ceil(heightMm)}mm;` : ''
  pageStyle.textContent = `@page { ${size} margin: 0; }`
}

async function printLabels(items: Label[]) {
  if (!items.length) {
    error.value = '请先勾选要打印的扎标签'
    return
  }
  error.value = ''
  printList.value = items
  document.body.classList.add('label-printing')
  await nextTick()
  // 用页面上已渲染的标签测量实际高度，按一张标签一页设置纸张尺寸，
  // 避免热敏小票打印时被裁切或跨页。
  const sample = document.querySelector('.label-grid .bundle-label, .preview-wrap .bundle-label') as HTMLElement | null
  const heightMm = sample ? (sample.getBoundingClientRect().height * 25.4) / 96 + 2 : 0
  applyPageStyle(heightMm)
  await nextTick()
  try {
    window.print()
  } finally {
    document.body.classList.remove('label-printing')
    printList.value = []
  }
}

function printOne(label: Label) {
  printLabels([label])
}

function printSelected() {
  printLabels(selectedLabels.value)
}

function printAll() {
  printLabels(labels.value)
}

onMounted(loadOrders)
</script>

<template>
  <div class="page-header">
    <div>
      <h1>扎标签</h1>
      <p>裁床确认后每扎自动生成一张标签，可显示、预览并按 58mm / 80mm 小票打印</p>
    </div>
    <div class="header-actions">
      <button class="button secondary" :disabled="loading" @click="loadLabels">刷新</button>
      <button class="button secondary" :disabled="!selectedLabels.length" @click="printSelected">打印选中（{{ selectedLabels.length }}）</button>
      <button class="button" :disabled="!labels.length" @click="printAll">全部打印</button>
    </div>
  </div>

  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>

  <section class="panel">
    <div class="toolbar">
      <FilterSelect v-model="orderID" :options="orderOptions" placeholder="选择工单" @change="loadLabels" />
      <FilterSelect v-model="paper" :options="[{ value: '58', label: '58mm 小票' }, { value: '80', label: '80mm 小票' }]" />
      <label class="select-all"><input type="checkbox" :checked="allSelected" :disabled="!labels.length" @change="toggleAll"> 全选</label>
      <span class="text-muted">共 {{ labels.length }} 扎，已选 {{ selectedLabels.length }} 扎</span>
    </div>

    <PageState :loading="loading" />
    <div v-if="!loading && !orderID" class="panel-body"><div class="notice" style="margin:0">请选择工单，查看该工单裁床确认后生成的扎标签。</div></div>
    <div v-if="!loading && orderID && !labels.length" class="panel-body"><div class="notice" style="margin:0">该工单暂无扎标签，请先在【项目与工单】完成裁床确认。</div></div>

    <div v-if="!loading && labels.length" class="label-grid">
      <div v-for="label in labels" :key="label.bundle_id" class="label-cell">
        <label class="label-check"><input v-model="selected[label.bundle_id]" type="checkbox"> 选择</label>
        <BundleLabelCard :label="label" :paper="paper" />
        <div class="label-actions">
          <button class="button link" @click="preview = label">预览</button>
          <button class="button link" @click="printOne(label)">打印</button>
        </div>
      </div>
    </div>
  </section>

  <Teleport to="body">
    <div v-if="printList.length" id="label-print-root">
      <BundleLabelCard v-for="label in printList" :key="label.bundle_id" :label="label" :paper="paper" />
    </div>
  </Teleport>

  <ModalDialog v-if="preview" wide title="扎标签预览" @close="preview = null">
    <div class="preview-wrap">
      <BundleLabelCard :label="preview" :paper="paper" />
    </div>
    <template #footer>
      <button class="button secondary" @click="preview = null">关闭</button>
      <button class="button" @click="printOne(preview)">打印这张</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.select-all { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; color: #475467; }
.label-grid { display: flex; flex-wrap: wrap; gap: 18px; padding: 18px; align-items: flex-start; }
.label-cell { position: relative; display: flex; flex-direction: column; align-items: center; gap: 8px; }
.label-check { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: #475467; align-self: flex-start; }
.label-actions { display: flex; gap: 10px; }
.preview-wrap { display: flex; justify-content: center; padding: 10px 0 18px; }
</style>

<!-- 打印隔离必须使用全局样式，并配合 Teleport 挂到 body 下：
     打印 root 会成为 body 的直接子节点，其余页面结构被彻底移除，
     避免把整个应用页面（导航、列表、弹窗）一起打印出来。 -->
<style>
#label-print-root { display: none; }

@media print {
  html,
  body {
    margin: 0 !important;
    padding: 0 !important;
    background: #fff !important;
  }

  body.label-printing > *:not(#label-print-root) {
    display: none !important;
  }

  #label-print-root {
    display: block !important;
    position: static !important;
    width: 100% !important;
  }

  #label-print-root .bundle-label {
    margin: 0 auto !important;
    box-shadow: none !important;
    border: none !important;
    page-break-after: always;
    break-after: page;
  }

  #label-print-root .bundle-label:last-child {
    page-break-after: auto;
    break-after: auto;
  }
}
</style>
