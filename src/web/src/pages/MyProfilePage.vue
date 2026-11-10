<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const profile = ref<any>(null)
const form = reactive({ phone: '', id_card_no: '', bank_card: '', bank_name: '', emergency_contact: '', emergency_phone: '', address: '', nation: '汉', native_place: '' })
const nationOptions = ['汉', '回', '壮', '苗', '其他'].map(v => ({ value: v, label: v }))

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
onMounted(load)
</script>
<template>
  <div class="page-header"><div><h1>我的资料</h1><p>敏感信息仅本人可查看与修改，其他人在其视图中看到的是脱敏内容</p></div><button class="button" :disabled="saving" @click="save">保存修改</button></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <PageState :loading="loading"/>
  <template v-if="!loading && profile">
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
