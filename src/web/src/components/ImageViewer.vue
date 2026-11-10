<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import ModalDialog from './ModalDialog.vue'
import ZoomableImage from './ZoomableImage.vue'

defineProps<{ attachmentId?: string | null; title?: string }>()
const emit = defineEmits<{ close: [] }>()
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') emit('close') }
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <ModalDialog :title="title || '查看图片'" wide @close="emit('close')">
    <ZoomableImage :attachment-id="attachmentId" />
  </ModalDialog>
</template>
