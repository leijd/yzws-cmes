<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{ label: any; paper?: '58' | '80' }>(), { paper: '58' })

const width = computed(() => (props.paper === '80' ? '74mm' : '52mm'))
const qrSize = computed(() => (props.paper === '80' ? '36mm' : '28mm'))
const fontSize = computed(() => (props.paper === '80' ? '10.5pt' : '9pt'))
const partsText = computed(() => {
  const parts = props.label?.parts || []
  if (!parts.length) return ''
  if (parts.length === 1 && parts[0].part_code === 'whole') return ''
  return parts.map((part: any) => `${part.part_name || part.part_code} ${part.qty}`).join(' / ')
})
function formatDate(micros?: number) {
  if (!micros) return ''
  return new Date(micros / 1000).toLocaleString('zh-CN', { hour12: false })
}
</script>

<template>
  <article class="bundle-label" :style="{ width, fontSize }">
    <div class="bl-tenant">{{ label.tenant_name || '一智算' }}</div>
    <div class="bl-brand">{{ label.brand || label.customer_name || '—' }}</div>
    <div class="bl-title">{{ label.project_code || '—' }} · 扎 {{ label.bundle_no }}</div>
    <img v-if="label.qr_image" class="bl-qr" :src="label.qr_image" :style="{ width: qrSize, height: qrSize }" alt="扎二维码">
    <div v-else class="bl-noqr">二维码未生成</div>
    <div v-if="label.public_code" class="bl-code">{{ label.public_code }}</div>
    <div class="bl-rows">
      <div><span>项目号</span><b>{{ label.project_code || '—' }}</b></div>
      <div><span>扎号</span><b>{{ label.bundle_no || '—' }}</b></div>
      <div><span>品牌</span><b>{{ label.brand || '—' }}</b></div>
      <div><span>款式</span><b>{{ label.style || '—' }}</b></div>
      <div><span>颜色</span><b>{{ label.color || '—' }}</b></div>
      <div><span>尺码</span><b>{{ label.size || '—' }}</b></div>
      <div><span>数量</span><b>{{ label.qty }} 件</b></div>
      <div><span>裁床人</span><b>{{ label.cutter_name || '—' }}</b></div>
      <div><span>床次</span><b>{{ label.bed_no || '—' }}</b></div>
      <div><span>工单</span><b>{{ label.work_order_code || '—' }}</b></div>
      <div v-if="partsText"><span>部位</span><b>{{ partsText }}</b></div>
    </div>
    <div class="bl-footer">扫码派工 / 报工</div>
    <div class="bl-time">{{ formatDate(label.generated_at) }}</div>
  </article>
</template>

<style scoped>
.bundle-label {
  background: #fff;
  color: #111;
  border: 1px solid #d0d5dd;
  border-radius: 3px;
  padding: 4mm 3mm;
  box-sizing: border-box;
  font-family: "Microsoft YaHei", "PingFang SC", ui-monospace, monospace;
  line-height: 1.35;
  text-align: center;
  page-break-inside: avoid;
  break-inside: avoid;
}
.bl-tenant { font-weight: 700; letter-spacing: .08em; padding-bottom: 1mm; border-bottom: 1px dashed #111; }
.bl-brand { font-weight: 700; margin-top: 1.5mm; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bl-title { font-weight: 700; margin: 1mm 0 1.5mm; }
.bl-qr { display: block; margin: 0 auto 1mm; image-rendering: pixelated; }
.bl-noqr { margin: 6mm 0; color: #667085; font-size: .9em; }
.bl-code { font-family: ui-monospace, "Courier New", monospace; letter-spacing: .1em; font-weight: 700; margin-bottom: 1mm; }
.bl-rows { text-align: left; border-top: 1px dashed #111; border-bottom: 1px dashed #111; padding: 1mm 0; }
.bl-rows div { display: flex; justify-content: space-between; gap: 2mm; padding: .3mm 0; }
.bl-rows span { color: #333; white-space: nowrap; }
.bl-rows b { font-weight: 700; text-align: right; word-break: break-all; }
.bl-footer { margin-top: 1.5mm; font-weight: 700; }
.bl-time { margin-top: .5mm; font-size: .8em; color: #555; }
</style>
