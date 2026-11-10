<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { readTrialSession, saveTrialSession } from '../trial'

const route = useRoute()
const router = useRouter()
/* mode=trial：体验注册（提交平台审批，通过后以手机号作为体验厂管理员，30 天）；默认：制衣厂注册（平台审批后启用） */
const isTrial = computed(() => route.query.mode === 'trial')

const form = reactive({
  name: '', code: '', region: '', scale: '',
  contact_name: '', contact_phone: '', contact_wechat: '', contact_qq: '', contact_alipay: '',
  sms_code: '', address: '', remark: '',
})
const loading = ref(false)
const error = ref('')
const done = ref(false)
const result = ref<{ code: string; name: string; admin_login_name?: string; initial_password?: string; trial_days?: number; status?: string } | null>(null)

/* 支持从登录页/首页带手机号跳转预填 */
const prefillPhone = typeof route.query.phone === 'string' ? route.query.phone : ''
if (/^1[3-9]\d{9}$/.test(prefillPhone)) form.contact_phone = prefillPhone

const scaleOptions = [
  { value: '', label: '请选择（可选）' },
  { value: 'micro', label: '微型厂' },
  { value: 'small', label: '小型厂' },
  { value: 'medium', label: '中型厂' },
  { value: 'large', label: '大型厂' },
]

/* ===== 短信验证码（当前无短信服务商，走演示模式：验证码直接显示在页面上） ===== */
const SMS_TTL_MS = 3 * 60 * 1000
const SMS_RESEND_MS = 60 * 1000
const codeLoading = ref(false)
const codeMsg = ref('')
const codeOk = ref(false)
const demoCode = ref('')
const expiresAt = ref(0)
const resendAt = ref(0)
const nowTs = ref(Date.now())
let ticker: number | undefined
const codeSeconds = computed(() => Math.max(0, Math.ceil((expiresAt.value - nowTs.value) / 1000)))
const resendSeconds = computed(() => Math.max(0, Math.ceil((resendAt.value - nowTs.value) / 1000)))
const isValidPhone = () => /^1[3-9]\d{9}$/.test(form.contact_phone.trim())

function startTicker() {
  if (ticker) return
  ticker = window.setInterval(() => { nowTs.value = Date.now() }, 1000)
}

/* 切换注册模式（体验 ⇄ 正式）：重置表单结果与验证码状态 */
function switchMode() {
  done.value = false
  error.value = ''
  result.value = null
  codeMsg.value = ''
  codeOk.value = false
  demoCode.value = ''
  expiresAt.value = 0
}

async function sendCode() {
  codeMsg.value = ''
  codeOk.value = false
  if (!isValidPhone()) { codeMsg.value = '请先填写正确的 11 位手机号'; return }
  if (resendSeconds.value > 0) { codeMsg.value = `请 ${resendSeconds.value} 秒后再获取验证码`; return }
  codeLoading.value = true
  try {
    const r = await api<{ expires_in: number; sms_sent: boolean; demo_code?: string; message?: string }>(
      '/public/tenant-register/send-code',
      { method: 'POST', body: JSON.stringify({ phone: form.contact_phone.trim() }) },
      { silent: true },
    )
    expiresAt.value = Date.now() + (r.expires_in || 180) * 1000
    resendAt.value = Date.now() + SMS_RESEND_MS
    demoCode.value = r.sms_sent ? '' : (r.demo_code || '')
    nowTs.value = Date.now()
    startTicker()
    codeOk.value = true
    codeMsg.value = r.sms_sent ? '验证码已发送，请查收短信，3 分钟内有效' : '演示模式（暂未接入短信）：请直接使用下方验证码完成注册'
  } catch (e) {
    codeMsg.value = (e as Error).message || '获取验证码失败，请稍后重试'
  } finally {
    codeLoading.value = false
  }
}

