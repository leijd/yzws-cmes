<script setup lang="ts">
import { computed, ref, type CSSProperties } from 'vue'
defineProps<{ title: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const position = ref<{ left: number; top: number } | null>(null)
const modalStyle = computed<CSSProperties>(() => position.value ? { left: `${position.value.left}px`, top: `${position.value.top}px`, position: 'fixed' } : {})
let dragOrigin: { pointerX: number; pointerY: number; left: number; top: number } | null = null
function startDrag(event: PointerEvent) {
  if (event.button !== 0) return
  const header = event.currentTarget as HTMLElement
  const modal = header.closest('.modal') as HTMLElement | null
  if (!modal) return
  const rect = modal.getBoundingClientRect()
  position.value = { left: rect.left, top: rect.top }
  dragOrigin = { pointerX: event.clientX, pointerY: event.clientY, left: rect.left, top: rect.top }
  header.setPointerCapture(event.pointerId)
}
function drag(event: PointerEvent) {
  if (!dragOrigin || !position.value) return
  const maxLeft = Math.max(0, window.innerWidth - 80)
  const maxTop = Math.max(0, window.innerHeight - 48)
  position.value = {
    left: Math.min(Math.max(0, dragOrigin.left + event.clientX - dragOrigin.pointerX), maxLeft),
    top: Math.min(Math.max(0, dragOrigin.top + event.clientY - dragOrigin.pointerY), maxTop),
  }
}
function stopDrag() { dragOrigin = null }
</script>
<template>
  <Teleport to="body"><div class="modal-backdrop" @mousedown.self="emit('close')"><section class="modal" :class="{ wide }" :style="modalStyle" role="dialog" aria-modal="true">
    <header class="modal-header modal-drag-handle" @pointerdown="startDrag" @pointermove="drag" @pointerup="stopDrag" @pointercancel="stopDrag"><h2>{{ title }}</h2><button class="icon-button" @pointerdown.stop @click="emit('close')">×</button></header>
    <div class="modal-body"><slot /></div>
    <footer v-if="$slots.footer" class="modal-footer"><slot name="footer" /></footer>
  </section></div></Teleport>
</template>
