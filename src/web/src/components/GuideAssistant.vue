<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { guides, permissionNames, type Guide, type GuideStep } from '../guides'

/**
 * 体验助手：右下角悬浮开关（可随时开启/关闭），选择功能后按脚本逐步引导——
 * 自动跳转页面、高亮目标元素并显示动画箭头（游戏教学式）。
 *
 * 按当前用户权限导航：无权限的步骤自动跳过，引导结束后提示
 * 「添加用户并分配权限」或「切换到有权限的账号登录」；整条引导都无权限时直接进入开通引导。
 */
const props = defineProps<{ permissions?: Record<string, boolean> }>()
const perms = computed(() => props.permissions || {})
const router = useRouter()
const panelOpen = ref(false)
const running = ref(false)
const guide = ref<Guide | null>(null)
const stepIndex = ref(0)
const rect = ref<DOMRect | null>(null)
const viewport = ref({ w: window.innerWidth, h: window.innerHeight })
const skippedFeatures = ref<string[]>([])
const permHint = ref(false)

const step = () => (guide.value && guide.value.steps[stepIndex.value]) || null
const progress = () => guide.value ? `${stepIndex.value + 1}/${guide.value.steps.length}` : ''

function hasPerm(p?: string) { return !p || !!perms.value[p] }
function permLabel(p: string) { return permissionNames[p] || p }
function missingOf(item: Guide) {
  const out: string[] = []
  for (const s of item.steps) {
    if (s.permission && !hasPerm(s.permission)) {
      const name = s.label || permLabel(s.permission)
      if (!out.includes(name)) out.push(name)
    }
  }
  return out
}
function itemDesc(item: Guide) {
  const missing = missingOf(item)
  const accessible = item.steps.filter(s => hasPerm(s.permission))
  if (accessible.length === 0) return `当前账号暂无权限：${missing.join('、')} · 点击查看如何开通`
  if (missing.length) return `${item.desc} · 缺权限：${missing.join('、')}（将自动跳过）`
  return `${item.desc} · ${item.steps.length} 步`
}

/* 「如何开通权限」引导：添加用户并分配权限 / 切换账号登录 */
const permGuide = computed<Guide>(() => {
  const steps: GuideStep[] = [
    { route: '/masterdata/B06', targetText: '员工与账号', title: '方式一 · 添加用户（1/2）', text: '由您厂的管理员在「员工与账号」中新增员工并创建用户账号。' },
  ]
  if (hasPerm('identity:read')) {
    steps.push({ route: '/identity/G05', targetText: '用户角色权限', title: '方式一 · 分配权限（2/2）', text: '在「用户角色权限」中为账号勾选所需权限，保存后重新登录生效。' })
  } else {
    steps.push({ title: '方式一 · 分配权限（2/2）', text: '「用户角色权限」页面需要 identity:read 权限才能查看，请联系您厂的管理员为您分配。' })
  }
  steps.push({ title: '方式二 · 切换账号登录', text: '如果已有带权限的账号：点击下方「退出并切换账号」，退出后用该账号重新登录即可。' })
  return { id: 'perm-guide', name: '如何开通权限', desc: '', steps }
})

function panelStyle() {
  const r = rect.value
  const top = r ? (r.bottom + 80 + 12 < viewport.value.h ? r.bottom + 80 + 12 : Math.max(16, r.top - 80 - 190)) : 0
  const left = r ? Math.max(16, Math.min(viewport.value.w - 310, r.left + r.width / 2 - 150)) : 0
  return { left: left + 'px', top: top + 'px' }
}

function arrowStyle() {
  const r = rect.value
  if (!r) return { display: 'none' }
  const panel = panelStyle()
  const fromTop = parseInt(panel.top) > r.bottom
  const x = Math.max(30, Math.min(viewport.value.w - 60, r.left + r.width / 2 - 12))
  return fromTop
    ? { left: x + 'px', top: r.bottom + 8 + 'px' }
    : { left: x + 'px', top: r.top - 32 + 'px', transform: 'rotate(180deg)' }
}

function findTarget(step: GuideStep): Element | null {
  if (step.targetText) {
    const links = Array.from(document.querySelectorAll<HTMLElement>('.sidebar a'))
    const hit = links.find(a => (a.textContent || '').trim().includes(step.targetText!)
      || a.getAttribute('title') === step.targetText)
    if (hit) return hit
  }
  if (step.selector) {
    const el = document.querySelector(step.selector)
    if (el) return el
  }
  return null
}

async function measure() {
  viewport.value = { w: window.innerWidth, h: window.innerHeight }
  const current = step()
  if (!current || !running.value) { rect.value = null; return }
  await nextTick()
  await new Promise(r => setTimeout(r, current.route ? 420 : 60))
  const el = findTarget(current)
  rect.value = el ? el.getBoundingClientRect() : null
}

