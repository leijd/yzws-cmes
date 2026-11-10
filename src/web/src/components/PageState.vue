<script setup lang="ts">
defineProps<{ loading?: boolean; error?: string; empty?: boolean }>()
const emit = defineEmits<{ retry: [] }>()
</script>
<template>
  <div v-if="loading" class="page-state"><span class="spinner"></span>正在加载数据…</div>
  <div v-else-if="error" class="page-state error-state">{{ error }}<button class="button secondary" @click="emit('retry')">重试</button></div>
  <div v-else-if="empty" class="page-state"><strong>暂无数据</strong><span>可调整筛选条件，或新建一条记录。</span></div>
</template>
