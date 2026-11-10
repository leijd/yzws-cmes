<script setup lang="ts">
import { ref } from 'vue'
import ZoomableImage from './ZoomableImage.vue'

defineProps<{ attachmentId?: string | null }>()
const open = ref(false)
const pos = ref({ left: 0, top: 0 })
function show(event: MouseEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  const width = 420
  let left = rect.right + 10
  if (left + width > window.innerWidth - 8) left = Math.max(8, rect.left - width - 10)
  open.value = true
  pos.value = { left, top: Math.max(8, Math.min(rect.top, window.innerHeight - 420)) }
}
</script>

<template>
  <span style="display: inline-block" @mouseenter="show" @mouseleave="open = false"><slot /></span>
  <Teleport to="body">
    <div v-if="open" class="hover-preview" :style="{ left: pos.left + 'px', top: pos.top + 'px' }" @mouseenter="open = true" @mouseleave="open = false">
      <ZoomableImage :attachment-id="attachmentId" />
    </div>
  </Teleport>
</template>

<style scoped>
.hover-preview { position: fixed; z-index: 9000; width: 420px; background: #fff; border: 1px solid #e4e9f0; border-radius: 10px; box-shadow: 0 12px 40px rgba(16, 24, 40, 0.28); padding: 10px; }
.hover-preview :deep(.zoom-stage) { max-height: 320px; min-height: 160px; }
</style>
