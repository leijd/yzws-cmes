<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{ modelValue?: string | null }>()
const emit = defineEmits<{ 'update:modelValue': [string] }>()

const open = ref(false)
const pos = ref({ left: 0, top: 0 })
const viewYear = ref(new Date().getFullYear())

function parse(v: string | null | undefined) {
  const m = /^(\d{4})-(\d{2})$/.exec(v || '')
  return m ? { year: Number(m[1]), month: Number(m[2]) } : null
}
const current = computed(() => parse(props.modelValue))
const display = computed(() => (current.value ? `${current.value.year}年${current.value.month}月` : ''))

function toggle(event: MouseEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  if (!open.value) {
    viewYear.value = current.value?.year ?? new Date().getFullYear()
    const width = 272
    const height = 300
    const left = Math.min(rect.left, window.innerWidth - width - 8)
    const below = rect.bottom + 6
    pos.value = { left, top: below + height > window.innerHeight - 8 ? Math.max(8, rect.top - height - 6) : below }
  }
  open.value = !open.value
}
function select(month: number) {
  const value = `${viewYear.value}-${String(month).padStart(2, '0')}`
  emit('update:modelValue', value)
  open.value = false
}
function clear() {
  emit('update:modelValue', '')
  open.value = false
}
function shiftYear(delta: number) {
  viewYear.value += delta
}
function onDocumentMouseDown(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (!target.closest('.zh-month-picker') && !target.closest('.zh-month-panel')) open.value = false
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
  <span class="zh-month-picker">
    <input :value="display" placeholder="选择月份" readonly @click="toggle">
    <Teleport to="body">
      <div v-if="open" class="zh-month-panel" :style="{ left: pos.left + 'px', top: pos.top + 'px' }">
        <div class="zh-month-header">
          <button type="button" @click="shiftYear(-1)">‹</button>
          <span>{{ viewYear }}年</span>
          <button type="button" @click="shiftYear(1)">›</button>
        </div>
        <div class="zh-month-grid">
          <button
            v-for="month in 12"
            :key="month"
            type="button"
            :class="{ active: current && current.year === viewYear && current.month === month }"
            @click="select(month)"
          >{{ month }}月</button>
        </div>
        <div class="zh-month-footer">
          <button type="button" @click="clear">清除</button>
        </div>
      </div>
    </Teleport>
  </span>
</template>

<style scoped>
.zh-month-picker { position: relative; display: inline-block; }
.zh-month-picker input { width: 150px; cursor: pointer; }
.zh-month-panel {
  position: fixed; z-index: 9000; width: 272px; background: #fff;
  border: 1px solid #e4e9f0; border-radius: 10px; box-shadow: 0 12px 32px rgba(16, 24, 40, 0.18);
  padding: 12px;
}
.zh-month-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.zh-month-header span { font-weight: 600; font-size: 15px; }
.zh-month-header button {
  border: 0; background: transparent; font-size: 18px; color: #475467; cursor: pointer;
  width: 30px; height: 30px; border-radius: 6px;
}
.zh-month-header button:hover { background: #f2f4f7; }
.zh-month-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
.zh-month-grid button {
  border: 0; background: transparent; padding: 9px 0; border-radius: 7px;
  font-size: 14px; color: #344054; cursor: pointer;
}
.zh-month-grid button:hover { background: #eff4ff; }
.zh-month-grid button.active { background: var(--primary, #155eef); color: #fff; }
.zh-month-footer { display: flex; justify-content: flex-end; margin-top: 10px; }
.zh-month-footer button {
  border: 0; background: transparent; color: var(--primary, #155eef);
  font-size: 13px; cursor: pointer; padding: 4px 8px;
}
</style>