async function submit() {
  if (!form.name.trim() || !form.contact_name.trim() || !form.contact_phone.trim()) {
    error.value = '请填写制衣厂名称、联系人和联系电话'
    return
  }
  if (!isValidPhone()) { error.value = '请输入正确的 11 位联系电话'; return }
  if (!/^\d{6}$/.test(form.sms_code.trim())) { error.value = '请填写 6 位短信验证码（点击“获取验证码”后可见）'; return }
  if (codeSeconds.value <= 0) { error.value = '验证码已过期，请重新获取'; return }
  loading.value = true
  error.value = ''
  try {
    if (isTrial.value) {
      const r = await api<{ code: string; name: string; admin_login_name?: string; initial_password?: string; trial_days: number; status?: string; registration_status?: string; entry_token?: string; message?: string }>('/public/trial-register', {
        method: 'POST',
        body: JSON.stringify({
          name: form.name, region: form.region, scale: form.scale,
          contact_name: form.contact_name, contact_phone: form.contact_phone,
          contact_wechat: form.contact_wechat, contact_qq: form.contact_qq, contact_alipay: form.contact_alipay,
          sms_code: form.sms_code.trim(), address: form.address, remark: form.remark,
        }),
      })
      const status = r.registration_status || r.status || 'pending'
      result.value = { code: r.code, name: r.name, admin_login_name: r.admin_login_name, initial_password: r.initial_password, trial_days: r.trial_days, status }
      /* 保存本机体验会话：回到体验主页即可点击右侧“开始体验” */
      const previous = readTrialSession()
      saveTrialSession({
        phone: form.contact_phone,
        ticket: 'trial-' + r.code,
        expiresAt: Math.floor((Date.now() + (r.trial_days || 30) * 86400000) / 1000),
        via: 'sms',
        displayName: r.name,
        tenantCode: r.code,
        trialName: r.name,
        adminLogin: r.admin_login_name || form.contact_phone,
        adminPassword: r.initial_password || (previous?.tenantCode === r.code ? previous.adminPassword : undefined),
        entryToken: r.entry_token || (previous?.tenantCode === r.code ? previous.entryToken : undefined),
        status,
      })
      /* 体验注册提交后进入「审批中」页：可查看状态并发起加急申请 */
      if (status === 'pending') {
        void router.push('/trial/pending')
        return
      }
    } else {
      const r = await api<{ code: string; name: string; message: string }>('/public/tenant-register', {
        method: 'POST',
        body: JSON.stringify({ ...form, sms_code: form.sms_code.trim() }),
      })
      result.value = { code: r.code, name: r.name }
    }
    done.value = true
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

onBeforeUnmount(() => { if (ticker) { window.clearInterval(ticker); ticker = undefined } })
</script>

<template>
  <div class="register-page">
    <div class="register-card panel">
      <div class="register-brand"><span class="brand-mark">智</span><strong>一智万商 · 制衣 MES</strong></div>
      <template v-if="!done">
        <h1>{{ isTrial ? '免费体验注册' : '制衣厂注册' }}</h1>
        <p class="register-tip" v-if="isTrial">申请体验制衣厂（免费 30 天）：与正式制衣厂同一套功能与页面。提交后由平台管理员审核，通过后以该手机号作为体验厂管理员账户，并开始计算 30 天体验期。</p>
        <p class="register-tip" v-else>提交注册申请后，平台管理员将确认订阅版本与订阅规模，并为贵厂分配管理员账户与初始密码；确认启用后即可登录使用。</p>
        <div v-if="error" class="notice error">{{ error }}</div>
        <div class="form-grid">
          <div class="field full"><label class="required">制衣厂名称</label><input v-model.trim="form.name" maxlength="60" placeholder="与营业执照一致的厂名，全平台唯一"></div>
          <div v-if="!isTrial" class="field"><label>厂编码</label><input v-model.trim="form.code" placeholder="留空由系统自动生成"></div>
          <div class="field"><label>所在国 / 地区</label><input v-model.trim="form.region" placeholder="如 中国广东 / 越南"></div>
          <div class="field"><label>厂规模</label><select v-model="form.scale"><option v-for="o in scaleOptions" :key="o.value" :value="o.value">{{ o.label }}</option></select></div>
          <div class="field"><label class="required">联系人</label><input v-model.trim="form.contact_name" maxlength="30"></div>
          <div class="field"><label class="required">联系电话</label><input v-model.trim="form.contact_phone" maxlength="11" type="tel" placeholder="11 位手机号，用于接收验证码"></div>
          <div class="field"><label>微信号</label><input v-model.trim="form.contact_wechat" maxlength="64" placeholder="选填，便于审批联系"></div>
          <div class="field"><label>QQ</label><input v-model.trim="form.contact_qq" maxlength="32" placeholder="选填"></div>
          <div class="field"><label>支付宝账号</label><input v-model.trim="form.contact_alipay" maxlength="64" placeholder="选填"></div>
          <div class="field full">
            <label class="required">短信验证码</label>
            <div class="code-row">
              <input v-model.trim="form.sms_code" maxlength="6" inputmode="numeric" placeholder="6 位数字验证码">
              <button type="button" class="button secondary code-button" :disabled="codeLoading || resendSeconds > 0" @click="sendCode">
                {{ codeLoading ? '发送中…' : resendSeconds > 0 ? `${resendSeconds}s 后重发` : '获取验证码' }}
              </button>
            </div>
            <small v-if="codeOk && codeSeconds > 0" class="code-hint">
              <template v-if="demoCode">演示模式验证码：<strong class="code-value">{{ demoCode }}</strong>（{{ Math.floor(codeSeconds / 60) }}:{{ String(codeSeconds % 60).padStart(2, '0') }} 内有效）</template>
              <template v-else>{{ codeMsg }}（{{ Math.floor(codeSeconds / 60) }}:{{ String(codeSeconds % 60).padStart(2, '0') }} 内有效）</template>
            </small>
            <small v-else-if="codeMsg" class="code-hint code-warn">{{ codeMsg }}</small>
            <small v-else class="code-hint">暂未接入短信服务商，点击“获取验证码”后验证码直接显示在此处（模拟短信验证）</small>
          </div>
          <div class="field full"><label>厂地址</label><input v-model.trim="form.address" maxlength="120"></div>
          <div class="field full"><label>备注</label><input v-model.trim="form.remark" maxlength="200" placeholder="主营品类、车间与产线数量等（可选）"></div>
        </div>
        <button class="button register-submit" :disabled="loading" @click="submit">{{ loading ? '正在提交…' : isTrial ? '提交体验注册申请' : '提交注册申请' }}</button>
        <p class="register-foot">
          <template v-if="isTrial">需要正式使用？<RouterLink :to="{ path: '/register', query: {} }" @click="switchMode">改为制衣厂注册（平台审批后启用）</RouterLink>　·　</template>
          <template v-else>想先免费体验？<RouterLink to="/register?mode=trial" @click="switchMode">改为免费体验注册（30 天）</RouterLink>　·　</template>
          <RouterLink to="/login">直接登录</RouterLink>　·　<RouterLink to="/portal">返回首页</RouterLink>
        </p>
      </template>
      <template v-else>
        <template v-if="isTrial">
          <h1>{{ result?.status === 'approved' ? '体验厂已通过审核' : '体验注册申请已提交' }}</h1>
          <div class="notice success">
            <template v-if="result?.status === 'approved'">
              「{{ result?.name }}」（编码 {{ result?.code }}）的体验制衣厂已通过审核，免费 {{ result?.trial_days ?? 30 }} 天。<br>
              请使用注册手机号作为管理员账户，登录后点击体验页右侧“开始体验”进入工作台。
            </template>
            <template v-else>
              「{{ result?.name }}」（编码 {{ result?.code }}）的体验注册申请已提交成功。<br>
              平台管理员审核通过后，将以注册手机号 <strong>{{ form.contact_phone }}</strong> 作为该体验厂管理员账户，并开始 30 天体验期。请留意平台通知。
            </template>
          </div>
          <div v-if="result?.status === 'approved' && result?.initial_password" class="credential-box">
            <p><strong>管理员账户：</strong><span class="code-value">{{ result?.admin_login_name }}</span></p>
            <p><strong>员工首次登录密码：</strong><span class="code-value">{{ result?.initial_password }}</span></p>
            <p class="cred-tip">该密码供本厂其他员工首次短信/微信登录使用；管理员本人可直接用手机号或微信登录。</p>
          </div>
        </template>
        <template v-else>
          <h1>注册申请已提交</h1>
          <div class="notice success">
            「{{ result?.name }}」（编码 {{ result?.code }}）的注册申请已提交成功。<br>
            平台管理员确认订阅版本与订阅规模并分配管理员账户后，贵厂即可登录使用。请留意平台通知。
          </div>
        </template>
        <div class="register-foot">
          <template v-if="isTrial"><RouterLink class="button" to="/trial/home">进入体验主页</RouterLink></template>
          <RouterLink class="button" :class="{ secondary: isTrial }" to="/login">前往登录页</RouterLink>
          <RouterLink class="button secondary" to="/portal">返回首页</RouterLink>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.register-page { min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 24px; }
.register-card { width: min(680px, 100%); padding: 28px 32px; }
.register-brand { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; }
.register-card h1 { font-size: 22px; margin: 0 0 8px; }
.register-tip { color: var(--text-muted, #64748b); font-size: 13px; margin: 0 0 18px; }
.register-submit { width: 100%; margin-top: 18px; }
.register-foot { margin-top: 16px; font-size: 13px; display: flex; gap: 12px; align-items: center; }
.register-foot a { color: var(--primary, #2563eb); }
.code-row { display: flex; gap: 10px; align-items: center; }
.code-row input { flex: 1; }
.code-button { white-space: nowrap; min-width: 120px; }
.code-hint { display: block; margin-top: 6px; color: var(--text-muted, #64748b); font-size: 12px; }
.code-hint.code-warn { color: #b45309; }
.code-value { color: #1d4ed8; font-size: 15px; letter-spacing: 2px; }
.credential-box { margin-top: 14px; padding: 14px 16px; border: 1px dashed #93c5fd; border-radius: 8px; background: #eff6ff; }
.credential-box p { margin: 4px 0; }
.cred-tip { color: var(--text-muted, #64748b); font-size: 12px; }
</style>
