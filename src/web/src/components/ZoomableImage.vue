<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { useAttachmentUrl } from '../attachment'

const props = defineProps<{ attachmentId?: string | null }>()
const { url, loading } = useAttachmentUrl(toRef(props, 'attachmentId'))
const steps = [0.5, 0.75, 1, 1.25, 1.5, 2, 3]
const index = ref(2)
const natural = ref<{ width: number; height: number } | null>(null)
const zoom = computed(() => steps[index.value])
const percent = computed(() => Math.round(zoom.value * 100) + '%')
const displaySize = computed(() => {
  if (!natural.value) return null
  return { width: `${Math.round(natural.value.width * zoom.value)}px`, height: `${Math.round(natural.value.height * zoom.value)}px` }
})
function zoomIn() { if (index.value < steps.length - 1) index.value++ }
function zoomOut() { if (index.value > 0) index.value-- }
function actual() { index.value = 2 }
function onLoad(event: Event) {
  const img = event.target as HTMLImageElement
  natural.value = { width: img.naturalWidth, height: img.naturalHeight }
}
</script>

<template>
  <div class="zoomable">
    <div class="zoom-toolbar">
      <button class="button secondary small" :disabled="index <= 0" @click="zoomOut">− 缩小</button>
      <span class="zoom-percent">{{ percent }}</span>
      <button class="button secondary small" :disabled="index >= steps.length - 1" @click="zoomIn">＋ 放大</button>
      <button class="button secondary small" @click="actual">1:1</button>
    </div>
    <div class="zoom-stage">
      <img v-if="url" :src="url" :style="displaySize || undefined" @load="onLoad" alt="图片预览">
      <span v-else class="text-muted">{{ loading ? '加载中…' : '无图' }}</span>
    </div>
  </div>
</template>

<style scoped>
.zoomable { display: flex; flex-direction: column; gap: 8px; }
.zoom-toolbar { display: flex; align-items: center; gap: 8px; justify-content: center; }
.zoom-percent { min-width: 48px; text-align: center; font-weight: 600; }
.zoom-stage { display: flex; align-items: center; justify-content: center; overflow: auto; background: #f2f4f7; border-radius: 6px; min-height: 200px; max-height: 62vh; }
.zoom-stage img { margin: auto; max-width: none; max-height: none; object-fit: contain; }
</style>
