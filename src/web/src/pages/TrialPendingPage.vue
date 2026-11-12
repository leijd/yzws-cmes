<script setup lang="ts">
/**
 * 体验厂审批中页面：提交体验注册后等待平台管理员审核。
 * - 显示厂信息与审批状态（正在审批）
 * - 「加急申请」按钮：提交后平台管理员审批列表显示加急图标并置顶（同一厂只能加急一次）
 * - 审批通过后回体验主页点「开始体验」即可进入工作台
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import NetworkBackground from '../components/NetworkBackground.vue'
import { toastError, toastSuccess } from '../toast'
import { readTrialSession, trialExpedite, trialInfo, trialUpdate, maskPhone } from '../trial'

const router = useRouter()
const session = readTrialSession()
const expedited = ref(false)
const submitting = ref(false)
const tenantCode = computed(() => session?.tenantCode || '')
const tenantName = computed(() => session?.trialName || session?.displayName || '体验制衣厂')
const scaleOptions = [
  { value: '', label: '请选择（可选）' },
  { value: 'micro', label: '微型（10 人以下）' },
  { value: 'small', label: '小型（10-50 人）' },
  { value: 'medium', label: '中型（50-200 人）' },
  { value: 'large', label: '大型（200 人以上）' },
]

/* 注册信息：等待审批期间可修改（联系电话 = 体验用户登录手机号，锁定不可改） */
const info = reactive({
  name: '', region: '', scale: '', contact_name: '', contact_phone: '',
  contact_wechat: '', contact_qq: '', contact_alipay: '', address: '', remark: '',
})
const infoOpen = ref(false)
const infoLoading = ref(false)
const savingInfo = ref(false)

async function loadInfo() {
  if (!session?.entryToken || !tenantCode.value) return
  infoLoading.value = true
  try {
    const r = await trialInfo(session.entryToken, tenantCode.value)
    Object.assign(info, {
      name: r.name, region: r.region, scale: r.scale,
      contact_name: r.contact_name, contact_phone: r.contact_phone,
      contact_wechat: r.contact_wechat, contact_qq: r.contact_qq, contact_alipay: r.contact_alipay,
      address: r.address, remark: r.remark,
    })
    expedited.value = r.expedited
  } catch { /* 凭证失效等：保持占位 */ }
  finally { infoLoading.value = false }
}

async function saveInfo() {
  if (!session?.entryToken || !tenantCode.value) { toastError('体验凭证已失效，请重新登录'); return }
  if (savingInfo.value) return
  savingInfo.value = true
  try {
    const r = await trialUpdate(session.entryToken, tenantCode.value, {
      name: info.name, region: info.region, scale: info.scale,
      contact_name: info.contact_name, contact_wechat: info.contact_wechat, contact_qq: info.contact_qq,
      contact_alipay: info.contact_alipay, address: info.address, remark: info.remark,
    })
    toastSuccess(r.message || '注册信息已更新')
    infoOpen.value = false
    await loadInfo()
  } catch (e) {
    toastError((e as Error).message || '保存失败，请稍后重试')
  } finally {
    savingInfo.value = false
  }
}

async function expedite() {
  if (!session?.entryToken || !tenantCode.value) {
    toastError('体验凭证已失效，请返回体验主页重新用手机验证码登录')
    return
  }
  if (submitting.value) return
  submitting.value = true
  try {
    const r = await trialExpedite(session.entryToken, tenantCode.value)
    expedited.value = true
    toastSuccess(r.message || '加急申请已提交')
  } catch (e) {
    toastError((e as Error).message || '加急申请失败，请稍后重试')
  } finally {
    submitting.value = false
  }
}

onMounted(loadInfo)
</script>

