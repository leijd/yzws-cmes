<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { api } from '../api'
import ModalDialog from './ModalDialog.vue'

/* 取证入口（设计 6.12.8）：线检/总检/审核报工/成品入库/发货/外协回货验收等环节内嵌。
 * 支持拍摄存证（走既有 4.26 手机拍摄通道）与直播取证（6.12.8 实时画面 + 同步封存）。 */
const props = withDefaults(defineProps<{
  bizType: string
  bizId: string
  sceneType?: string
  sceneSub?: string
  label?: string
}>(), { sceneType: '', sceneSub: '', label: '取证' })

const emit = defineEmits<{ (e: 'saved'): void }>()
const open = ref(false)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const sessions = ref<any[]>([])
/* 默认手机实时画面：camera 来源后端要求绑定有效摄像头 ID，未选设备时不应默认走该通道 */
const form = reactive({ source_type: 'mobile', camera_id: '', remark: '', seal: true })

const allowed = computed(() => props.bizId !== '')

async function loadSessions() {
  if (!props.bizId) return
  try {
    const res = await api<{ items: any[] }>(`/evidence/live-sessions?biz_type=${encodeURIComponent(props.bizType)}&biz_id=${encodeURIComponent(props.bizId)}`, {}, { silent: true })
    sessions.value = res.items
  } catch { sessions.value = [] }
}

async function openPanel() {
  error.value = ''; notice.value = ''
  open.value = true
  await loadSessions()
}

