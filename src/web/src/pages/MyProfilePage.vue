<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'
import { dictOptions, type DictOption } from '../dictionaries'

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const profile = ref<any>(null)
const form = reactive({ phone: '', id_card_no: '', bank_card: '', bank_name: '', emergency_contact: '', emergency_phone: '', address: '', nation: '汉', native_place: '' })
/* 民族选项从数据库字典获取（带缓存），当前值缺失时注入兜底 */
const nationOptions = ref<DictOption[]>([])
async function loadNationOptions() {
  nationOptions.value = await dictOptions('nation', 'name')
  if (form.nation && !nationOptions.value.some(o => o.value === form.nation)) nationOptions.value.push({ value: form.nation, label: form.nation })
}

async function load() {
  loading.value = true; error.value = ''
  try {
    profile.value = await api<any>('/my/profile')
    Object.assign(form, { phone: profile.value.phone || '', id_card_no: profile.value.id_card_no || '', bank_card: profile.value.bank_card || '', bank_name: profile.value.bank_name || '', emergency_contact: profile.value.emergency_contact || '', emergency_phone: profile.value.emergency_phone || '', address: profile.value.address || '', nation: profile.value.nation || '汉', native_place: profile.value.native_place || '' })
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
async function save() {
  saving.value = true; error.value = ''
  try { await api('/my/profile', { method: 'PATCH', body: JSON.stringify(form) }); notice.value = '资料已保存'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
const readOnlyInfo = computed(() => {
  const p = profile.value
  if (!p) return []
  return [
    ['工号', p.employee_no], ['姓名', p.name], ['性别', p.gender === 'male' ? '男' : p.gender === 'female' ? '女' : '—'],
    ['出生日期', p.birth_date], ['入职日期', p.hire_date], ['岗位', p.position], ['责任车间', p.workshop],
    ['生产线', p.production_line], ['技能评级', p.skill_grade], ['工资模式', p.pay_mode === 'monthly' ? '月薪' : '计件'],
  ].filter(([, v]) => v)
})
/* 登录验证方式：短信验证码恒开，密码验证与微信设备快捷登录由本人自行开关 */
const authMethods = ref({ sms: true, password: false, wechat: false })
const authSaving = ref(false)
async function loadAuthMethods() {
  try {
    const r = await api<{ methods: typeof authMethods.value }>('/me/auth-methods', {}, { silent: true })
    authMethods.value = { ...authMethods.value, ...r.methods, sms: true }
  } catch { /* 无账号时隐藏失败不影响资料页 */ }
}
async function toggleAuthMethod(key: 'password' | 'wechat', ev: Event) {
  const value = (ev.target as HTMLInputElement).checked
  authSaving.value = true; error.value = ''
  try {
    const r = await api<{ methods: typeof authMethods.value }>('/me/auth-methods', { method: 'PUT', body: JSON.stringify({ [key]: value }) })
    authMethods.value = { ...authMethods.value, ...r.methods, sms: true }
    notice.value = value ? '已开通该验证方式' : '已关闭该验证方式'
  } catch (e) { error.value = (e as Error).message; await loadAuthMethods() } finally { authSaving.value = false }
}
onMounted(async () => { await load(); void loadNationOptions(); void loadAuthMethods() })
</script>
<template>
  <div class="page-header"><div><h1>我的资料</h1><p>敏感信息仅本人可查看与修改，其他人在其视图中看到的是脱敏内容</p></div><button class="button" :disabled="saving" @click="save">保存修改</button></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <PageState :loading="loading"/>
  <template v-if="!loading && profile">
    <section class="panel" style="margin-bottom:20px"><div class="panel-header"><h2>登录验证</h2></div><div class="panel-body">
      <p class="text-muted" style="margin-top:0">短信验证码是基础验证，始终开启且不可关闭；以下验证方式由本人自行开通或关闭，也可由厂管理员统一设置。</p>
      <div class="progress-list">
        <div style="display:flex;align-items:center;justify-content:space-between;gap:20px"><div><strong>短信验证码</strong><p class="text-muted">登录时必选，固定开启</p></div><input type="checkbox" checked disabled></div>
        <div style="display:flex;align-items:center;justify-content:space-between;gap:20px"><div><strong>密码验证</strong><p class="text-muted">开通后，短信验证之外还需输入账号密码</p></div><input type="checkbox" :checked="authMethods.password" :disabled="authSaving" @change="toggleAuthMethod('password', $event)"></div>
        <div style="display:flex;align-items:center;justify-content:space-between;gap:20px"><div><strong>微信设备快捷登录</strong><p class="text-muted">开通并绑定设备后可扫码快捷登录；关闭时自动解绑</p></div><input type="checkbox" :checked="authMethods.wechat" :disabled="authSaving" @change="toggleAuthMethod('wechat', $event)"></div>
      </div>
    </div></section>
    <section class="panel" style="margin-bottom:20px"><div class="panel-header"><h2>基本信息（不可修改）</h2></div><div class="panel-body"><div class="form-grid"><div v-for="[k, v] in readOnlyInfo" :key="k" class="field"><label>{{ k }}</label><input :value="v" disabled></div></div></div></section>
    <section class="panel"><div class="panel-header"><h2>联系与敏感信息（本人可修改）</h2></div><div class="panel-body"><div class="form-grid">
      <div class="field"><label>联系电话</label><input v-model.trim="form.phone" placeholder="11 位手机号"></div>
      <div class="field"><label>身份证号</label><input v-model.trim="form.id_card_no" placeholder="18 位"></div>
      <div class="field"><label>银行卡号</label><input v-model.trim="form.bank_card" placeholder="发薪使用"></div>
      <div class="field"><label>开户行</label><input v-model.trim="form.bank_name" placeholder="如 工行"></div>
      <div class="field"><label>紧急联系人</label><input v-model.trim="form.emergency_contact" placeholder="姓名及关系"></div>
      <div class="field"><label>紧急联系电话</label><input v-model.trim="form.emergency_phone"></div>
      <div class="field"><label>民族</label><FilterSelect v-model="form.nation" :options="nationOptions"/></div>
      <div class="field"><label>籍贯</label><input v-model.trim="form.native_place" placeholder="如 广西贵港"></div>
      <div class="field full"><label>现住址</label><textarea v-model="form.address" rows="2"></textarea></div>
    </div></div></section>
  </template>
</template>
