<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { QR_APP_COLORS, QR_APP_LOGIN, QR_APP_NAMES, type QrApp } from '../qrLogin'
import { readTrialSession, saveTrialSession } from '../trial'
import {
  TRIAL_DEMO_PASSWORD, TRIAL_DEMO_USERNAME,
  trialAuthLogin, trialAuthRegister, validateTrialPassword, validateTrialUsername,
} from '../trialAuth'

/** 三个模块入口共用的用户名密码登录弹层：登录成功后进入用户体验页面 */
const props = defineProps<{ app: QrApp }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const router = useRouter()

const appName = computed(() => QR_APP_NAMES[props.app] || '应用')
const color = computed(() => QR_APP_COLORS[props.app] || '#2563eb')
const gradient = computed(() => `linear-gradient(90deg, ${color.value}, #06b6d4)`)

const mode = ref<'login' | 'register'>('login')
const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const showPassword = ref(false)
const loading = ref(false)
const error = ref('')
const nameInput = ref<HTMLInputElement | null>(null)
const existing = readTrialSession()

function switchMode(next: 'login' | 'register') {
  mode.value = next
  error.value = ''
  confirmPassword.value = ''
}

function fillDemo() {
  mode.value = 'login'
  username.value = TRIAL_DEMO_USERNAME
  password.value = TRIAL_DEMO_PASSWORD
  confirmPassword.value = ''
  error.value = ''
}