<template>
  <div class="tp-page">
    <NetworkBackground theme="aurora" />
    <div class="tp-card">
      <div class="tp-brand"><span class="tp-mark">智</span><strong>一智算 · 免费体验</strong></div>
      <div class="tp-status-icon"><span class="tp-clock">⏳</span></div>
      <h1>体验注册已提交，正在审批</h1>
      <p class="tp-desc">
        体验制衣厂 <strong>{{ tenantName }}</strong>
        <span v-if="tenantCode" class="tp-code">{{ tenantCode }}</span>
        已提交，平台管理员审核通过后，将以手机号
        <strong>{{ session ? maskPhone(session.phone) : '' }}</strong>
        作为该厂管理员账户开通，并开始计算 30 天体验期。
      </p>
      <ol class="tp-steps">
        <li class="done"><span>✓</span> 提交体验注册申请</li>
        <li :class="{ now: !expedited }"><span>⏳</span> 平台管理员审核{{ expedited ? '（已加急，优先处理）' : '' }}</li>
        <li><span>3</span> 审核通过 → 点击「开始体验」进入工作台</li>
      </ol>
      <div class="tp-actions">
        <button v-if="!expedited" type="button" class="tp-btn solid" :disabled="submitting" @click="expedite">
          {{ submitting ? '正在提交…' : '🔥 加急申请' }}
        </button>
        <span v-else class="tp-expedited">🔥 已加急，平台管理员将优先处理</span>
        <button type="button" class="tp-btn ghost" @click="infoOpen = !infoOpen">
          {{ infoOpen ? '收起注册信息' : '查看/修改注册信息' }}
        </button>
        <button type="button" class="tp-btn ghost" @click="router.push('/trial/home')">返回体验主页</button>
      </div>

      <!-- 注册信息：等待审批期间可修改；联系电话为体验用户登录手机号，不可修改 -->
      <div v-if="infoOpen" class="tp-info">
        <div v-if="infoLoading" class="tp-info-tip">正在加载注册信息…</div>
        <template v-else>
          <div class="tp-form">
            <div class="tp-field full"><label class="required">制衣厂名称</label><input v-model.trim="info.name" maxlength="60" placeholder="与营业执照一致的厂名"></div>
            <div class="tp-field"><label>所在国 / 地区</label><input v-model.trim="info.region" maxlength="40" placeholder="如 中国广东 / 越南"></div>
            <div class="tp-field"><label>厂规模</label><select v-model="info.scale"><option v-for="o in scaleOptions" :key="o.value" :value="o.value">{{ o.label }}</option></select></div>
            <div class="tp-field"><label>联系人</label><input v-model.trim="info.contact_name" maxlength="30"></div>
            <div class="tp-field"><label>联系电话（体验用户登录手机号，不可修改）</label><input :value="maskPhone(info.contact_phone)" disabled></div>
            <div class="tp-field"><label>微信号</label><input v-model.trim="info.contact_wechat" maxlength="64" placeholder="选填"></div>
            <div class="tp-field"><label>QQ</label><input v-model.trim="info.contact_qq" maxlength="32" placeholder="选填"></div>
            <div class="tp-field"><label>支付宝账号</label><input v-model.trim="info.contact_alipay" maxlength="64" placeholder="选填"></div>
            <div class="tp-field full"><label>厂地址</label><input v-model.trim="info.address" maxlength="120"></div>
            <div class="tp-field full"><label>备注</label><input v-model.trim="info.remark" maxlength="120" placeholder="主营品类、车间与产线数量等（可选）"></div>
          </div>
          <div class="tp-info-actions">
            <button type="button" class="tp-btn solid" :disabled="savingInfo || !info.name" @click="saveInfo">
              {{ savingInfo ? '正在保存…' : '保存修改' }}
            </button>
            <small>平台管理员审批时将看到最新内容；联系电话不可修改（以体验用户登录手机号为准）。</small>
          </div>
        </template>
      </div>
      <p class="tp-tip">审批通过后，回到体验主页点击右侧「开始体验」即可进入一智算工作台；本页可随时查看审批状态。</p>
    </div>
  </div>
</template>

