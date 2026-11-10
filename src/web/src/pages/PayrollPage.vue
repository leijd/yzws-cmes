<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, command } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import MonthPicker from '../components/MonthPicker.vue'
import FilterSelect from '../components/FilterSelect.vue'

const { has } = usePermissions()
const payrollStatusText: Record<string, string> = { draft: '草稿', confirmed: '待支付', partially_paid: '部分支付', paid: '全部支付', locked: '已锁定' }
const canConfirm = computed(() => has('payroll:confirm'))
const canPay = computed(() => has('payroll:pay'))

const sheets = ref<any[]>([])
const employees = ref<any[]>([])
const period = ref(new Date().toISOString().slice(0, 7))
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const detail = ref<any>(null)
const detailSheet = ref<any>(null)
const detailItems = ref<any[]>([])
const calculation = ref<any>(null)
const payment = ref('0')
const payTarget = ref<any>(null)
const itemEdits = reactive<Record<string, string>>({})
const adjustment = reactive({ adjustment_type: 'manual', amount: '', reason: '', target_period: '' })

const editableItems = ['base_salary', 'position_allowance', 'attendance_bonus', 'meal_allowance', 'defect_deduction', 'late_deduction', 'advance_deduction', 'social_insurance', 'tax']
const deductionItems = new Set(['defect_deduction', 'late_deduction', 'advance_deduction', 'social_insurance', 'tax'])

