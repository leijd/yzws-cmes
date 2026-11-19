<script setup lang="ts">
import { ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  modelValue?: string
  counts?: Record<string, number>
}>(), { modelValue: 'running', counts: () => ({}) })
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void; (e: 'change', v: string): void }>()
const active = ref(props.modelValue)
watch(() => props.modelValue, v => { active.value = v })
const tabs = [
  { key: 'new', label: '新建', icon: '✚' },
  { key: 'running', label: '进行时', icon: '◷' },
  { key: 'history', label: '历史', icon: '▤' },
]
function pick(key: string) {
  active.value = key
  emit('update:modelValue', key)
  emit('change', key)
}
</script>

<template>
  <div class="three-state">
    <button v-for="t in tabs" :key="t.key" type="button" class="state-tab" :class="{ active: active === t.key }" @click="pick(t.key)">
      <span class="state-icon">{{ t.icon }}</span>{{ t.label }}
      <span v-if="counts[t.key] !== undefined" class="state-count">{{ counts[t.key] }}</span>
    </button>
  </div>
</template>

<style scoped>
.three-state { display: inline-flex; gap: 2px; padding: 3px; background: #F1F3F7; border: 1px solid #E5E8EF; border-radius: 10px; }
.state-tab { display: inline-flex; align-items: center; gap: 5px; padding: 6px 14px; border: 0; border-radius: 7px; background: transparent;
  color: #4E5562; font-size: 13px; font-weight: 500; cursor: pointer; transition: background .18s, color .18s, box-shadow .18s; }
.state-tab:hover { color: #2F6BFF; background: rgba(47,107,255,.07); }
.state-tab.active { color: #fff; background: linear-gradient(135deg, #2F6BFF, #5B8BFF); box-shadow: 0 2px 8px rgba(47,107,255,.28); }
.state-icon { font-size: 13px; }
.state-count { min-width: 18px; padding: 0 5px; border-radius: 9px; font-size: 11px; line-height: 16px; text-align: center; background: rgba(0,0,0,.06); }
.state-tab.active .state-count { background: rgba(255,255,255,.24); }
</style>
