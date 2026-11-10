<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, command } from '../api'

interface Label {
  bundle_id: string
  ticket_id: string
  public_code: string
  tenant_code: string
  tenant_name: string
  project_code: string
  brand: string
  customer_name: string
  work_order_code: string
  product_code: string
  product_name: string
  spec: string
  style: string
  color: string
  size: string
  qty: number
  bundle_no: string
  bed_no: string
  cutter_name: string
  parts: { part_code: string; part_name: string; qty: number }[]
}

interface Task {
  task_id: string
  version: number
  sequence_no: number
  process_code: string
  process_name: string
  planned_qty: number
  reported_qty: number
  remaining_qty: number
  assigned_qty: number
}

interface ResolveResult {
  ticket_id: string
  bundle_id: string
  work_order_code: string
  product_code: string
  product_name: string
  bundle_no: string
  color: string
  size: string
  current_qty: number
  report_mode: string
  tasks: Task[]
  employees?: { id: string; employee_no: string; name: string }[]
  can_assign?: boolean
  can_report?: boolean
}

const route = useRoute()
const router = useRouter()
const tenant = String(route.params.tenant || '')
const code = String(route.params.code || '')

const label = ref<Label | null>(null)
const context = ref<ResolveResult | null>(null)
const loading = ref(true)
const error = ref('')
const notice = ref('')
const mode = ref<'report' | 'dispatch'>('report')
const selectedTaskID = ref('')
const goodQty = ref<number | null>(null)
const defectQty = ref<number | null>(null)
const remark = ref('')
const dispatchEmployeeID = ref('')
const dispatchQty = ref<number | null>(null)
const submitting = ref(false)