async function startLive() {
  if (!allowed.value) { error.value = '请先保存业务单据后再发起直播'; return }
  if (form.source_type === 'camera' && !form.camera_id.trim()) {
    error.value = '固定摄像头直播需填写摄像头 ID，或改用手机实时画面'; return
  }
  busy.value = true; error.value = ''
  try {
    const res = await api<{ id: string; live_ticket: string }>('/evidence/live-sessions', {
      method: 'POST',
      body: JSON.stringify({ biz_type: props.bizType, biz_id: props.bizId, scene_type: props.sceneType,
        scene_sub: props.sceneSub, source_type: form.source_type, camera_id: form.camera_id, remark: form.remark }),
    })
    notice.value = `直播已发起（票据 ${res.live_ticket}）`
    await loadSessions()
    emit('saved')
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}

async function endLive(row: any) {
  busy.value = true; error.value = ''
  try {
    const res = await api<{ evidence_clip_id: string }>(`/evidence/live-sessions/${row.id}/end`, {
      method: 'POST', body: JSON.stringify({ seal: form.seal, remark: '' }),
    })
    notice.value = res.evidence_clip_id ? '直播已结束并封存证据片段' : '直播已结束'
    await loadSessions()
    emit('saved')
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}

async function capturePhoto() {
  /* 存证类拍摄：H5 调起相机实时拍摄，禁止相册导入；上传走既有 /evidence/mobile-capture 通道 */
  const input = document.createElement('input')
  input.type = 'file'; input.accept = 'image/*'; input.capture = 'environment' as any
  input.onchange = async () => {
    const file = input.files?.[0]
    if (!file) return
    busy.value = true; error.value = ''
    try {
      /* 与 EvidencePage 一致：分片上传（init → chunks → complete）后登记证据元数据 */
      const bytes = new Uint8Array(await file.arrayBuffer())
      const digest = await crypto.subtle.digest('SHA-256', bytes)
      const sha256 = Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('')
      const init = await api<{ attachment_id: string; chunk_size: number }>('/attachments/init', { method: 'POST',
        body: JSON.stringify({ sha256, size_bytes: file.size, mime_type: file.type || 'image/jpeg', owner_type: 'evidence' }) })
      const chunkSize = init.chunk_size || 4 * 1024 * 1024
      let index = 0
      for (let offset = 0; offset < file.size; offset += chunkSize) {
        const res = await fetch(`/api/v1/attachments/${init.attachment_id}/chunks/${index++}`, {
          method: 'PUT', headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}`, 'Content-Type': 'application/octet-stream' },
          body: file.slice(offset, offset + chunkSize) })
        if (!res.ok) throw new Error('分片上传失败')
      }
      await api(`/attachments/${init.attachment_id}/complete`, { method: 'POST', body: '{}' })
      /* evidence_clip.biz_type 为固定枚举，环节名映射到 quality/shipment/outsourcing/general */
      const bizEnum: Record<string, string> = {
        line_inspection: 'quality', final_inspection: 'quality', report_review: 'quality',
        shipment: 'shipment', outsource_return: 'outsourcing', finished_receipt: 'general',
      }
      await api('/evidence/mobile-capture', { method: 'POST', body: JSON.stringify({
        attachment_id: init.attachment_id, biz_type: bizEnum[props.bizType] || 'general', biz_id: props.bizId,
        device_info: navigator.userAgent.slice(0, 200), remark: `环节：${props.bizType}` }) })
      notice.value = '拍摄存证已上传并绑定当前单据'
      emit('saved')
    } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
  }
  input.click()
}
</script>

<template>
  <span class="evidence-entry">
    <button type="button" class="btn-ghost" :disabled="!allowed" :title="allowed ? '拍摄存证 / 直播取证' : '请先保存业务单据'" @click="openPanel">
      <span class="ev-icon">◎</span>{{ label }}
    </button>
    <ModalDialog v-if="open" :title="`取证 · ${label}`" @close="open = false">
      <div class="ev-body">
        <p class="ev-hint">线检、总检、审核报工、成品入库、发货、外协回货验收等环节可拍摄存证或发起直播取证；直播观看按数据范围授权。</p>
        <div class="ev-grid">
          <label>直播来源
            <select v-model="form.source_type"><option value="camera">固定摄像头实时流</option><option value="mobile">手机实时画面</option></select>
          </label>
          <label>摄像头 ID（可选）<input v-model="form.camera_id" placeholder="绑定车间/产线摄像头"></label>
          <label class="ev-full">备注<input v-model="form.remark" placeholder="取证说明"></label>
          <label class="ev-check"><input type="checkbox" v-model="form.seal"> 结束时同步封存片段为证据</label>
        </div>
        <div class="ev-actions">
          <button type="button" class="btn-primary" :disabled="busy" @click="startLive">● 发起直播取证</button>
          <button type="button" class="btn-outline" :disabled="busy" @click="capturePhoto">▣ 拍摄存证</button>
        </div>
        <p v-if="error" class="ev-error">{{ error }}</p>
        <p v-if="notice" class="ev-notice">{{ notice }}</p>
        <div v-if="sessions.length" class="ev-list">
          <h4>直播会话</h4>
          <div v-for="s in sessions" :key="s.id" class="ev-row">
            <span>{{ s.source_type === 'mobile' ? '手机实时画面' : '摄像头实时流' }}</span>
            <span class="ev-status" :class="s.status">{{ s.status === 'live' ? '直播中' : s.status === 'sealed' ? '已封存' : '已结束' }}</span>
            <span v-if="s.evidence_clip_id" class="ev-clip">证据 {{ s.evidence_clip_id.slice(0, 8) }}</span>
            <button v-if="s.status === 'live'" type="button" class="btn-mini" :disabled="busy" @click="endLive(s)">结束</button>
          </div>
        </div>
      </div>
      <template #footer><button type="button" class="btn-outline" @click="open = false">关闭</button></template>
    </ModalDialog>
  </span>
</template>

<style scoped>
.evidence-entry { display: inline-flex; }
.btn-ghost { padding: 5px 10px; border: 1px solid #DDE3EE; border-radius: 7px; background: #fff; color: #4E5562; font-size: 12px; cursor: pointer; }
.btn-ghost:hover:not(:disabled) { border-color: #2F6BFF; color: #2F6BFF; }
.btn-ghost:disabled { opacity: .5; cursor: not-allowed; }
.ev-icon { margin-right: 4px; }
.ev-hint { color: #6B7280; font-size: 12px; line-height: 1.6; margin: 0 0 12px; }
.ev-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.ev-grid label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: #4E5562; }
.ev-grid input, .ev-grid select { padding: 6px 8px; border: 1px solid #DDE3EE; border-radius: 6px; font-size: 13px; }
.ev-full { grid-column: 1 / -1; }
.ev-check { flex-direction: row !important; align-items: center; gap: 6px !important; grid-column: 1 / -1; }
.ev-actions { display: flex; gap: 8px; margin-top: 14px; }
.btn-primary { padding: 7px 14px; border: 0; border-radius: 7px; background: linear-gradient(135deg,#DC2626,#F97316); color: #fff; font-size: 13px; cursor: pointer; }
.btn-outline { padding: 7px 14px; border: 1px solid #DDE3EE; border-radius: 7px; background: #fff; color: #4E5562; font-size: 13px; cursor: pointer; }
.btn-primary:disabled, .btn-outline:disabled { opacity: .55; cursor: not-allowed; }
.ev-error { color: #DC2626; font-size: 12px; margin: 10px 0 0; }
.ev-notice { color: #16A34A; font-size: 12px; margin: 10px 0 0; }
.ev-list { margin-top: 16px; border-top: 1px solid #EEF1F6; padding-top: 12px; }
.ev-list h4 { margin: 0 0 8px; font-size: 13px; }
.ev-row { display: flex; align-items: center; gap: 10px; padding: 6px 0; font-size: 12px; border-bottom: 1px dashed #EEF1F6; }
.ev-status { padding: 1px 7px; border-radius: 9px; }
.ev-status.live { color: #DC2626; background: #FEE2E2; }
.ev-status.sealed { color: #16A34A; background: #DCFCE7; }
.ev-status.ended { color: #6B7280; background: #F3F4F6; }
.ev-clip { color: #8A9099; }
.btn-mini { margin-left: auto; padding: 3px 9px; border: 1px solid #DDE3EE; border-radius: 6px; background: #fff; cursor: pointer; font-size: 11px; }
</style>