async function start(item: Guide) {
  panelOpen.value = false
  permHint.value = false
  const missing = missingOf(item)
  const accessible = item.steps.filter(s => hasPerm(s.permission))
  /* 整条引导都无权限 → 直接进入「如何开通权限」引导 */
  if (accessible.length === 0) { await startRaw(permGuide.value, missing); return }
  await startRaw({ ...item, steps: accessible }, missing)
}

async function startRaw(item: Guide, missing: string[]) {
  running.value = true
  guide.value = item
  skippedFeatures.value = missing
  stepIndex.value = 0
  await measure()
}

async function go(delta: number) {
  if (!guide.value) return
  const nextIndex = stepIndex.value + delta
  if (nextIndex < 0) return
  if (nextIndex >= guide.value.steps.length) {
    /* 引导结束：有被跳过的越权功能时，提示如何开通权限 */
    if (delta > 0 && skippedFeatures.value.length) { permHint.value = true; rect.value = null; return }
    stop(); return
  }
  permHint.value = false
  const target = guide.value.steps[nextIndex]
  stepIndex.value = nextIndex
  if (target.route && router.currentRoute.value.path !== target.route) {
    await router.push(target.route)
  } else {
    const el = findTarget(target)
    el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }
  await measure()
}

function stop() {
  running.value = false
  guide.value = null
  rect.value = null
  permHint.value = false
  skippedFeatures.value = []
}

function switchAccount() {
  localStorage.removeItem('clothp_access_token')
  localStorage.removeItem('clothp_refresh_token')
  window.location.href = '/login'
}

function onResize() { if (running.value) void measure() }

onMounted(() => {
  window.addEventListener('resize', onResize)
  window.addEventListener('scroll', onResize, true)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  window.removeEventListener('scroll', onResize, true)
})
</script>

<template>
  <!-- 悬浮开关 -->
  <button v-if="!running" type="button" class="ga-fab" :class="{ active: panelOpen }" title="体验助手" @click="panelOpen = !panelOpen">
    <span v-if="!panelOpen">?</span><span v-else>×</span>
  </button>

  <!-- 功能选择面板 -->
  <div v-if="panelOpen && !running" class="ga-panel">
    <div class="ga-panel-head">
      <strong>体验助手</strong>
      <small>按您的权限推荐功能；无权限的功能会提示如何开通</small>
    </div>
    <button v-for="g in guides" :key="g.id" type="button" class="ga-item" @click="start(g)">
      <strong>{{ g.name }}<span v-if="missingOf(g).length" class="ga-lock" title="当前账号缺少部分权限">🔒</span></strong>
      <small>{{ itemDesc(g) }}</small>
    </button>
  </div>

  <!-- 引导中：高亮框 + 动画箭头 + 提示卡 -->
  <template v-if="running && guide && !permHint">
    <div class="ga-mask" @click="stop"></div>
    <div v-if="rect" class="ga-spot" :style="{ left: rect.left - 6 + 'px', top: rect.top - 6 + 'px', width: rect.width + 12 + 'px', height: rect.height + 12 + 'px' }"></div>
    <div v-if="rect" class="ga-arrow" :style="arrowStyle()"><span></span></div>
    <div class="ga-card" :style="rect ? panelStyle() : { left: '50%', top: '38%', transform: 'translateX(-50%)' }">
      <div class="ga-card-head">
        <span class="ga-progress">{{ progress() }}</span>
        <strong>{{ step()?.title }}</strong>
      </div>
      <p>{{ step()?.text }}</p>
      <div class="ga-card-actions">
        <button type="button" class="ga-btn ghost" @click="stop">退出</button>
        <button v-if="stepIndex > 0" type="button" class="ga-btn ghost" @click="go(-1)">上一步</button>
        <button v-if="guide.id === 'perm-guide' && stepIndex === guide.steps.length - 1" type="button" class="ga-btn solid" @click="switchAccount">退出并切换账号</button>
        <button type="button" class="ga-btn solid" @click="go(1)">{{ stepIndex === guide.steps.length - 1 ? (skippedFeatures.length ? '下一步' : '完成') : '下一步' }}</button>
      </div>
    </div>
  </template>

  <!-- 引导结束：越权功能提示（开通权限 / 切换账号） -->
  <template v-if="running && permHint">
    <div class="ga-mask" @click="stop"></div>
    <div class="ga-card" style="left:50%;top:34%;transform:translateX(-50%)">
      <div class="ga-card-head">
        <span class="ga-progress">权限提示</span>
        <strong>以下功能超出当前账号权限</strong>
      </div>
      <p><span class="ga-chips"><span v-for="f in skippedFeatures" :key="f" class="ga-chip">{{ f }}</span></span></p>
      <p>您可以：<br>① 由管理员在「用户角色权限」中为您的账号分配上述权限；<br>② 或切换到有权限的账号登录。</p>
      <div class="ga-card-actions">
        <button type="button" class="ga-btn ghost" @click="stop">完成</button>
        <button type="button" class="ga-btn ghost" @click="switchAccount">退出并切换账号</button>
        <button type="button" class="ga-btn solid" @click="start(permGuide)">查看如何开通</button>
      </div>
    </div>
  </template>