async function load() {
  loading.value = true; error.value = ''
  try {
    const [s, e] = await Promise.all([api<{ items: any[] }>(`/payroll-sheets?period=${period.value}`), api<{ items: any[] }>('/employees')])
    sheets.value = s.items; employees.value = e.items
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function generate() {
  error.value = ''
  try {
    const result = await api<{ results: any[] }>('/payroll-sheets/generate', { method: 'POST', body: JSON.stringify({ period: period.value, employee_ids: employees.value.map(e => e.id), source_version: 'web-' + Date.now() }) })
    notice.value = `工资草稿生成完成，共 ${result.results.filter(r => r.status === 'succeeded').length} 份`
    await load()
  } catch (e) { error.value = (e as Error).message }
}
async function openDetail(item: any) {
  error.value = ''; detail.value = item; detailItems.value = []; calculation.value = null
  try {
    const [sheet, calc] = await Promise.all([api<any>(`/payroll-sheets/${item.id}`), api<any>(`/payroll-sheets/${item.id}/calculation`)])
    detailSheet.value = sheet.sheet; detailItems.value = sheet.items; calculation.value = calc
    for (const code of editableItems) {
      const found = sheet.items.find((i: any) => i.item_code === code)
      const raw = found ? Number(found.amount) : 0
      itemEdits[code] = String(Math.abs(raw))
    }
    adjustment.target_period = sheet.sheet.payroll_period || period.value
    adjustment.amount = ''; adjustment.reason = ''
  } catch (e) { error.value = (e as Error).message }
}
async function saveItem(code: string) {
  if (!detail.value) return
  saving.value = true; error.value = ''
  try {
    await api(`/payroll-sheets/${detail.value.id}/items/${code}`, { method: 'PUT', body: JSON.stringify({ amount: itemEdits[code] || '0', reason: 'web 调整' }) })
    notice.value = '工资项已更新'
    await openDetail(detail.value); await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function addAdjustment() {
  if (!detail.value) return
  if (!adjustment.amount || !adjustment.reason.trim()) { error.value = '调整金额和原因必填'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/payroll-sheets/${detail.value.id}/adjustments`, { method: 'POST', body: JSON.stringify({ ...adjustment }) })
    notice.value = '工资调整已记录'
    await openDetail(detail.value); await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function openConfirm(item: any) {
  error.value = ''
  try {
    const detailData = await api<any>(`/payroll-sheets/${item.id}`)
    await api(`/payroll-sheets/${item.id}/confirm`, { method: 'POST', body: JSON.stringify({ version: detailData.sheet.version, source_snapshot_hash: detailData.sheet.source_snapshot_hash, second_factor: '' }) })
    notice.value = '工资单已确认，来源快照已锁定'; await load()
  } catch (e) { error.value = (e as Error).message }
}
async function openPay(item: any) {
  payTarget.value = item; payment.value = item.outstanding_amount; error.value = ''
}
async function pay() {
  if (!payTarget.value) return
  saving.value = true; error.value = ''
  try {
    const detailData = await api<any>(`/payroll-sheets/${payTarget.value.id}`)
    await command(`/payroll-sheets/${payTarget.value.id}/payments`, { version: detailData.sheet.version, amount: payment.value, payment_method: 'bank_transfer', second_factor: '' })
    payTarget.value = null; notice.value = '付款流水已追加，未发余额已更新'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>工资单管理</h1><p>23 列工资口径、项目调整、付款与计算说明</p></div><div class="header-actions"><MonthPicker v-model="period" @update:modelValue="load"/><button v-if="canConfirm" class="button" @click="generate">生成工资草稿</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><PageState :loading="loading"/>
    <div v-if="!loading" class="table-wrap"><table><thead><tr><th>工号</th><th>姓名</th><th>岗位</th><th>车间/产线</th><th>出勤</th><th>计件数</th><th>计件金额</th><th>底薪</th><th>岗位补贴</th><th>全勤奖</th><th>餐补</th><th>次品扣款</th><th>迟到扣款</th><th>预支扣款</th><th>应发</th><th>社保</th><th>个税</th><th>实发</th><th>已发</th><th>未发</th><th>银行卡</th><th>签名</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in sheets" :key="item.id"><td>{{ item.employee_no }}</td><td>{{ item.employee_name }}</td><td>{{ item.position || '—' }}</td><td>{{ [item.workshop, item.production_line].filter(Boolean).join(' / ') || '—' }}</td><td>{{ item.attendance_days }}</td><td>{{ item.piecework_qty }}</td><td>{{ item.piecework_amount }}</td><td>{{ item.base_salary }}</td><td>{{ item.position_allowance }}</td><td>{{ item.attendance_bonus }}</td><td>{{ item.meal_allowance }}</td><td>{{ item.defect_deduction }}</td><td>{{ item.late_deduction }}</td><td>{{ item.advance_deduction }}</td><td><strong>{{ item.gross_amount }}</strong></td><td>{{ item.social_insurance }}</td><td>{{ item.tax }}</td><td><strong>{{ item.net_amount }}</strong></td><td>{{ item.paid_amount }}</td><td>{{ item.outstanding_amount }}</td><td class="text-muted">{{ item.bank_card_masked }}</td><td>{{ item.signature_status }}</td><td><span class="status" :class="item.status">{{ payrollStatusText[item.status] || item.status }}</span></td><td><button class="button link" @click="openDetail(item)">明细/调整</button><button v-if="item.status === 'draft' && canConfirm" class="button link" @click="openConfirm(item)">确认工资</button><button v-if="['confirmed','partially_paid'].includes(item.status) && canPay" class="button link" @click="openPay(item)">追加付款</button></td></tr></tbody></table><PageState :empty="!sheets.length"/></div>
  </section>

  <ModalDialog v-if="detail" wide :title="`工资明细 · ${detail.employee_name || ''}`" @close="detail = null">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="quantity-summary"><span>应发<strong>{{ detailSheet?.gross_amount }}</strong></span><span>实发<strong>{{ detailSheet?.net_amount }}</strong></span><span>已发<strong>{{ detailSheet?.paid_amount }}</strong></span><span>未发<strong>{{ detailSheet?.outstanding_amount }}</strong></span></div>
    <h3>工资项目</h3>
    <div class="table-wrap" style="max-height:36vh"><table><thead><tr><th>项目</th><th>数量</th><th>金额</th><th>维护</th></tr></thead><tbody><tr v-for="item in detailItems" :key="item.item_code"><td>{{ item.item_name }} <small class="text-muted">{{ item.item_code }}</small></td><td>{{ item.qty ?? '—' }}</td><td>{{ item.amount }}</td><td><template v-if="detailSheet?.status === 'draft' && canConfirm && editableItems.includes(item.item_code)"><input v-model="itemEdits[item.item_code]" inputmode="decimal" style="width:100px"> <button class="button link" :disabled="saving" @click="saveItem(item.item_code)">保存</button></template></td></tr></tbody></table></div>
    <div class="notice" style="margin-top:12px">{{ calculation?.formula?.gross_amount }}；{{ calculation?.formula?.net_amount }}；{{ calculation?.formula?.outstanding }}</div>
    <template v-if="detailSheet?.status !== 'draft' && canConfirm"><h3>工资调整</h3><div class="form-grid"><div class="field"><label>类型</label><FilterSelect v-model="adjustment.adjustment_type" :options="[{value:'manual',label:'手工'},{value:'quality',label:'质量'},{value:'report',label:'报工'},{value:'correction',label:'纠错'}]"/></div><div class="field"><label>金额（正加负减）</label><input v-model="adjustment.amount" inputmode="decimal"></div><div class="field"><label>目标期间</label><input v-model="adjustment.target_period" placeholder="YYYY-MM"></div><div class="field full"><label class="required">原因</label><input v-model="adjustment.reason"></div></div><div class="form-actions"><button class="button" :disabled="saving || !adjustment.amount || !adjustment.reason" @click="addAdjustment">记录调整</button></div></template>
    <template v-if="calculation?.payments?.length"><h3>付款流水</h3><div class="table-wrap"><table><thead><tr><th>金额</th><th>时间</th><th>方式</th><th>撤销</th></tr></thead><tbody><tr v-for="p in calculation.payments" :key="p.id"><td>{{ p.amount }}</td><td>{{ p.paid_at }}</td><td>{{ p.payment_method }}</td><td>{{ p.reversal_of_id ? '是' : '否' }}</td></tr></tbody></table></div></template>
    <template #footer><button class="button secondary" @click="detail = null">关闭</button></template>
  </ModalDialog>

  <ModalDialog v-if="payTarget" :title="`追加付款 · ${payTarget.employee_name}`" @close="payTarget = null"><div class="quantity-summary"><span>实发<strong>{{ payTarget.net_amount }}</strong></span><span>已发<strong>{{ payTarget.paid_amount }}</strong></span><span>未发<strong>{{ payTarget.outstanding_amount }}</strong></span></div><div class="field"><label>本次付款金额</label><input v-model="payment" inputmode="decimal"></div><div class="notice">付款只追加流水，不覆盖原始工资项目；超过未发余额将被服务端拒绝。</div><template #footer><button class="button secondary" @click="payTarget = null">取消</button><button v-if="canPay" class="button" :disabled="saving" @click="pay">确认付款</button></template></ModalDialog>
</template>
