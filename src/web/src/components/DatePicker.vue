<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{ modelValue?: string | null; placeholder?: string }>(), { placeholder: '选择日期' })
const emit = defineEmits<{ 'update:modelValue': [string] }>()

const WEEKDAYS = ['一', '二', '三', '四', '五', '六', '日']
const pad = (n: number) => String(n).padStart(2, '0')
const open = ref(false)
const mode = ref<'day' | 'month'>('day')
const pos = ref({ left: 0, top: 0 })
const inputEl = ref<HTMLInputElement>()
const text = ref('')
const invalid = ref(false)
const today = new Date()
const todayValue = `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}`
const view = ref({ year: today.getFullYear(), month: today.getMonth() + 1 })

function parse(v: string | null | undefined) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(v || '')
  return m ? { year: Number(m[1]), month: Number(m[2]), day: Number(m[3]) } : null
}
const current = computed(() => parse(props.modelValue))

// 快捷输入：yyyyMMdd、yyyy-MM-dd、yyyy/MM/dd、yyyy.MM.dd
function normalize(raw: string): string | null {
  const s = raw.trim()
  if (!s) return ''
  let m = /^(\d{4})(\d{2})(\d{2})$/.exec(s)
  if (!m) m = /^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$/.exec(s)
  if (!m) return null
  const year = Number(m[1])
  const month = Number(m[2])
  const day = Number(m[3])
  if (month < 1 || month > 12 || day < 1) return null
  if (day > new Date(year, month, 0).getDate()) return null
  return `${year}-${pad(month)}-${pad(day)}`
}
function onInput() {
  if (!text.value.trim()) { invalid.value = false; return }
  const value = normalize(text.value)
  invalid.value = value === null
  if (value) emit('update:modelValue', value)
}
function commit(close = false) {
  if (!text.value.trim()) {
    invalid.value = false
    if (props.modelValue) emit('update:modelValue', '')
    if (close) open.value = false
    return
  }
  const value = normalize(text.value)
  if (value === null) { invalid.value = true; return }
  invalid.value = false
  text.value = value
  if (value !== (props.modelValue || '')) emit('update:modelValue', value)
  if (close) open.value = false
}
watch(() => props.modelValue, v => { text.value = v || ''; invalid.value = false }, { immediate: true })

const cells = computed(() => {
  const { year, month } = view.value
  const daysInMonth = new Date(year, month, 0).getDate()
  const firstWeekday = (new Date(year, month - 1, 1).getDay() + 6) % 7
  const prevDays = new Date(year, month - 1, 0).getDate()
  const list: { day: number; value: string; inMonth: boolean }[] = []
  for (let i = firstWeekday - 1; i >= 0; i--) {
    const d = prevDays - i
    const pm = month === 1 ? 12 : month - 1
    const py = month === 1 ? year - 1 : year
    list.push({ day: d, value: `${py}-${pad(pm)}-${pad(d)}`, inMonth: false })
  }
  for (let d = 1; d <= daysInMonth; d++) {
    list.push({ day: d, value: `${year}-${pad(month)}-${pad(d)}`, inMonth: true })
  }
  let next = 1
  const nm = month === 12 ? 1 : month + 1
  const ny = month === 12 ? year + 1 : year
  while (list.length % 7 !== 0 || list.length < 42) {
    list.push({ day: next, value: `${ny}-${pad(nm)}-${pad(next)}`, inMonth: false })
    next++
  }
  return list
})