</template>

<style scoped>
.ga-fab{position:fixed;right:22px;bottom:88px;z-index:1200;width:48px;height:48px;border-radius:50%;border:none;cursor:pointer;
  color:#fff;font-size:20px;font-weight:700;background:linear-gradient(135deg,#2563eb,#06b6d4);
  box-shadow:0 10px 26px rgba(37,99,235,.4);transition:transform .15s;font-family:inherit}
.ga-fab:hover{transform:scale(1.08)}
.ga-fab.active{background:linear-gradient(135deg,#334155,#64748b)}
.ga-panel{position:fixed;right:22px;bottom:146px;z-index:1200;width:264px;background:#fff;border:1px solid #e2e8f0;
  border-radius:14px;padding:14px;box-shadow:0 18px 48px rgba(15,23,42,.18)}
.ga-panel-head{display:flex;flex-direction:column;gap:2px;margin-bottom:10px}
.ga-panel-head strong{font-size:15px;color:#0f172a}
.ga-panel-head small{color:#64748b;font-size:12px}
.ga-item{display:flex;flex-direction:column;gap:2px;width:100%;text-align:left;border:1px solid #e2e8f0;background:#f8fafc;
  border-radius:10px;padding:10px 12px;margin-top:8px;cursor:pointer;font-family:inherit}
.ga-item:hover{border-color:#2563eb;background:#eff6ff}
.ga-item strong{font-size:13.5px;color:#0f172a}
.ga-item small{font-size:12px;color:#64748b}
.ga-lock{font-size:12px;margin-left:6px}
.ga-mask{position:fixed;inset:0;z-index:1150;background:rgba(15,23,42,.45)}
.ga-spot{position:fixed;z-index:1160;border-radius:10px;border:2px solid #38bdf8;pointer-events:none;
  box-shadow:0 0 0 9999px rgba(15,23,42,.45),0 0 22px rgba(56,189,248,.75);transition:all .25s ease}
.ga-arrow{position:fixed;z-index:1170;pointer-events:none}
.ga-arrow span{display:block;width:0;height:0;border-left:12px solid transparent;border-right:12px solid transparent;
  border-top:18px solid #38bdf8;animation:ga-bounce .9s ease-in-out infinite;filter:drop-shadow(0 2px 6px rgba(56,189,248,.6))}
@keyframes ga-bounce{0%,100%{transform:translateY(0)}50%{transform:translateY(10px)}}
.ga-card{position:fixed;z-index:1180;width:320px;background:#fff;border:1px solid #e2e8f0;border-radius:14px;padding:14px 16px;
  box-shadow:0 18px 48px rgba(15,23,42,.25)}
.ga-card-head{display:flex;align-items:center;gap:8px;margin-bottom:6px}
.ga-progress{font-size:11px;font-weight:700;color:#1d4ed8;background:#dbeafe;border-radius:999px;padding:2px 8px}
.ga-card-head strong{font-size:15px;color:#0f172a}
.ga-card p{margin:4px 0 12px;font-size:13px;line-height:1.7;color:#475569}
.ga-chips{display:flex;flex-wrap:wrap;gap:6px}
.ga-chip{font-size:12px;color:#b45309;background:#fef3c7;border-radius:999px;padding:2px 10px}
.ga-card-actions{display:flex;gap:8px;justify-content:flex-end;flex-wrap:wrap}
.ga-btn{border-radius:8px;padding:7px 14px;font-size:12.5px;font-weight:600;cursor:pointer;border:1px solid transparent;font-family:inherit}
.ga-btn.solid{background:linear-gradient(90deg,#2563eb,#06b6d4);color:#fff}
.ga-btn.ghost{background:#fff;border-color:#cbd5e1;color:#475569}
.ga-btn:hover{filter:brightness(1.05)}
@media(max-width:640px){.ga-fab{right:14px;bottom:70px}.ga-panel{right:14px;width:calc(100vw - 28px)}.ga-card{width:calc(100vw - 40px)}}
</style>
