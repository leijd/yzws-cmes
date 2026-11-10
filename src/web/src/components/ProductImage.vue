<script setup lang="ts">
import { toRef } from 'vue'
import { useAttachmentUrl } from '../attachment'
const props = withDefaults(defineProps<{ attachmentId?: string | null; alt?: string; width?: number; height?: number }>(), { width: 64, height: 64 })
const { url, loading } = useAttachmentUrl(toRef(props, 'attachmentId'))
</script>
<template><div class="product-image" :style="{ width: `${width}px`, height: `${height}px` }"><img v-if="url" :src="url" :alt="alt || '产品图片'"><span v-else>{{ loading ? '加载中' : '无图' }}</span></div></template>
<style scoped>.product-image { display: inline-flex; align-items: center; justify-content: center; overflow: hidden; border: 1px solid #e4e9f0; border-radius: 6px; background: #f2f4f7; color: #98a2b3; font-size: 11px; cursor: inherit; }.product-image img { width: 100%; height: 100%; object-fit: cover; }</style>
