<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import DatePicker from '../components/DatePicker.vue'
import PageState from '../components/PageState.vue'
import SceneBadge from '../components/SceneBadge.vue'

/* 业务受理 · 新建项目向导（设计 6.12.6）：第一步选场景，按 A/B/C 分步展示，
 * Web 端用 Tab 分步 + 逐级进入，每一级弹窗不超过浏览器全屏 90%，可逐级返回。 */

const router = useRouter()
const steps = ref<any[]>([])
const scenes = ref<any[]>([])
const rules = ref<any[]>([])
const options = ref<any>({ customers: [], products: [], work_orders: [] })
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const step = ref(0)
const created = ref<any>(null)

const form = reactive({ scene_type: 'A', scene_sub: 'A1', project_code: '', customer_id: '', product_id: '',
  order_qty: 0, source_order_ref: '', required_delivery_date: '', remark: '', create_work_order: true })

const sceneSubs = computed(() => scenes.value.find(s => s.scene_type === form.scene_type)?.subs || [])
const currentSteps = computed<string[]>(() => {
  const found = steps.value.find(s => s.scene_type === form.scene_type)
  return found?.steps || []
})
const idx = (v: string | number) => Number(v)
const allowEmpty = computed(() => rules.value.find(r => r.scene_sub === form.scene_sub)?.allow_empty || [])
const autoFill = computed(() => rules.value.find(r => r.scene_sub === form.scene_sub)?.auto_fill || [])
const sceneColor = computed(() => ({ A: '#2F6BFF', B: '#F59E0B', C: '#8B5CF6' }[form.scene_type] || '#2F6BFF'))
const needsProduct = computed(() => form.scene_type !== 'C')
const needsSourceRef = computed(() => form.scene_type === 'B' || form.scene_type === 'C')

