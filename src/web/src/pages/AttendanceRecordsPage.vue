<script setup lang="ts">
/* 考勤记录管理（设计 6.10）：筛选查询、作废（原因必填） */
import { onMounted, reactive, ref } from 'vue'
import { api, formatTime } from '../api'
import { toastError, toastSuccess } from '../toast'
import FilterSelect from '../components/FilterSelect.vue'
import DatePicker from '../components/DatePicker.vue'
import PageState from '../components/PageState.vue'

const items = ref<any[]>([])
const loading = ref(false)
const filter = reactive({ att_type: '', point: '', date: '' })

async function load() {
  loading.value = true
  try {
    const q = new URLSearchParams()
    if (filter.att_type) q.set('att_type', filter.att_type)
    if (filter.point) q.set('point', filter.point)
    if (filter.date) q.set('date', filter.date)
    items.value = (await api<{ items: any[] }>('/attendance/records?' + q.toString())).items
  } finally { loading.value = false }
}

async function voidRecord(item: any) {
  const reason = window.prompt(`作废 ${item.employee_name} 的考勤记录，请输入原因`) || ''
  if (!reason.trim()) { if (reason !== null) toastError('作废必须填写原因'); return }
  try {
    await api(`/attendance/records/${item.id}/void`, { method: 'POST', body: JSON.stringify({ reason, version: 1 }) })
    toastSuccess('已作废'); await load()
  } catch { /* */ }
}

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-header">
      <h2>考勤记录</h2>
      <div class="toolbar">
        <FilterSelect v-model="filter.att_type" :options="[{ value: '', label: '全部类型' }, { value: 'in', label: '上班' }, { value: 'out', label: '下班' }]" @update:model-value="load" />
        <input v-model.trim="filter.point" class="search-input" placeholder="考勤点" @keyup.enter="load">
        <DatePicker v-model="filter.date" placeholder="选择日期" @update:model-value="load"/>
        <button class="button secondary" @click="load">查询</button>
      </div>
    </header>
    <div class="table-wrap"><table><thead><tr><th>工号</th><th>姓名</th><th>类型</th><th>时间</th><th>考勤点</th><th>状态</th><th>操作</th></tr></thead>
      <tbody><tr v-for="item in items" :key="item.id" :style="item.status === 'void' ? 'opacity:.5' : ''">
        <td>{{ item.employee_no || '—' }}</td><td>{{ item.employee_name || '—' }}</td>
        <td><span class="status" :class="item.att_type === 'in' ? 'active' : 'disabled'">{{ item.att_type === 'in' ? '上班' : '下班' }}</span></td>
        <td>{{ formatTime(item.att_at) }}</td><td>{{ item.point }}</td>
        <td>{{ item.status === 'normal' ? '正常' : '已作废' }}</td>
        <td><button v-if="item.status === 'normal'" class="button link text-danger" @click="voidRecord(item)">作废</button></td>
      </tr></tbody></table><PageState :empty="!items.length" /></div>
  </section>
</template>