const token = () => localStorage.getItem('clothp_access_token')
const loggedIn = computed(() => !!token())
const tasks = computed(() => context.value?.tasks || [])
const selectedTask = computed(() => tasks.value.find(task => task.task_id === selectedTaskID.value) || null)
const canAssign = computed(() => !!context.value?.can_assign)
const canReport = computed(() => !!context.value?.can_report)
const supportsDispatch = computed(() => context.value?.report_mode === 'A')
const canDispatch = computed(() => supportsDispatch.value && canAssign.value)
const employees = computed(() => context.value?.employees || [])
const partsText = computed(() => {
  const parts = label.value?.parts || []
  if (!parts.length || (parts.length === 1 && parts[0].part_code === 'whole')) return ''
  return parts.map(part => `${part.part_name} ${part.qty}`).join(' / ')
})
const assignableQty = computed(() => {
  if (!label.value || !selectedTask.value) return 0
  return Math.max(0, Math.min(selectedTask.value.remaining_qty, label.value.qty - selectedTask.value.assigned_qty))
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    label.value = await api<Label>(`/public/scan/${encodeURIComponent(tenant)}/${encodeURIComponent(code)}`)
    if (loggedIn.value) await loadContext()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function loadContext() {
  try {
    context.value = await api<ResolveResult>(`/scan/resolve?payload=${encodeURIComponent(code)}`)
    const available = context.value.tasks || []
    if (!selectedTaskID.value && available.length) {
      selectedTaskID.value = available[0].task_id
      onTaskChange()
    }
    mode.value = context.value.can_report || !canDispatch.value ? 'report' : 'dispatch'
  } catch {
    context.value = null
  }
}

function onTaskChange() {
  goodQty.value = null
  defectQty.value = null
  dispatchQty.value = assignableQty.value > 0 ? assignableQty.value : null
}

function goLogin() {
  router.push(`/login?redirect=${encodeURIComponent(route.fullPath)}`)
}

async function submitReport() {
  const good = Number(goodQty.value || 0)
  const defect = Number(defectQty.value || 0)
  if (!selectedTask.value) { error.value = '请选择工序'; return }
  if (good + defect <= 0) { error.value = '请填写合格数或次品数'; return }
  submitting.value = true
  error.value = ''
  try {
    const result = await command<{ review_status?: string }>('/work-reports', {
      task_id: selectedTask.value.task_id,
      bundle_id: label.value?.bundle_id,
      good_qty: good,
      defect_qty: defect,
      report_kind: 'normal',
      occurred_at: Date.now() * 1000,
      remark: remark.value || null,
    })
    notice.value = `报工成功${result.review_status === 'pending' ? '，等待审核' : ''}`
    goodQty.value = null
    defectQty.value = null
    remark.value = ''
    await loadContext()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    submitting.value = false
  }
}

async function submitDispatch() {
  if (!canDispatch.value) { error.value = '当前账号无派工权限或工单不支持扎级派工'; return }
  if (!selectedTask.value) { error.value = '请选择工序'; return }
  if (!dispatchEmployeeID.value) { error.value = '请选择派工员工'; return }
  const qty = Number(dispatchQty.value || 0)
  if (qty <= 0) { error.value = '请填写派工数量'; return }
  if (qty > assignableQty.value) { error.value = `派工数量不能超过可派数量 ${assignableQty.value}`; return }
  submitting.value = true
  error.value = ''
  try {
    await api(`/production-tasks/${selectedTask.value.task_id}/bundle-assignments`, {
      method: 'POST',
      body: JSON.stringify({
        version: selectedTask.value.version,
        items: [{ bundle_id: label.value?.bundle_id, employee_id: dispatchEmployeeID.value, planned_qty: qty }],
      }),
    })
    notice.value = '派工成功'
    dispatchEmployeeID.value = ''
    await loadContext()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    submitting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="scan-page">
    <header class="scan-head">
      <div class="scan-tenant">{{ label?.tenant_name || '一智算' }}</div>
      <h1 v-if="label">{{ label.project_code }} · 扎 {{ label.bundle_no }}</h1>
      <h1 v-else>扎标签</h1>
      <p v-if="label">{{ label.work_order_code }} · {{ label.product_name }}</p>
    </header>

    <div v-if="loading" class="scan-card">正在读取扎标签…</div>
    <div v-if="error" class="scan-error">{{ error }}</div>
    <div v-if="notice" class="scan-notice">{{ notice }}</div>

    <section v-if="label" class="scan-card">
      <div class="scan-grid">
        <div><span>项目号</span><b>{{ label.project_code || '—' }}</b></div>
        <div><span>扎号</span><b>{{ label.bundle_no || '—' }}</b></div>
        <div><span>品牌</span><b>{{ label.brand || '—' }}</b></div>
        <div><span>款式</span><b>{{ label.style || '—' }}</b></div>
        <div><span>颜色 / 尺码</span><b>{{ label.color || '—' }} / {{ label.size || '—' }}</b></div>
        <div><span>数量</span><b>{{ label.qty }} 件</b></div>
        <div><span>裁床人</span><b>{{ label.cutter_name || '—' }}</b></div>
        <div><span>床次</span><b>{{ label.bed_no || '—' }}</b></div>
        <div v-if="partsText" class="full"><span>部位</span><b>{{ partsText }}</b></div>
      </div>
      <div class="scan-code">扫码码：{{ label.public_code }}</div>
    </section>

    <section v-if="label && !loggedIn" class="scan-card">
      <p class="scan-tip">登录后可对该扎进行派工、报工。</p>
      <button class="scan-primary" @click="goLogin">登录后派工 / 报工</button>
    </section>

    <template v-if="label && loggedIn && context">
      <section class="scan-card">
        <div v-if="canDispatch" class="scan-tabs">
          <button :class="{ active: mode === 'report' }" @click="mode = 'report'">报工</button>
          <button :class="{ active: mode === 'dispatch' }" @click="mode = 'dispatch'">派工</button>
        </div>
        <div class="scan-task-list">
          <button
            v-for="task in tasks"
            :key="task.task_id"
            class="scan-task"
            :class="{ active: selectedTaskID === task.task_id }"
            @click="selectedTaskID = task.task_id; onTaskChange()"
          >
            <span>{{ task.sequence_no }}. {{ task.process_name }}</span>
            <small>已报 {{ task.reported_qty }} / 计划 {{ task.planned_qty }}</small>
          </button>
          <div v-if="!tasks.length" class="scan-tip">该工单暂无工序任务。</div>
        </div>
      </section>

      <section v-if="selectedTask && mode === 'report'" class="scan-card">
        <h2>报工 · {{ selectedTask.process_name }}</h2>
        <div class="scan-form-row"><label>合格数</label><input v-model.number="goodQty" type="number" min="0" placeholder="0"></div>
        <div class="scan-form-row"><label>次品数</label><input v-model.number="defectQty" type="number" min="0" placeholder="0"></div>
        <div class="scan-form-row"><label>备注</label><input v-model.trim="remark" placeholder="选填"></div>
        <div class="scan-tip">本工序剩余可报 {{ selectedTask.remaining_qty }} 件；同一扎同一工序只能报工一次。</div>
        <button class="scan-primary" :disabled="submitting || !canReport" @click="submitReport">{{ submitting ? '提交中…' : '提交报工' }}</button>
      </section>

      <section v-if="selectedTask && canDispatch && mode === 'dispatch'" class="scan-card">
        <h2>派工 · {{ selectedTask.process_name }}</h2>
        <div class="scan-form-row">
          <label>派工员工</label>
          <select v-model="dispatchEmployeeID">
            <option value="">请选择员工</option>
            <option v-for="employee in employees" :key="employee.id" :value="employee.id">{{ employee.employee_no }} · {{ employee.name }}</option>
          </select>
        </div>
        <div class="scan-form-row"><label>派工数量</label><input v-model.number="dispatchQty" type="number" min="1" :max="assignableQty" placeholder="0"></div>
        <div class="scan-tip">本扎本工序可派 {{ assignableQty }} 件；派工数量不能超过扎件数。</div>
        <button class="scan-primary" :disabled="submitting || !canAssign" @click="submitDispatch">{{ submitting ? '提交中…' : '确认派工' }}</button>
      </section>
    </template>

    <footer class="scan-foot">一智算 · 扫码派工 / 报工</footer>
  </div>
</template>

<style scoped>
.scan-page { min-height: 100vh; background: #eef2f7; padding: 18px 12px 30px; color: #172b4d; }
.scan-head { text-align: center; padding: 10px 0 16px; }
.scan-tenant { font-size: 13px; color: #667085; letter-spacing: .12em; }
.scan-head h1 { font-size: 24px; margin: 6px 0 4px; }
.scan-head p { margin: 0; color: #667085; font-size: 13px; }
.scan-card { background: #fff; border-radius: 14px; padding: 16px; margin-bottom: 12px; box-shadow: 0 4px 14px rgba(16, 24, 40, .06); }
.scan-error { background: #fef3f2; color: #b42318; border: 1px solid #fecdca; border-radius: 10px; padding: 10px 12px; margin-bottom: 12px; font-size: 13px; }
.scan-notice { background: #ecfdf3; color: #027a48; border: 1px solid #abefc6; border-radius: 10px; padding: 10px 12px; margin-bottom: 12px; font-size: 13px; }
.scan-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px 12px; }
.scan-grid div { display: flex; justify-content: space-between; gap: 8px; border-bottom: 1px dashed #e4e9f0; padding: 5px 0; font-size: 13px; }
.scan-grid div.full { grid-column: 1 / -1; }
.scan-grid span { color: #667085; white-space: nowrap; }
.scan-grid b { text-align: right; }
.scan-code { margin-top: 12px; font-family: ui-monospace, "Courier New", monospace; font-size: 12px; color: #667085; text-align: center; letter-spacing: .08em; }
.scan-tip { color: #667085; font-size: 12px; line-height: 1.6; margin: 8px 0; }
.scan-primary { width: 100%; border: 0; border-radius: 10px; background: #155eef; color: #fff; padding: 13px; font-size: 16px; font-weight: 600; }
.scan-primary:disabled { opacity: .5; }
.scan-tabs { display: flex; gap: 8px; background: #f2f4f7; border-radius: 10px; padding: 4px; margin-bottom: 12px; }
.scan-tabs button { flex: 1; border: 0; background: transparent; border-radius: 8px; padding: 9px; color: #475467; font-size: 14px; }
.scan-tabs button.active { background: #fff; color: #155eef; font-weight: 600; box-shadow: 0 1px 3px rgba(16, 24, 40, .12); }
.scan-task-list { display: flex; flex-direction: column; gap: 8px; }
.scan-task { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; border: 1px solid #e4e9f0; background: #fff; border-radius: 10px; padding: 10px 12px; text-align: left; color: #344054; }
.scan-task small { color: #667085; font-size: 11px; }
.scan-task.active { border-color: #155eef; background: #eff4ff; }
.scan-card h2 { font-size: 17px; margin: 0 0 12px; }
.scan-form-row { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
.scan-form-row label { width: 80px; color: #667085; font-size: 13px; flex: 0 0 auto; }
.scan-form-row input, .scan-form-row select { flex: 1; min-width: 0; border: 1px solid #d0d5dd; border-radius: 8px; padding: 10px 12px; font-size: 14px; background: #fff; }
.scan-foot { text-align: center; color: #98a2b3; font-size: 11px; padding-top: 12px; }
@media (min-width: 760px) {
  .scan-page { max-width: 560px; margin: 0 auto; }
}
</style>