async function load() {
  loading.value = true
  try {
    const [s, sc, r, o] = await Promise.all([
      api<{ scenes: any[] }>('/intake/steps', {}, { silent: true }),
      api<{ items: any[] }>('/scenes', {}, { silent: true }),
      api<{ items: any[] }>('/scenes/rules', {}, { silent: true }),
      api<any>('/intake/options', {}, { silent: true }),
    ])
    steps.value = s.scenes; scenes.value = sc.items; rules.value = r.items; options.value = o
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

function pickScene(type: string) {
  form.scene_type = type
  form.scene_sub = scenes.value.find(s => s.scene_type === type)?.subs?.[0]?.scene_sub || (type === 'C' ? 'C1' : 'A1')
  step.value = 1
  error.value = ''
}
function next() { if (step.value < currentSteps.value.length) step.value++ }
function back() { if (step.value > 1) step.value--; else step.value = 0 }

async function submit() {
  saving.value = true; error.value = ''
  try {
    created.value = await api<any>('/intake/projects', { method: 'POST', body: JSON.stringify({
      ...form, order_qty: Number(form.order_qty) || 0 }) })
    notice.value = `项目已创建（场景 ${created.value.scene_type}${created.value.scene_sub}）`
    step.value = currentSteps.value.length + 1
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

function goNextModule(path: string) { router.push(path) }
onMounted(load)
</script>

<template>
  <div class="wizard" :style="{ '--scene-color': sceneColor }">
    <header class="page-head">
      <div><h2>新建项目向导</h2><p class="sub">按业务场景分步建档；分步 Tab 展示、可逐级返回</p></div>
      <button type="button" class="btn-outline" @click="router.push('/orders/C03')">返回项目工单</button>
    </header>
    <PageState :loading="loading" />

    <!-- 场景选择 -->
    <section v-if="!loading && step === 0" class="scene-picker">
      <h3>第一步 · 选择业务场景</h3>
      <div class="scene-cards">
        <button v-for="s in scenes" :key="s.scene_type" type="button" class="scene-card" :style="{ '--card-color': s.color }" @click="pickScene(s.scene_type)">
          <span class="card-badge">{{ s.badge }}</span>
          <b>{{ s.label }}</b>
          <small>{{ (s.subs || []).map((x: any) => x.label).join(' · ') }}</small>
          <span class="card-arrow">开始 →</span>
        </button>
      </div>
    </section>

    <!-- 分步向导 -->
    <section v-else-if="step > 0 && step <= currentSteps.length" class="wizard-body">
      <div class="step-bar">
        <button v-for="(s, i) in currentSteps" :key="i" type="button" class="step-tab" :class="{ active: step === idx(i) + 1, done: step > idx(i) + 1 }" @click="step = idx(i) + 1">
          <span class="step-no">{{ step > idx(i) + 1 ? '✓' : idx(i) + 1 }}</span>{{ s }}
        </button>
      </div>
      <div class="step-panel">
        <div class="panel-head">
          <SceneBadge :scene-type="form.scene_type" :scene-sub="form.scene_sub" :outsource="form.scene_type !== 'A'" />
          <span class="panel-title">第 {{ step }} 步 · {{ currentSteps[step - 1] }}</span>
          <button type="button" class="btn-mini" @click="step = 0">切换场景</button>
        </div>

        <!-- 第 1 步：来源信息 -->
        <div v-if="step === 1" class="form-grid">
          <label>项目编号*<input v-model="form.project_code" placeholder="如 QD2026111901"></label>
          <label>客商 / 委托方*<select v-model="form.customer_id"><option value="">请选择</option>
            <option v-for="c in options.customers" :key="c.id" :value="c.id">{{ c.name }}</option></select></label>
          <label>对方单号<span v-if="needsSourceRef" class="req">*</span><input v-model="form.source_order_ref" :placeholder="needsSourceRef ? '外部委托方单号' : '选填'"></label>
          <label>场景子类型<select v-model="form.scene_sub"><option v-for="s in sceneSubs" :key="s.scene_sub" :value="s.scene_sub">{{ s.label }}</option></select></label>
        </div>
        <!-- 第 2 步：物料/半成品 -->
        <div v-else-if="step === 2" class="form-grid">
          <label>订单数量*<input v-model.number="form.order_qty" type="number" min="1"></label>
          <label>交期<DatePicker v-model="form.required_delivery_date"/></label>
          <label v-if="needsProduct">产品*<select v-model="form.product_id"><option value="">请选择</option>
            <option v-for="p in options.products" :key="p.id" :value="p.id">{{ p.code }} · {{ p.name }}</option></select></label>
          <div v-else class="empty-note">接外包工序：无产品信息（允许无记录），裁床单仅接收上游扎号/菲票部分信息，产品字段为空，以「—/不适用」呈现。</div>
          <label class="full">备注<input v-model="form.remark" placeholder="客供料代管、工艺要求等"></label>
        </div>
        <!-- 第 3～5 步：链路说明（后续模块承接） -->
        <div v-else class="step-hint">
          <p>{{ currentSteps[step - 1] }}：本向导完成建档后由相应菜单逐级进入。</p>
          <div class="module-links">
            <button type="button" class="btn-outline" @click="goNextModule('/materials/receipts')">物料入库</button>
            <button type="button" class="btn-outline" @click="goNextModule('/cutting/sheets')">裁床</button>
            <button type="button" class="btn-outline" @click="goNextModule('/products/B01')">产品工序</button>
            <button type="button" class="btn-outline" @click="goNextModule('/orders/C03')">项目工单</button>
          </div>
          <p v-if="form.scene_type === 'C'" class="note">C 场景跳过裁床自建环节，允许无产品信息与裁床产品字段。</p>
        </div>

        <div v-if="allowEmpty.length || autoFill.length" class="rules-box">
          <div><b>自动填写：</b>{{ autoFill.join('、') || '—' }}</div>
          <div><b>允许无记录：</b>{{ allowEmpty.join('、') || '—' }} <span class="muted">（以「—/不适用」呈现，不阻断流程）</span></div>
        </div>
        <p v-if="error" class="alert-error">{{ error }}</p>
        <div class="wizard-actions">
          <button type="button" class="btn-outline" @click="back">← 逐级返回</button>
          <button v-if="step < currentSteps.length" type="button" class="btn-primary" @click="next">下一步 →</button>
          <button v-else type="button" class="btn-primary" :disabled="saving || !form.project_code || !form.customer_id || !form.order_qty" @click="submit">完成建档</button>
        </div>
      </div>
    </section>

    <!-- 完成 -->
    <section v-else-if="created" class="done-panel">
      <div class="done-icon">✓</div>
      <h3>建档完成</h3>
      <p class="alert-ok">{{ notice }}</p>
      <div class="done-meta">
        <SceneBadge :scene-type="created.scene_type" :scene-sub="created.scene_sub" :outsource="created.outsource" />
        <span>数据归属：{{ created.order_source === 'own' ? '本厂自有' : '受托加工' }}</span>
      </div>
      <div class="module-links">
        <button type="button" class="btn-primary" @click="goNextModule('/orders/C03')">进入项目工单</button>
        <button type="button" class="btn-outline" @click="goNextModule('/cutting/sheets')">进入裁床</button>
        <button type="button" class="btn-outline" @click="step = 0; created = null">新建另一个项目</button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.wizard { padding: 4px 0; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
h2 { margin: 0; font-size: 18px; font-weight: 600; }
.sub { margin: 4px 0 0; color: #8A9099; font-size: 12px; }
h3 { font-size: 15px; margin: 0 0 14px; }
.btn-primary { padding: 8px 16px; border: 0; border-radius: 8px; background: linear-gradient(135deg, var(--scene-color), #5B8BFF); color: #fff; font-size: 13px; cursor: pointer; }
.btn-primary:disabled { opacity: .5; cursor: not-allowed; }
.btn-outline { padding: 7px 13px; border: 1px solid #DDE3EE; border-radius: 7px; background: #fff; color: #4E5562; font-size: 12px; cursor: pointer; }
.btn-outline:hover { border-color: var(--scene-color); color: var(--scene-color); }
.btn-mini { padding: 3px 9px; border: 1px solid #E5E8EF; border-radius: 6px; background: #fff; font-size: 11px; cursor: pointer; }
.scene-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 14px; }
.scene-card { position: relative; display: flex; flex-direction: column; align-items: flex-start; gap: 7px; padding: 20px;
  border: 1px solid #E5E8EF; border-radius: 14px; background: #fff; cursor: pointer; text-align: left;
  transition: transform .2s, box-shadow .2s, border-color .2s; }
.scene-card:hover { transform: translateY(-3px); border-color: var(--card-color); box-shadow: 0 12px 28px rgba(31,35,41,.1); }
.card-badge { width: 30px; height: 30px; line-height: 30px; text-align: center; border-radius: 9px; color: #fff; font-weight: 700; background: var(--card-color); }
.scene-card b { font-size: 15px; color: #1F2329; }
.scene-card small { color: #8A9099; font-size: 12px; line-height: 1.6; }
.card-arrow { margin-top: 8px; color: var(--card-color); font-size: 12px; font-weight: 600; }
.step-bar { display: flex; gap: 4px; flex-wrap: wrap; margin-bottom: 14px; padding: 5px; background: #F1F3F7; border-radius: 10px; }
.step-tab { display: inline-flex; align-items: center; gap: 6px; padding: 7px 12px; border: 0; border-radius: 7px; background: transparent;
  color: #6B7280; font-size: 12px; cursor: pointer; }
.step-tab.active { color: #fff; background: var(--scene-color); box-shadow: 0 2px 8px rgba(47,107,255,.24); }
.step-tab.done { color: #16A34A; background: #DCFCE7; }
.step-no { width: 17px; height: 17px; line-height: 17px; text-align: center; border-radius: 50%; background: rgba(0,0,0,.07); font-size: 10px; }
.step-tab.active .step-no { background: rgba(255,255,255,.26); }
.step-panel { border: 1px solid #E5E8EF; border-radius: 12px; padding: 18px; background: #fff; max-width: 900px; }
.panel-head { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; padding-bottom: 12px; border-bottom: 1px solid #EEF1F6; }
.panel-title { font-size: 14px; font-weight: 600; }
.panel-head .btn-mini { margin-left: auto; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.form-grid label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: #4E5562; }
.form-grid input, .form-grid select { padding: 7px 9px; border: 1px solid #DDE3EE; border-radius: 7px; font-size: 13px; }
.form-grid .full { grid-column: 1 / -1; }
.req { color: #DC2626; }
.empty-note, .note { grid-column: 1 / -1; padding: 10px 12px; border-radius: 8px; background: #FFF7ED; border: 1px solid #FED7AA; color: #9A3412; font-size: 12px; line-height: 1.6; }
.step-hint p { font-size: 13px; color: #4E5562; line-height: 1.7; }
.module-links { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
.rules-box { margin-top: 16px; padding: 10px 12px; border-radius: 8px; background: #F8FAFF; border: 1px dashed #C7D7FE; font-size: 12px; color: #4E5562; line-height: 1.8; }
.muted { color: #8A9099; }
.alert-error { color: #DC2626; background: #FEF2F2; border: 1px solid #FECACA; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.alert-ok { color: #16A34A; background: #F0FDF4; border: 1px solid #BBF7D0; padding: 8px 12px; border-radius: 7px; font-size: 12px; }
.wizard-actions { display: flex; justify-content: space-between; margin-top: 18px; }
.done-panel { text-align: center; padding: 40px 20px; border: 1px solid #E5E8EF; border-radius: 14px; background: #fff; }
.done-icon { width: 48px; height: 48px; line-height: 48px; margin: 0 auto 12px; border-radius: 50%; background: #DCFCE7; color: #16A34A; font-size: 24px; }
.done-meta { display: flex; align-items: center; justify-content: center; gap: 14px; margin: 14px 0; font-size: 12px; color: #4E5562; }
.done-panel .module-links { justify-content: center; }
</style>