function toggle() {
  if (open.value) { open.value = false; return }
  view.value = current.value ? { year: current.value.year, month: current.value.month } : { year: today.getFullYear(), month: today.getMonth() + 1 }
  mode.value = 'day'
  const el = inputEl.value
  if (el) {
    const rect = el.getBoundingClientRect()
    const width = 290
    const height = 340
    const left = Math.min(rect.left, window.innerWidth - width - 8)
    const below = rect.bottom + 6
    pos.value = { left, top: below + height > window.innerHeight - 8 ? Math.max(8, rect.top - height - 6) : below }
  }
  open.value = true
}
function select(value: string) {
  emit('update:modelValue', value)
  text.value = value
  invalid.value = false
  open.value = false
}
function clear() {
  emit('update:modelValue', '')
  text.value = ''
  open.value = false
}
function pickToday() {
  select(todayValue)
}
function shiftMonth(delta: number) {
  let { year, month } = view.value
  month += delta
  if (month < 1) { month = 12; year-- }
  if (month > 12) { month = 1; year++ }
  view.value = { year, month }
}
function shiftYear(delta: number) {
  view.value = { ...view.value, year: view.value.year + delta }
}
function pickMonth(month: number) {
  view.value = { ...view.value, month }
  mode.value = 'day'
}
function onDocumentMouseDown(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (!target.closest('.zh-date-picker') && !target.closest('.zh-date-panel')) open.value = false
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
  <span class="zh-date-picker">
    <input
      ref="inputEl"
      v-model="text"
      :class="{ invalid }"
      :placeholder="placeholder"
      maxlength="10"
      autocomplete="off"
      @click="toggle"
      @input="onInput"
      @blur="commit()"
      @keydown.enter.prevent="commit(true)"
      @keydown.esc="open = false"
    >
    <Teleport to="body">
      <div v-if="open" class="zh-date-panel" :style="{ left: pos.left + 'px', top: pos.top + 'px' }">
        <template v-if="mode === 'day'">
          <div class="zh-date-header">
            <button type="button" title="上一年" @click="shiftYear(-1)">«</button>
            <button type="button" title="上个月" @click="shiftMonth(-1)">‹</button>
            <button type="button" class="zh-date-title" title="选择年月" @click="mode = 'month'">{{ view.year }}年{{ view.month }}月</button>
            <button type="button" title="下个月" @click="shiftMonth(1)">›</button>
            <button type="button" title="下一年" @click="shiftYear(1)">»</button>
          </div>
          <div class="zh-date-weekdays">
            <span v-for="day in WEEKDAYS" :key="day">{{ day }}</span>
          </div>
          <div class="zh-date-grid">
            <button
              v-for="(cellValue, index) in cells"
              :key="index"
              type="button"
              :class="{ muted: !cellValue.inMonth, active: cellValue.value === modelValue, today: cellValue.value === todayValue }"
              @click="select(cellValue.value)"
            >{{ cellValue.day }}</button>
          </div>
          <div class="zh-date-footer">
            <button type="button" @click="clear">清除</button>
            <button type="button" @click="pickToday">今天</button>
          </div>
        </template>
        <template v-else>
          <div class="zh-date-header">
            <button type="button" title="上一年" @click="shiftYear(-1)">‹</button>
            <span>{{ view.year }}年</span>
            <button type="button" title="下一年" @click="shiftYear(1)">›</button>
          </div>
          <div class="zh-month-grid">
            <button
              v-for="m in 12"
              :key="m"
              type="button"
              :class="{ active: m === view.month }"
              @click="pickMonth(m)"
            >{{ m }}月</button>
          </div>
          <div class="zh-date-footer">
            <button type="button" @click="mode = 'day'">返回日历</button>
            <button type="button" @click="open = false">关闭</button>
          </div>
        </template>
      </div>
    </Teleport>
  </span>
</template>

<style scoped>
.zh-date-picker { position: relative; display: inline-block; }
.zh-date-picker input { width: 170px; cursor: text; }
.zh-date-picker input.invalid { border-color: var(--danger, #d92d20); box-shadow: 0 0 0 3px #fef3f2; }
.zh-date-panel {
  position: fixed; z-index: 9000; width: 290px; background: #fff;
  border: 1px solid #e4e9f0; border-radius: 10px; box-shadow: 0 12px 32px rgba(16, 24, 40, 0.18);
  padding: 12px;
}
.zh-date-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.zh-date-header span { font-weight: 600; font-size: 15px; }
.zh-date-header button {
  border: 0; background: transparent; font-size: 18px; color: #475467; cursor: pointer;
  width: 30px; height: 30px; border-radius: 6px;
}
.zh-date-header button:hover { background: #f2f4f7; }
.zh-date-header .zh-date-title {
  width: auto; padding: 0 8px; font-size: 15px; font-weight: 600; color: #172b4d;
}
.zh-date-weekdays { display: grid; grid-template-columns: repeat(7, 1fr); margin-bottom: 4px; }
.zh-date-weekdays span { text-align: center; font-size: 12px; color: #98a2b3; padding: 4px 0; }
.zh-date-grid { display: grid; grid-template-columns: repeat(7, 1fr); gap: 2px; }
.zh-date-grid button {
  border: 0; background: transparent; padding: 7px 0; border-radius: 7px;
  font-size: 13px; color: #344054; cursor: pointer;
}
.zh-date-grid button:hover { background: #eff4ff; }
.zh-date-grid button.muted { color: #c4cdd5; }
.zh-date-grid button.active { background: var(--primary, #155eef); color: #fff; }
.zh-date-grid button.today:not(.active) { border: 1px solid var(--primary, #155eef); }
.zh-month-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 4px; padding: 4px 0; }
.zh-month-grid button {
  border: 0; background: transparent; padding: 10px 0; border-radius: 7px;
  font-size: 13px; color: #344054; cursor: pointer;
}
.zh-month-grid button:hover { background: #eff4ff; }
.zh-month-grid button.active { background: var(--primary, #155eef); color: #fff; }
.zh-date-footer { display: flex; justify-content: space-between; margin-top: 10px; }
.zh-date-footer button {
  border: 0; background: transparent; color: var(--primary, #155eef);
  font-size: 13px; cursor: pointer; padding: 4px 8px;
}
</style>
