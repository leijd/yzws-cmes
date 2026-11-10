<script setup lang="ts">
/**
 * 门户设置：本厂管理员维护制衣厂门户页的 logo 与欢迎标题/欢迎语。
 * logo 支持直接上传 PNG/GIF（≤500KB，存库后经公开接口输出），也可沿用图片网址；
 * 留空/移除即使用平台默认内容。
 */
import { onMounted, reactive, ref } from 'vue'
import { api } from '../api'

/* logo 标准：正方形 512×512，PNG/GIF，≤500KB */
const LOGO_MAX_BYTES = 500 * 1024
const LOGO_STANDARD = '512 × 512 像素（正方形）'

const portal = ref<any>(null)
const form = reactive({ logo_url: '', system_name: '', welcome_title: '', welcome_text: '' })
const logoPreview = ref('')
const loading = ref(true)
const saving = ref(false)
const uploading = ref(false)
const error = ref('')
const notice = ref('')
const fileInput = ref<HTMLInputElement | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await api<any>('/tenant-portal/content', {}, { silent: true })
    portal.value = r
    form.logo_url = r.logo_url || ''
    form.system_name = r.system_name || ''
    form.welcome_title = r.welcome_title || ''
    form.welcome_text = r.welcome_text || ''
    logoPreview.value = r.logo_preview_url || r.logo_url || ''
  } catch (e) {
    error.value = (e as Error).message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function save(reset = false) {
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const body = reset
      ? { logo_url: '', system_name: '', welcome_title: '', welcome_text: '', reset_logo: true, reason: '恢复平台默认门户内容' }
      : { ...form, reset_logo: false, reason: '本厂管理员修改门户内容' }
    const r = await api<{ message: string }>('/tenant-portal/content', { method: 'PUT', body: JSON.stringify(body) })
    notice.value = r.message || '已保存'
    await load()
    window.dispatchEvent(new Event('clothp-branding-updated'))
  } catch (e) {
    error.value = (e as Error).message || '保存失败'
  } finally {
    saving.value = false
  }
}

function pickLogo() {
  fileInput.value?.click()
}

/* 上传前先按格式与大小校验，服务端再按文件头二次校验 */
async function onLogoPicked(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  error.value = ''
  notice.value = ''
  const type = file.type || ''
  const isPng = type === 'image/png' || (!type && /\.png$/i.test(file.name))
  const isGif = type === 'image/gif' || (!type && /\.gif$/i.test(file.name))
  if (!isPng && !isGif) {
    error.value = 'logo 仅支持 PNG 或 GIF 格式'
    return
  }
  if (file.size > LOGO_MAX_BYTES) {
    error.value = 'logo 文件不能超过 500KB，请压缩后再上传'
    return
  }
  uploading.value = true
  try {
    const r = await api<{ logo_url: string; message: string }>('/tenant-portal/logo', {
      method: 'POST',
      headers: { 'Content-Type': isPng ? 'image/png' : 'image/gif' },
      body: file,
    })
    logoPreview.value = r.logo_url
    form.logo_url = ''
    notice.value = r.message || '企业 logo 已上传'
    await load()
    window.dispatchEvent(new Event('clothp-branding-updated'))
  } catch (e) {
    error.value = (e as Error).message || '上传失败，请稍后重试'
  } finally {
    uploading.value = false
  }
}

