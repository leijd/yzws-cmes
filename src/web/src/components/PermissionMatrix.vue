<script setup lang="ts">
import { computed, reactive, ref } from 'vue'

/* 角色权限配置矩阵：功能模块 | 权限类型 两列默认可见，「说明」列默认隐藏，
 * 需要时用工具栏开关显示。每行一个功能模块，模块复选框可整组勾选；
 * 权限类型按两列网格排列；说明汇总该模块下各权限点的描述，过长时折叠并提供「详细」展开。 */
interface PermItem {
  code: string
  module: string
  name: string
  kind: string
  description?: string
  admin_only?: boolean
}
interface PermModule { name: string; items: PermItem[]; summary: string }

const props = defineProps<{
  items: PermItem[]
  selected: Record<string, boolean>
  readonly?: boolean
}>()

const filter = ref('')
const showDescription = ref(false)
const expanded = reactive<Record<string, boolean>>({})
const SUMMARY_LIMIT = 46

const groups = computed<PermModule[]>(() => {
  const q = filter.value.trim().toLowerCase()
  const leaves = props.items.filter(item => {
    if (item.admin_only) return false
    /* 固定管理员角色只读视图：只列已授予的权限点 */
    if (props.readonly && !props.selected[item.code]) return false
    if (!q) return true
    return [item.module, item.name, item.description].some(v => String(v || '').toLowerCase().includes(q))
  })
  const byModule = new Map<string, PermModule>()
  for (const item of leaves) {
    let group = byModule.get(item.module)
    if (!group) {
      group = { name: item.module, items: [], summary: '' }
      byModule.set(item.module, group)
    }
    group.items.push(item)
  }
  const result = [...byModule.values()]
  for (const group of result) {
    const parts = group.items.map(item => item.description).filter(Boolean)
    group.summary = parts.length ? parts.join('；') + '。' : ''
  }
  return result
})

function state(group: PermModule) {
  const checked = group.items.filter(item => props.selected[item.code]).length
  return { all: group.items.length > 0 && checked === group.items.length, none: checked === 0 }
}
function toggleModule(group: PermModule, ev: Event) {
  if (props.readonly) return
  const checked = (ev.target as HTMLInputElement).checked
  group.items.forEach(item => props.selected[item.code] = checked)
}
function text(group: PermModule) {
  return !expanded[group.name] && group.summary.length > SUMMARY_LIMIT
    ? group.summary.slice(0, SUMMARY_LIMIT) + '…' : group.summary
}
</script>

<template>
  <div class="permission-matrix">
    <div class="pm-toolbar">
      <input v-model="filter" placeholder="检索功能模块 / 权限类型 / 说明">
      <label class="pm-toggle"><input v-model="showDescription" type="checkbox">显示说明</label>
    </div>
    <div class="table-wrap pm-table">
      <table>
        <thead><tr><th>功能模块</th><th>权限类型</th><th v-if="showDescription" style="width:34%">说明</th></tr></thead>
        <tbody>
          <tr v-for="group in groups" :key="group.name">
            <td class="pm-module">
              <label>
                <input type="checkbox" :checked="state(group).all" :indeterminate="!state(group).all && !state(group).none" :disabled="readonly" @change="toggleModule(group, $event)">
                <strong>{{ group.name }}</strong>
              </label>
            </td>
            <td>
              <div class="pm-grid">
                <label v-for="item in group.items" :key="item.code" class="pm-item">
                  <input v-model="selected[item.code]" type="checkbox" :disabled="readonly">
                  <span>{{ item.name }}</span>
                </label>
              </div>
            </td>
            <td v-if="showDescription" class="pm-summary">
              <span>{{ text(group) }}</span>
              <button v-if="group.summary.length > SUMMARY_LIMIT" class="button link" type="button" @click="expanded[group.name] = !expanded[group.name]">{{ expanded[group.name] ? '收起' : '详细' }}</button>
            </td>
          </tr>
          <tr v-if="!groups.length"><td :colspan="showDescription ? 3 : 2" class="text-muted" style="text-align:center;padding:14px">无匹配的权限点</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.pm-toolbar { display: flex; align-items: center; gap: 12px; margin: 8px 0 12px; }
.pm-toolbar > input { flex: 1; }
.pm-toggle { display: flex; align-items: center; gap: 6px; flex: none; white-space: nowrap; color: #64748b; font-size: 13px; cursor: pointer; }
.pm-table { max-height: 46vh; overflow: auto; }
.pm-table thead th { position: sticky; top: 0; z-index: 2; background: #fff; box-shadow: 0 1px 0 var(--border, #e2e8f0); }
.pm-table th:first-child { width: 168px; }
.pm-table td { vertical-align: top; }
.pm-module { background: #f8fafc; }
.pm-module label { display: flex; align-items: center; gap: 8px; font-weight: 600; color: #17255f; white-space: nowrap; }
.pm-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px 16px; }
.pm-item { display: flex; align-items: center; gap: 6px; cursor: pointer; white-space: nowrap; }
.pm-summary { color: #64748b; font-size: 13px; line-height: 1.65; white-space: normal; }
.pm-summary .button.link { font-size: 13px; }
</style>