async function submit() {
  if (loading.value) return
  error.value = ''
  const name = username.value.trim()
  const nameError = validateTrialUsername(name)
  if (nameError) { error.value = nameError; return }
  const pwdError = validateTrialPassword(password.value)
  if (pwdError) { error.value = pwdError; return }
  if (mode.value === 'register' && password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  loading.value = true
  try {
    const result = mode.value === 'register'
      ? await trialAuthRegister(name, password.value)
      : await trialAuthLogin(name, password.value)
    saveTrialSession({
      phone: result.username,
      ticket: result.ticket,
      expiresAt: Date.now() + result.expires_in * 1000,
      displayName: result.username,
      via: 'account',
    })
    emit('close')
    await router.push(`/trial/home?app=${props.app}&via=account`)
  } catch (e) {
    error.value = (e as Error).message || '操作失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function enterExisting() {
  emit('close')
  await router.push(`/trial/home?app=${props.app}`)
}

function onKeydown(e: KeyboardEvent) { if (e.key === 'Escape') emit('close') }
onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  nameInput.value?.focus()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div class="ta-overlay" @click.self="emit('close')">
      <div class="ta-dialog" role="dialog" aria-modal="true" :aria-label="`${appName} 账号登录`">
        <button class="ta-close" type="button" aria-label="关闭" @click="emit('close')">✕</button>

        <div class="ta-head">
          <span class="ta-mark" :style="{ background: gradient }">账</span>
          <div>
            <h2>登录 {{ appName }}</h2>
            <p>用户名密码验证后，立即进入用户体验页面</p>
          </div>
        </div>

        <div class="ta-tabs" role="tablist">
          <button type="button" :class="{ active: mode === 'login' }" role="tab" @click="switchMode('login')">账号登录</button>
          <button type="button" :class="{ active: mode === 'register' }" role="tab" @click="switchMode('register')">注册体验账号</button>
        </div>

        <form @submit.prevent="submit">
          <label for="ta-username">用户名</label>
          <input id="ta-username" ref="nameInput" v-model.trim="username" type="text" autocomplete="username"
            maxlength="20" placeholder="2-20 位字母、数字、下划线或中文" />

          <label for="ta-password">密码</label>
          <div class="ta-pwd">
            <input id="ta-password" v-model="password" :type="showPassword ? 'text' : 'password'"
              :autocomplete="mode === 'register' ? 'new-password' : 'current-password'" placeholder="至少 6 位" />
            <button type="button" @click="showPassword = !showPassword">{{ showPassword ? '隐藏' : '显示' }}</button>
          </div>

          <template v-if="mode === 'register'">
            <label for="ta-confirm">确认密码</label>
            <input id="ta-confirm" v-model="confirmPassword" :type="showPassword ? 'text' : 'password'"
              autocomplete="new-password" placeholder="再次输入密码" />
          </template>

          <p v-if="error" class="ta-error">{{ error }}</p>
          <button class="ta-submit" type="submit" :disabled="loading" :style="{ background: gradient }">
            {{ loading ? '请稍候…' : mode === 'register' ? '注册并进入体验页' : '登录并进入体验页' }}
          </button>
        </form>

        <div class="ta-demo">
          <span>演示账号 <b>{{ TRIAL_DEMO_USERNAME }}</b> / <b>{{ TRIAL_DEMO_PASSWORD }}</b></span>
          <button type="button" @click="fillDemo">一键填入</button>
        </div>

        <div v-if="existing" class="ta-existing">
          <span>当前已有体验会话：{{ existing.displayName || existing.phone }}</span>
          <button type="button" @click="enterExisting">直接进入 →</button>
        </div>

        <div class="ta-foot">
          <RouterLink :to="QR_APP_LOGIN[app]" @click="emit('close')">更多登录方式 →</RouterLink>
          <span>体验账号仅用于演示，请勿使用真实密码</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ta-overlay{position:fixed;inset:0;z-index:220;background:rgba(10,20,50,.5);backdrop-filter:blur(4px);
  display:grid;place-items:center;padding:20px}
.ta-dialog{position:relative;width:min(420px,100%);max-height:92vh;overflow-y:auto;background:#fff;border-radius:18px;
  padding:26px 28px 22px;box-shadow:0 24px 60px rgba(10,20,50,.35);
  font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;color:#17255f}
.ta-close{position:absolute;top:10px;right:10px;width:30px;height:30px;border:0;background:#f2f5fb;
  color:#667085;border-radius:50%;cursor:pointer;font-size:14px}
.ta-close:hover{background:#e7edf7}
.ta-head{display:flex;align-items:center;gap:12px;margin-bottom:20px}
.ta-mark{display:grid;place-items:center;width:44px;height:44px;border-radius:12px;color:#fff;
  font-weight:700;font-size:18px;flex-shrink:0;box-shadow:0 6px 16px rgba(37,99,235,.24)}
.ta-head h2{margin:0 0 4px;font-size:19px}
.ta-head p{margin:0;font-size:12.5px;color:#8b9ac0}
.ta-tabs{display:flex;gap:6px;background:#f2f6fd;border-radius:11px;padding:4px;margin-bottom:20px}
.ta-tabs button{flex:1;border:0;background:transparent;color:#667085;border-radius:8px;padding:9px 6px;
  font-size:13.5px;font-weight:600;cursor:pointer;font-family:inherit;transition:.15s}
.ta-tabs button.active{background:#fff;color:#1d4ed8;box-shadow:0 2px 8px rgba(23,37,95,.1)}
.ta-dialog form label{display:block;font-size:13px;font-weight:600;color:#41537e;margin:0 0 7px}
.ta-dialog form label:not(:first-child){margin-top:15px}
.ta-dialog form input{width:100%;box-sizing:border-box;border:1px solid #d5e1f5;border-radius:10px;
  padding:11px 13px;font-size:14.5px;background:#fff;color:#17255f;outline:none;transition:.15s;font-family:inherit}
.ta-dialog form input:focus{border-color:#2563eb;box-shadow:0 0 0 3px rgba(37,99,235,.12)}
.ta-pwd{display:flex;gap:8px}
.ta-pwd input{flex:1;min-width:0}
.ta-pwd button{border:1px solid #d5e1f5;background:#f4f8ff;color:#2563eb;border-radius:10px;
  padding:0 13px;font-size:13px;font-weight:600;white-space:nowrap;cursor:pointer;font-family:inherit}
.ta-error{margin:12px 0 0;font-size:13px;color:#d92d20}
.ta-submit{width:100%;margin-top:18px;border:0;border-radius:10px;padding:13px;font-size:15px;font-weight:700;
  color:#fff;cursor:pointer;font-family:inherit;box-shadow:0 8px 20px rgba(37,99,235,.22)}
.ta-submit:hover{filter:brightness(1.06)}
.ta-submit:disabled{opacity:.6;cursor:not-allowed}
.ta-demo{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-top:14px;
  padding:10px 12px;border:1px dashed #93b6f5;background:#f5f9ff;border-radius:10px;font-size:12.5px;color:#41537e}
.ta-demo b{color:#1d4ed8}
.ta-demo button,.ta-existing button{border:0;background:transparent;color:#2563eb;font-size:12.5px;
  font-weight:700;cursor:pointer;white-space:nowrap;font-family:inherit}
.ta-demo button:hover,.ta-existing button:hover{text-decoration:underline}
.ta-existing{display:flex;align-items:center;justify-content:space-between;gap:10px;
  margin-top:10px;font-size:12.5px;color:#667085}
.ta-foot{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;
  margin-top:18px;padding-top:14px;border-top:1px solid #eef2fa;font-size:11.5px;color:#98a2b3}
.ta-foot a{color:#2563eb;font-weight:600}
.ta-foot a:hover{text-decoration:underline}
@media(max-width:600px){
  .ta-dialog{padding:22px 18px 18px}
  .ta-foot{justify-content:flex-start}
}
</style>
