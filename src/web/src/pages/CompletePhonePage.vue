<script setup lang="ts">
/**
 * 完善注册手机号：旧数据租户（体验/付费）注册信息里没有联系电话，一直用账号密码登录。
 * 厂管理员登录后若缺手机号，先在本页补录，再进入门户/工作台。
 * 只登记号码，不发送也不校验短信验证码（注册流程仍然需要短信验证码，此处不属于注册）。
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import NetworkBackground from '../components/NetworkBackground.vue'
import { api } from '../api'
import { clearNeedPhone, pendingPhoneTenant } from '../phoneGate'
import { toastError, toastSuccess } from '../toast'

const router = useRouter()
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const profile = ref<{ code?: string; name?: string; contact_name?: string; contact_phone?: string } | null>(null)
const form = reactive({ phone: '' })

const tenantCode = computed(() => profile.value?.code || pendingPhoneTenant() || '')
const tenantName = computed(() => profile.value?.name || '本厂')
const phoneValid = computed(() => /^1[3-9]\d{9}$/.test(form.phone.trim()))

async function load() {
  loading.value = true
  try {
    const r = await api<{ code?: string; name?: string; contact_name?: string; contact_phone?: string }>(
      '/tenant/profile', {}, { silent: true })
    profile.value = r || null
    /* 已经登记过手机号（残留标记）：直接回门户，不再拦人 */
    if (r?.contact_phone) {
      clearNeedPhone()
      const code = r.code || pendingPhoneTenant()
      await router.replace(code ? `/portal/home?tenant=${encodeURIComponent(code)}` : '/portal/home')
      return
    }
  } catch {
    /* 资料读取失败不阻断补录：厂编码回退到登录时记下的值 */
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (saving.value) return
  error.value = ''
  const phone = form.phone.trim()
  if (!/^1[3-9]\d{9}$/.test(phone)) { error.value = '请填写 11 位手机号'; return }
  saving.value = true
  try {
    await api('/auth/complete-phone', { method: 'POST', body: JSON.stringify({ phone }) })
    clearNeedPhone()
    toastSuccess('联系电话已完善')
    const code = tenantCode.value
    await router.replace(code ? `/portal/home?tenant=${encodeURIComponent(code)}` : '/portal/home')
  } catch (e) {
    error.value = (e as Error).message || '保存失败，请稍后重试'
    toastError(error.value)
  } finally {
    saving.value = false
  }
}

function logout() {
  localStorage.removeItem('clothp_access_token')
  localStorage.removeItem('clothp_refresh_token')
  clearNeedPhone()
  void router.replace('/login')
}

onMounted(load)
</script>

<template>
  <div class="cp-page">
    <NetworkBackground theme="aurora" />
    <div class="cp-card">
      <div class="cp-brand"><span class="cp-mark">智</span><strong>一智算 · 完善资料</strong></div>
      <h1>完善注册手机号</h1>
      <p class="cp-desc">
        <strong>{{ tenantName }}</strong><span v-if="tenantCode" class="cp-code">{{ tenantCode }}</span>
        的注册信息里还没有登记联系电话。为便于接收平台通知（审核、订阅到期、异常提醒），请补填厂管理员手机号。
      </p>
      <div class="cp-note">仅登记号码，不会发送或校验短信验证码。</div>

      <div v-if="error" class="cp-error">{{ error }}</div>
      <div v-if="loading" class="cp-loading">正在读取本厂资料…</div>
      <form class="cp-form" @submit.prevent="submit">
        <div class="cp-field">
          <label class="required" for="cp-phone">手机号</label>
          <input id="cp-phone" v-model.trim="form.phone" type="tel" maxlength="11" inputmode="numeric"
            autocomplete="tel" placeholder="11 位手机号" :disabled="saving">
        </div>
        <div class="cp-actions">
          <button type="submit" class="cp-btn solid" :disabled="saving || !phoneValid">
            {{ saving ? '正在保存…' : '保存并进入门户' }}
          </button>
          <button type="button" class="cp-btn ghost" :disabled="saving" @click="logout">退出登录</button>
        </div>
      </form>

      <p class="cp-tip">保存后可随时在「本厂信息」中修改联系人资料。</p>
    </div>
  </div>
</template>

<style scoped>
.cp-page{min-height:100vh;display:grid;place-items:center;padding:32px 20px;position:relative;
  font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;color:#17255f;
  background:linear-gradient(160deg,#eaf3ff 0%,#f5f9ff 45%,#eef8fc 100%)}
.cp-card{position:relative;z-index:1;width:min(520px,100%);background:#fff;border:1px solid #e3ecfb;border-radius:20px;
  padding:34px 38px 30px;box-shadow:0 18px 48px rgba(23,37,95,.10)}
.cp-brand{display:flex;align-items:center;gap:8px;font-size:14px;color:#41537e;margin-bottom:18px}
.cp-mark{display:grid;place-items:center;width:28px;height:28px;border-radius:9px;color:#fff;font-size:12px;
  background:linear-gradient(135deg,#2563eb,#06b6d4)}
h1{margin:0 0 10px;font-size:22px}
.cp-desc{margin:0 0 10px;font-size:13.5px;line-height:1.9;color:#475569}
.cp-code{display:inline-block;margin-left:6px;font-size:11.5px;font-weight:700;color:#1d4ed8;background:#dbeafe;border-radius:999px;padding:2px 9px}
.cp-note{font-size:12.5px;color:#0e7490;background:#ecfeff;border:1px solid #a5f3fc;border-radius:10px;padding:9px 12px;margin-bottom:16px}
.cp-error{font-size:13px;color:#b42318;background:#fef3f2;border:1px solid #fecdca;border-radius:10px;padding:9px 12px;margin-bottom:14px}
.cp-loading{font-size:13px;color:#94a3b8;padding:6px 0 12px}
.cp-form{display:flex;flex-direction:column;gap:16px}
.cp-field{display:flex;flex-direction:column;gap:6px}
.cp-field label{font-size:12.5px;color:#475569}
.cp-field label.required::after{content:" *";color:#d92d20}
.cp-field input{border:1px solid #cbd5e1;border-radius:10px;padding:11px 14px;font-size:15px;font-family:inherit;color:#0f172a}
.cp-field input:focus{outline:2px solid #bfdbfe;border-color:#2563eb}
.cp-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.cp-btn{border-radius:10px;padding:11px 22px;font-size:14px;font-weight:600;cursor:pointer;border:1px solid transparent;font-family:inherit}
.cp-btn.solid{background:linear-gradient(90deg,#2563eb,#06b6d4);color:#fff;box-shadow:0 8px 20px rgba(37,99,235,.25)}
.cp-btn.ghost{background:#fff;border-color:#cbd5e1;color:#475569}
.cp-btn:disabled{opacity:.6;cursor:not-allowed}
.cp-tip{margin:18px 0 0;font-size:12px;color:#94a3b8;line-height:1.8}
@media(max-width:560px){.cp-card{padding:26px 20px}}
</style>
