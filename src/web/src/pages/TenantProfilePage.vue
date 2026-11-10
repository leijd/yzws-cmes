<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, formatTime } from '../api'
import { usePermissions } from '../permissions'
import PageState from '../components/PageState.vue'

const { has } = usePermissions()
const canWrite = computed(() => has('identity:write'))

const loading = ref(true)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const editing = ref(false)
const profile = ref<any>(null)
const form = reactive({
  contact_name: '', contact_phone: '', contact_wechat: '', contact_qq: '', contact_alipay: '',
  address: '', region: '', remark: '',
})

const editionText: Record<string, string> = { basic: '基础版', standard: '标准版', pro: '专业版' }
const scaleText: Record<string, string> = { micro: '微型厂', small: '小型厂', medium: '中型厂', large: '大型厂' }
const customerTypeText: Record<string, string> = { trial: '体验客户', paid: '付费客户' }
const regStatusText: Record<string, string> = { pending: '待平台确认', approved: '已启用', rejected: '已驳回' }
const subStatusText: Record<string, string> = { trial: '体验中', active: '订阅生效', expired: '已到期', suspended: '已停用' }

const quotaRatio = computed(() => {
  const q = profile.value?.user_quota
  if (!q || !q.limit) return 0
  return Math.min(Math.round((q.used / q.limit) * 100), 100)
})

async function load() {
  loading.value = true; error.value = ''
  try {
    profile.value = await api<any>('/tenant/profile')
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function openEdit() {
  Object.assign(form, {
    contact_name: profile.value?.contact_name || '',
    contact_phone: profile.value?.contact_phone || '',
    contact_wechat: profile.value?.contact_wechat || '',
    contact_qq: profile.value?.contact_qq || '',
    contact_alipay: profile.value?.contact_alipay || '',
    address: profile.value?.address || '',
    region: profile.value?.region || '',
    remark: profile.value?.remark || '',
  })
  editing.value = true
}
async function save() {
  saving.value = true; error.value = ''; notice.value = ''
  try {
    await api('/tenant/profile', { method: 'PUT', body: JSON.stringify({ ...form, version: profile.value?.version ?? 0 }) })
    editing.value = false
    notice.value = '厂资料已保存'
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <div class="page-header"><div><h1>本厂信息</h1><p>制衣厂资料、订阅状态与用户配额</p></div>
    <div class="header-actions"><button v-if="canWrite && !editing" class="button" @click="openEdit">修改辅助信息</button></div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>
  <PageState :loading="loading" :error="error" @retry="load" />
  <template v-if="!loading && profile">
    <div v-if="profile.registration_status === 'pending'" class="notice">注册申请审核中：平台管理员确认订阅版本与规模并分配管理员账户后即可正常使用。</div>
    <div class="dashboard-grid">
      <section class="panel">
        <div class="panel-header"><h2>厂资料</h2></div>
        <div class="panel-body">
          <template v-if="!editing">
            <div class="table-wrap"><table><tbody>
              <tr><th style="width:130px">厂编码</th><td><strong>{{ profile.code }}</strong></td></tr>
              <tr><th>厂名称</th><td>{{ profile.name }}</td></tr>
              <tr><th>厂标识（短码）</th><td>{{ profile.short_code }}</td></tr>
              <tr><th>客户类型</th><td>{{ customerTypeText[profile.customer_type] || profile.customer_type }}</td></tr>
              <tr><th>注册状态</th><td><span class="status" :class="profile.registration_status">{{ regStatusText[profile.registration_status] || profile.registration_status }}</span></td></tr>
              <tr><th>注册时间</th><td>{{ formatTime(profile.registered_at) }}</td></tr>
              <tr><th>所在国/地区</th><td>{{ profile.region || '—' }}</td></tr>
              <tr><th>厂规模</th><td>{{ profile.scale ? scaleText[profile.scale] || profile.scale : '—' }}</td></tr>
              <tr><th>联系人</th><td>{{ profile.contact_name || '—' }}</td></tr>
              <tr><th>联系电话</th><td>{{ profile.contact_phone || '—' }}</td></tr>
              <tr><th>微信号</th><td>{{ profile.contact_wechat || '—' }}</td></tr>
              <tr><th>QQ</th><td>{{ profile.contact_qq || '—' }}</td></tr>
              <tr><th>支付宝账号</th><td>{{ profile.contact_alipay || '—' }}</td></tr>
              <tr><th>厂地址</th><td>{{ profile.address || '—' }}</td></tr>
              <tr><th>备注</th><td>{{ profile.remark || '—' }}</td></tr>
            </tbody></table></div>
          </template>
          <template v-else>
            <div class="form-grid">
              <div class="field"><label>联系人</label><input v-model.trim="form.contact_name" maxlength="30"></div>
              <div class="field"><label>联系电话</label><input v-model.trim="form.contact_phone" maxlength="20" type="tel"></div>
              <div class="field"><label>微信号</label><input v-model.trim="form.contact_wechat" maxlength="64"></div>
              <div class="field"><label>QQ</label><input v-model.trim="form.contact_qq" maxlength="32"></div>
              <div class="field"><label>支付宝账号</label><input v-model.trim="form.contact_alipay" maxlength="64"></div>
              <div class="field"><label>所在国/地区</label><input v-model.trim="form.region" maxlength="40"></div>
              <div class="field"><label>厂地址</label><input v-model.trim="form.address" maxlength="120"></div>
              <div class="field full"><label>备注</label><input v-model.trim="form.remark" maxlength="200"></div>
            </div>
            <div class="form-actions"><button class="button secondary" @click="editing = false">取消</button><button class="button" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</button></div>
          </template>
        </div>
      </section>
      <section class="panel">
        <div class="panel-header"><h2>订阅状态</h2></div>
        <div class="panel-body">
          <template v-if="profile.subscription">
            <div class="table-wrap"><table><tbody>
              <tr><th style="width:130px">订阅版本</th><td><strong>{{ editionText[profile.subscription.edition] || profile.subscription.edition }}</strong></td></tr>
              <tr><th>订阅状态</th><td><span class="status" :class="profile.subscription.status === 'active' ? 'active' : 'pending'">{{ subStatusText[profile.subscription.status] || profile.subscription.status }}</span></td></tr>
              <tr><th>到期时间</th><td>{{ formatTime(profile.subscription.expires_at) }}</td></tr>
            </tbody></table></div>
            <h3>用户配额</h3>
            <div class="progress-row" style="margin-top:8px">
              <span>启用账号 {{ profile.user_quota.used }}<template v-if="profile.user_quota.limit"> / {{ profile.user_quota.limit }}</template></span>
              <div class="progress-track"><div class="progress-bar" :style="{ width: quotaRatio + '%' }"></div></div>
              <strong>{{ profile.user_quota.limit ? quotaRatio + '%' : '不限' }}</strong>
            </div>
            <p class="text-muted" style="font-size:12px;margin-top:10px">新开账号或启用账号时受订阅规模限制；达到上限请联系平台管理员调整。</p>
          </template>
          <div v-else class="notice">尚未确认订阅：请联系平台管理员确认订阅版本与订阅规模。</div>
        </div>
      </section>
    </div>
  </template>
</template>
