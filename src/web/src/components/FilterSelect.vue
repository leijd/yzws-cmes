<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{
  modelValue?: string | number | null
  options: { value: any; label: string }[]
  placeholder?: string
}>(), { placeholder: '请选择' })
const emit = defineEmits<{ 'update:modelValue': [any]; change: [any] }>()

const open = ref(false)
const keyword = ref('')
const pos = ref({ left: 0, top: 0 })
const searchInput = ref<HTMLInputElement>()

const current = computed(() => props.options.find(o => o.value === props.modelValue))
const display = computed(() => current.value?.label || '')
const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  return kw ? props.options.filter(o => o.label.toLowerCase().includes(kw)) : props.options
})

async function toggle(event: MouseEvent) {
  if (open.value) { open.value = false; return }
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  const width = Math.max(rect.width, 260)
  const height = 320
  const below = rect.bottom + 6
  pos.value = { left: Math.min(rect.left, window.innerWidth - width - 8), top: below + height > window.innerHeight - 8 ? Math.max(8, rect.top - height - 6) : below }
  keyword.value = ''
  open.value = true
  await nextTick()
  searchInput.value?.focus()
}
function choose(option: { value: any; label: string }) {
  emit('update:modelValue', option.value)
  emit('change', option.value)
  open.value = false
}
function onDocumentMouseDown(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (!target.closest('.filter-select') && !target.closest('.fs-panel')) open.value = false
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') open.value = false
}
onMounted(() => {
  document.addEventListener('mousedown', onDocumentMouseDown)
  window.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocumentMouseDown)
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <span class="filter-select">
    <input :value="display" :placeholder="placeholder" readonly @click="toggle">
    <Teleport to="body">
      <div v-if="open" class="fs-panel" :style="{ left: pos.left + 'px', top: pos.top + 'px' }">
        <input ref="searchInput" v-model="keyword" class="fs-search" placeholder="输入关键字筛选">
        <div class="fs-options">
          <button
            v-for="option in filtered"
            :key="String(option.value)"
            type="button"
            :class="{ active: option.value === modelValue }"
            @click="choose(option)"
          >{{ option.label }}</button>
          <div v-if="!filtered.length" class="fs-empty">无匹配项</div>
        </div>
      </div>
    </Teleport>
  </span>
</template>

<style scoped>
.filter-select { position: relative; display: inline-block; }
.filter-select > input { cursor: pointer; min-width: 160px; }
.fs-panel {
  position: fixed; z-index: 9000; min-width: 260px; max-width: 380px; background: #fff;
  border: 1px solid #e4e9f0; border-radius: 10px; box-shadow: 0 12px 32px rgba(16, 24, 40, 0.18);
  padding: 10px;
}
.fs-search { width: 100%; margin-bottom: 8px; }
.fs-options { display: flex; flex-direction: column; gap: 2px; max-height: 250px; overflow: auto; }
.fs-options button {
  flex: 0 0 auto;
  border: 0; background: transparent; text-align: left; padding: 9px 10px; border-radius: 7px;
  font-size: 14px; line-height: 1.4; color: #344054; cursor: pointer; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.fs-options button:hover { background: #eff4ff; }
.fs-options button.active { background: var(--primary, #155eef); color: #fff; }
.fs-empty { padding: 18px; text-align: center; color: #98a2b3; font-size: 13px; }
</style>