<style scoped>
.tp-page{min-height:100vh;display:grid;place-items:center;padding:32px 20px;position:relative;
  font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;color:#17255f;
  background:linear-gradient(160deg,#eaf3ff 0%,#f5f9ff 45%,#eef8fc 100%)}
.tp-card{position:relative;z-index:1;width:min(560px,100%);background:#fff;border:1px solid #e3ecfb;border-radius:20px;
  padding:36px 40px;box-shadow:0 18px 48px rgba(23,37,95,.10);text-align:center}
.tp-brand{display:flex;align-items:center;justify-content:center;gap:8px;font-size:15px;color:#41537e;margin-bottom:20px}
.tp-mark{display:grid;place-items:center;width:30px;height:30px;border-radius:9px;color:#fff;font-size:13px;
  background:linear-gradient(135deg,#2563eb,#06b6d4)}
.tp-status-icon{margin:4px 0 10px}
.tp-clock{display:inline-grid;place-items:center;width:64px;height:64px;border-radius:50%;font-size:28px;
  background:#eff6ff;border:2px solid #bfdbfe;animation:tpFloat 2.4s ease-in-out infinite}
@keyframes tpFloat{0%,100%{transform:translateY(0)}50%{transform:translateY(-6px)}}
h1{margin:0 0 10px;font-size:22px}
.tp-desc{margin:0 auto 18px;max-width:440px;font-size:13.5px;line-height:1.9;color:#475569}
.tp-code{display:inline-block;margin-left:6px;font-size:11.5px;font-weight:700;color:#1d4ed8;background:#dbeafe;border-radius:999px;padding:2px 9px}
.tp-steps{list-style:none;margin:0 auto 22px;padding:0;max-width:380px;text-align:left;display:flex;flex-direction:column;gap:10px}
.tp-steps li{display:flex;align-items:center;gap:10px;font-size:13.5px;color:#94a3b8}
.tp-steps li span{display:grid;place-items:center;width:26px;height:26px;border-radius:50%;font-size:12px;flex:none;
  background:#f1f5f9;color:#94a3b8}
.tp-steps li.done{color:#059669}.tp-steps li.done span{background:#d1fae5;color:#059669}
.tp-steps li.now{color:#1d4ed8;font-weight:600}.tp-steps li.now span{background:#dbeafe;color:#1d4ed8}
.tp-actions{display:flex;align-items:center;justify-content:center;gap:12px;flex-wrap:wrap;margin-bottom:14px}
.tp-btn{border-radius:10px;padding:11px 20px;font-size:14px;font-weight:600;cursor:pointer;border:1px solid transparent;font-family:inherit}
.tp-btn.solid{background:linear-gradient(90deg,#2563eb,#06b6d4);color:#fff;box-shadow:0 8px 20px rgba(37,99,235,.25)}
.tp-btn.ghost{background:#fff;border-color:#cbd5e1;color:#475569}
.tp-btn:disabled{opacity:.6;cursor:wait}
.tp-expedited{font-size:13px;font-weight:700;color:#b45309;background:#fef3c7;border-radius:999px;padding:8px 16px}
.tp-tip{margin:0;font-size:12px;color:#94a3b8;line-height:1.8}
/* 注册信息查看与修改 */
.tp-info{margin-top:6px;border-top:1px dashed #dbeafe;padding-top:16px;text-align:left}
.tp-info-tip{font-size:13px;color:#94a3b8;text-align:center;padding:8px 0}
.tp-form{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.tp-field{display:flex;flex-direction:column;gap:5px}
.tp-field.full{grid-column:1 / -1}
.tp-field label{font-size:12px;color:#475569}
.tp-field label.required::after{content:" *";color:#d92d20}
.tp-field input,.tp-field select{border:1px solid #cbd5e1;border-radius:8px;padding:9px 12px;font-size:13.5px;font-family:inherit;color:#0f172a;min-height:36px;line-height:1.4;box-sizing:border-box}
.tp-field input:disabled{background:#f1f5f9;color:#667085;cursor:not-allowed}
.tp-field input:focus,.tp-field select:focus{outline:2px solid #bfdbfe;border-color:#2563eb}
.tp-info-actions{margin-top:14px;display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.tp-info-actions small{font-size:11.5px;color:#94a3b8}
@media(max-width:560px){.tp-form{grid-template-columns:1fr}.tp-card{padding:26px 20px}}
</style>