async function removeLogo() {
  if (!window.confirm('确定移除本厂 logo？门户将显示平台默认 logo。')) return
  uploading.value = true
  error.value = ''
  notice.value = ''
  try {
    const r = await api<{ message: string }>('/tenant-portal/logo', { method: 'DELETE' })
    logoPreview.value = ''
    form.logo_url = ''
    notice.value = r.message || '企业 logo 已移除'
    await load()
    window.dispatchEvent(new Event('clothp-branding-updated'))
  } catch (e) {
    error.value = (e as Error).message || '移除失败，请稍后重试'
  } finally {
    uploading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page-header">
    <div><h1>门户内容</h1><p>门户页（登录后进入的欢迎页）顶部显示本厂 logo 与厂名；欢迎标题与欢迎语可自行维护</p></div>
    <button class="button secondary" @click="load">刷新</button>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>

  <section class="panel">
    <div class="panel-header">
      <h2>企业 logo 与系统名称</h2>
      <span class="status" :class="portal?.has_uploaded_logo ? 'active' : ''">{{ portal?.has_uploaded_logo ? '已上传' : '平台默认' }}</span>
    </div>
    <div class="panel-body">
      <p class="text-muted" style="margin-top:0">
        logo 标准尺寸：<strong>{{ LOGO_STANDARD }}</strong>；支持 <strong>PNG / GIF</strong> 格式，文件不超过 <strong>500KB</strong>。
        登录后的工作页面（左侧品牌区）会使用本厂 logo 与下方系统名称。
      </p>
      <div class="logo-row">
        <div class="logo-preview">
          <img v-if="logoPreview" :src="logoPreview" alt="企业 logo 预览">
          <span v-else>平台默认 logo</span>
        </div>
        <div class="logo-actions">
          <input ref="fileInput" class="hidden-file" type="file" accept="image/png,image/gif" @change="onLogoPicked">
          <button class="button" type="button" :disabled="uploading || loading" @click="pickLogo">
            {{ uploading ? '处理中…' : '上传 logo' }}
          </button>
          <button class="button secondary" type="button" :disabled="uploading || loading || !logoPreview" @click="removeLogo">
            移除本厂 logo
          </button>
          <p class="text-muted" style="margin:0">建议使用正方形、透明背景的图片，避免缩放后变形。</p>
        </div>
      </div>
      <div class="field" style="margin-top:16px;max-width:420px">
        <label>制衣厂系统名称</label>
        <input v-model.trim="form.system_name" maxlength="30" :placeholder="`留空默认「${portal?.default_system_name || '一智算'}」`">
      </div>
      <p class="text-muted" style="margin:8px 0 0">该名称显示在工作页面 logo 的旁边；修改后点下方「保存门户内容」生效。</p>
    </div>
  </section>

  <section class="panel">
    <div class="panel-header">
      <h2>欢迎区内容</h2>
      <span class="status active">{{ portal?.edition === 'trial' ? '免费体验版' : '正式版' }}</span>
    </div>
    <div class="panel-body">
      <p class="text-muted" style="margin-top:0">
        留空即使用平台默认内容。当前默认标题：<strong>{{ portal?.default_title || '—' }}</strong>
      </p>
      <div class="form-grid">
        <div class="field">
          <label>logo 图片网址（可选）</label>
          <input v-model.trim="form.logo_url" placeholder="https://… 或 /uploads/logo.png（留空用平台默认 logo）" maxlength="500">
        </div>
        <div class="field">
          <label>欢迎标题</label>
          <input v-model.trim="form.welcome_title" :placeholder="portal?.default_title || ''" maxlength="60">
        </div>
        <div class="field full">
          <label>欢迎语</label>
          <textarea v-model.trim="form.welcome_text" rows="4" maxlength="400" :placeholder="portal?.default_text || ''"></textarea>
        </div>
      </div>
      <p class="text-muted" style="margin:10px 0 0">
        提示：填写图片网址后以网址为准；使用「上传 logo」则以上传的文件为准，并可点「移除本厂 logo」恢复平台默认。
      </p>
      <div style="display:flex;gap:10px;margin-top:14px">
        <button class="button" :disabled="saving || loading" @click="save(false)">{{ saving ? '保存中…' : '保存门户内容' }}</button>
        <button class="button secondary" :disabled="saving || loading" @click="save(true)">恢复平台默认</button>
      </div>
      <p class="text-muted">只有本厂管理员可以修改；修改后门户页下次打开即生效。</p>
    </div>
  </section>
</template>

<style scoped>
.logo-row{display:flex;gap:22px;align-items:flex-start;flex-wrap:wrap}
.logo-preview{width:128px;height:128px;border:1px dashed #cfd8e6;border-radius:12px;display:grid;place-items:center;
  background:#f8fafc;overflow:hidden;color:#94a3b8;font-size:12px;text-align:center;padding:6px}
.logo-preview img{max-width:100%;max-height:100%;object-fit:contain}
.logo-actions{display:flex;flex-direction:column;gap:10px;min-width:200px}
.hidden-file{display:none}
</style>
