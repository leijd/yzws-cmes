<script setup lang="ts">
/* 巡厂与证据（设计 4.26/5.15/6.9）：摄像设备管理、手机拍摄存证、证据封存与一次性导出 */
import { computed, onMounted, reactive, ref } from 'vue'
import { api, formatTime } from '../api'
import { toastError, toastSuccess } from '../toast'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import FilterSelect from '../components/FilterSelect.vue'
import PageState from '../components/PageState.vue'

const { has } = usePermissions()
const canManage = computed(() => has('video:manage'))
const canSeal = computed(() => has('evidence:seal'))
const canExport = computed(() => has('evidence:export'))

const tab = ref<'cameras' | 'evidence'>('evidence')
const loading = ref(false)
const bizTypeText: Record<string, string> = { quality: '质检', shipment: '出货', rework: '返工', payroll_dispute: '工资争议', security: '安全事件', outsourcing: '外协', general: '通用' }
const evidenceStatus: Record<string, string> = { capturing: '待封存', sealed: '已封存', archived: '已归档', expired: '已过期' }

/* ===== 摄像头 ===== */
const cameras = ref<any[]>([])
const cameraModal = ref(false)
const cameraForm = reactive({ id: '', code: '', name: '', protocol: 'rtsp', channel_no: '', stream_ref: '', bind_org_id: '', location_desc: '', no_recording_zone: false, retention_days: 30, remark: '', version: 0 })
const orgOptions = ref<any[]>([])
async function loadCameras() {
  loading.value = true
  try { cameras.value = (await api<{ items: any[] }>('/cameras')).items } finally { loading.value = false }
}
async function loadOrgs() {
  try { orgOptions.value = (await api<{ items: any[] }>('/workshops')).items.map((w: any) => ({ value: w.id, label: `${w.code} · ${w.name}` })) } catch { orgOptions.value = [] }
}
function openCamera(item?: any) {
  Object.assign(cameraForm, item ? { ...item, stream_ref: '' } : { id: '', code: '', name: '', protocol: 'rtsp', channel_no: '', stream_ref: '', bind_org_id: '', location_desc: '', no_recording_zone: false, retention_days: 30, remark: '', version: 0 })
  cameraModal.value = true
}
async function saveCamera() {
  try {
    if (cameraForm.id) await api(`/cameras/${cameraForm.id}`, { method: 'PATCH', body: JSON.stringify(cameraForm) })
    else await api('/cameras', { method: 'POST', body: JSON.stringify(cameraForm) })
    toastSuccess('已保存'); cameraModal.value = false; await loadCameras()
  } catch { /* */ }
}
async function disableCamera(item: any) {
  if (!window.confirm(`确认停用摄像头「${item.name}」？`)) return
  try { await api(`/cameras/${item.id}/disable`, { method: 'POST', body: '{}' }); toastSuccess('已停用'); await loadCameras() } catch { /* */ }
}
async function liveTicket(item: any) {
  try { const r = await api<{ note?: string }>(`/cameras/${item.id}/live-ticket`); toastSuccess(r.note || '播放票据已签发') } catch { /* */ }
}

/* ===== 证据 ===== */
const evidences = ref<any[]>([])
const filter = reactive({ biz_type: '', status: '' })
async function loadEvidence() {
  loading.value = true
  try {
    const q = new URLSearchParams()
    if (filter.biz_type) q.set('biz_type', filter.biz_type)
    if (filter.status) q.set('status', filter.status)
    evidences.value = (await api<{ items: any[] }>('/evidence?' + q.toString())).items
  } finally { loading.value = false }
}

/* 手机拍摄存证：选文件 → 附件直传 → 登记证据 */
const captureModal = ref(false)
const captureForm = reactive({ biz_type: 'quality', biz_id: '', remark: '' })
const captureFile = ref<File | null>(null)
const uploading = ref(false)
function onPick(e: Event) { captureFile.value = (e.target as HTMLInputElement).files?.[0] || null }
async function uploadCapture() {
  if (!captureFile.value) { toastError('请先拍摄或选择文件'); return }
  uploading.value = true
  try {
    const file = captureFile.value
    const bytes = new Uint8Array(await file.arrayBuffer())
    const hashBuffer = await crypto.subtle.digest('SHA-256', bytes)
    const sha256 = Array.from(new Uint8Array(hashBuffer), b => b.toString(16).padStart(2, '0')).join('')
    const init = await api<{ attachment_id: string; chunk_size: number }>('/attachments/init', {
      method: 'POST', body: JSON.stringify({ sha256, size_bytes: file.size, mime_type: file.type || 'image/jpeg', owner_type: 'evidence' }),
    })
    const chunkSize = init.chunk_size || 4 * 1024 * 1024
    let index = 0
    for (let offset = 0; offset < file.size; offset += chunkSize) {
      const chunk = file.slice(offset, offset + chunkSize)
      await fetch(`/api/v1/attachments/${init.attachment_id}/chunks/${index++}`, {
        method: 'PUT', headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}`, 'Content-Type': 'application/octet-stream' }, body: chunk,
      })
    }
    await api(`/attachments/${init.attachment_id}/complete`, { method: 'POST', body: '{}' })
    await api('/evidence/mobile-capture', {
      method: 'POST', body: JSON.stringify({
        attachment_id: init.attachment_id, biz_type: captureForm.biz_type, biz_id: captureForm.biz_id,
        device_info: navigator.userAgent.slice(0, 200), remark: captureForm.remark,
      }),
    })
    toastSuccess('拍摄证据已登记，可绑定单据封存')
    captureModal.value = false; captureFile.value = null
    await loadEvidence()
  } catch { /* toast 已提示 */ } finally { uploading.value = false }
}

/* 封存 */
const sealModal = ref(false)
const sealForm = reactive({ id: '', biz_type: 'quality', biz_id: '' })
function openSeal(item: any) { Object.assign(sealForm, { id: item.id, biz_type: item.biz_type || 'quality', biz_id: item.biz_id || '' }); sealModal.value = true }
async function doSeal() {
  try {
    const r = await api<{ seal_no: string }>(`/evidence/${sealForm.id}/bind`, { method: 'POST', body: JSON.stringify({ biz_type: sealForm.biz_type, biz_id: sealForm.biz_id }) })
    toastSuccess(`已封存：${r.seal_no}`); sealModal.value = false; await loadEvidence()
  } catch { /* */ }
}
/* 摄像头片段封存 */
const cameraSealModal = ref(false)
const cameraSealForm = reactive({ camera_id: '', start: '', end: '', biz_type: 'quality', biz_id: '', remark: '' })
function openCameraSeal() { Object.assign(cameraSealForm, { camera_id: '', start: '', end: '', biz_type: 'quality', biz_id: '', remark: '' }); cameraSealModal.value = true }
async function doCameraSeal() {
  const startAt = cameraSealForm.start ? new Date(cameraSealForm.start).getTime() * 1000 : 0
  const endAt = cameraSealForm.end ? new Date(cameraSealForm.end).getTime() * 1000 : 0
  try {
    const r = await api<{ seal_no: string }>('/evidence/seal', { method: 'POST', body: JSON.stringify({ camera_id: cameraSealForm.camera_id, start_at: startAt, end_at: endAt, biz_type: cameraSealForm.biz_type, biz_id: cameraSealForm.biz_id, remark: cameraSealForm.remark }) })
    toastSuccess(`已封存：${r.seal_no}`); cameraSealModal.value = false; await loadEvidence()
  } catch { /* */ }
}
/* 导出 */
async function exportEvidence(item: any) {
  if (!window.confirm('导出包含水印的证据文件？下载链接一次性、5 分钟有效。')) return
  try {
    const r = await api<{ ticket: string }>(`/evidence/${item.id}/export`, { method: 'POST', body: '{}' })
    window.open(`/api/v1/evidence/${item.id}/download?ticket=${encodeURIComponent(r.ticket)}`, '_blank')
  } catch { /* */ }
}

onMounted(() => { void loadCameras(); void loadEvidence(); void loadOrgs() })

const cameraOptions = computed(() => cameras.value.filter(c => c.status === 'active' && !c.no_recording_zone).map(c => ({ value: c.id, label: `${c.code} · ${c.name}` })))
const bizOptions = Object.entries(bizTypeText).map(([value, label]) => ({ value, label }))
</script>

<template>
  <section class="page">
    <header class="page-header">
      <h2>巡厂与证据</h2>
      <div class="toolbar">
        <button class="button" :class="{ secondary: tab !== 'evidence' }" @click="tab = 'evidence'">证据管理</button>
        <button class="button" :class="{ secondary: tab !== 'cameras' }" @click="tab = 'cameras'">摄像设备</button>
      </div>
    </header>

    <!-- 证据 -->
    <div v-if="tab === 'evidence'">
      <div class="toolbar">
        <FilterSelect v-model="filter.biz_type" :options="[{ value: '', label: '全部类型' }, ...bizOptions]" @update:model-value="loadEvidence" />
        <FilterSelect v-model="filter.status" :options="[{ value: '', label: '全部状态' }, { value: 'capturing', label: '待封存' }, { value: 'sealed', label: '已封存' }, { value: 'archived', label: '已归档' }]" @update:model-value="loadEvidence" />
        <span class="spacer"></span>
        <button v-if="canSeal" class="button" @click="captureModal = true">拍摄存证</button>
        <button v-if="canSeal" class="button secondary" @click="openCameraSeal">摄像头片段封存</button>
        <button class="button secondary" @click="loadEvidence">刷新</button>
      </div>
      <div class="table-wrap"><table><thead><tr><th>封存编号</th><th>来源</th><th>摄像头/拍摄人</th><th>业务类型</th><th>关联单据</th><th>SHA-256</th><th>状态</th><th>时间</th><th>操作</th></tr></thead>
        <tbody><tr v-for="e in evidences" :key="e.id">
          <td><strong>{{ e.seal_no || '—' }}</strong></td>
          <td>{{ e.source_type === 'mobile' ? '手机拍摄' : '摄像头' }}</td>
          <td>{{ e.source_type === 'mobile' ? (e.capturer_name || e.captured_by) : (e.camera_name || '—') }}</td>
          <td>{{ bizTypeText[e.biz_type] || e.biz_type }}</td><td>{{ e.biz_id || '—' }}</td>
          <td class="text-muted" style="font-family:monospace;font-size:12px">{{ e.sha256 ? e.sha256.slice(0, 12) + '…' : '—' }}</td>
          <td><span class="status" :class="e.status">{{ evidenceStatus[e.status] || e.status }}</span></td>
          <td>{{ formatTime(e.capture_at || e.created_at) }}</td>
          <td>
            <button v-if="canSeal && e.status === 'capturing'" class="button link" @click="openSeal(e)">绑定封存</button>
            <button v-if="canExport && e.status === 'sealed' && e.has_file" class="button link" @click="exportEvidence(e)">导出</button>
          </td>
        </tr></tbody></table><PageState :empty="!evidences.length" /></div>
    </div>

    <!-- 摄像头 -->
    <div v-if="tab === 'cameras'">
      <div class="toolbar">
        <button v-if="canManage" class="button" @click="openCamera()">新增摄像头</button>
        <button class="button secondary" @click="loadCameras">刷新</button>
      </div>
      <div class="table-wrap"><table><thead><tr><th>编码</th><th>名称</th><th>协议</th><th>绑定车间</th><th>位置</th><th>禁拍区</th><th>保留天数</th><th>状态</th><th>操作</th></tr></thead>
        <tbody><tr v-for="c in cameras" :key="c.id">
          <td><strong>{{ c.code }}</strong></td><td>{{ c.name }}</td><td>{{ c.protocol }}</td>
          <td>{{ c.bind_org_name || '—' }}</td><td>{{ c.location_desc || '—' }}</td>
          <td>{{ c.no_recording_zone ? '是' : '否' }}</td><td>{{ c.retention_days }}</td>
          <td><span class="status" :class="c.status">{{ c.status === 'active' ? '在线' : c.status === 'offline' ? '离线' : '停用' }}</span></td>
          <td>
            <button class="button link" @click="liveTicket(c)">实时查看</button>
            <button v-if="canManage" class="button link" @click="openCamera(c)">编辑</button>
            <button v-if="canManage && c.status !== 'disabled'" class="button link text-danger" @click="disableCamera(c)">停用</button>
          </td>
        </tr></tbody></table><PageState :empty="!cameras.length" /></div>
    </div>

    <!-- 摄像头新增/编辑 -->
    <ModalDialog v-if="cameraModal" :title="cameraForm.id ? '编辑摄像头' : '新增摄像头'" @close="cameraModal = false">
      <div class="form-grid">
        <div class="field"><label class="required">编码</label><input v-model.trim="cameraForm.code" :disabled="!!cameraForm.id"></div>
        <div class="field"><label class="required">名称</label><input v-model.trim="cameraForm.name"></div>
        <div class="field"><label>协议</label><FilterSelect v-model="cameraForm.protocol" :options="[{ value: 'rtsp', label: 'RTSP' }, { value: 'gb28181', label: 'GB28181' }, { value: 'onvif', label: 'ONVIF' }]" /></div>
        <div class="field"><label>通道号</label><input v-model.trim="cameraForm.channel_no"></div>
        <div class="field full"><label>流地址引用（不展示凭据）</label><input v-model.trim="cameraForm.stream_ref" placeholder="留空保持不变"></div>
        <div class="field"><label>绑定车间/产线</label><FilterSelect v-model="cameraForm.bind_org_id" :options="orgOptions" /></div>
        <div class="field"><label>位置说明</label><input v-model.trim="cameraForm.location_desc"></div>
        <div class="field"><label>保留天数</label><input v-model.number="cameraForm.retention_days" type="number" min="1"></div>
        <div class="field"><label style="display:flex;align-items:center;gap:6px"><input type="checkbox" v-model="cameraForm.no_recording_zone">禁拍区域（休息/更衣区）</label></div>
        <div class="field full"><label>备注</label><input v-model.trim="cameraForm.remark"></div>
      </div>
      <template #footer><button class="button secondary" @click="cameraModal = false">取消</button><button class="button" @click="saveCamera">保存</button></template>
    </ModalDialog>

    <!-- 拍摄存证 -->
    <ModalDialog v-if="captureModal" title="拍摄存证（照片/视频）" @close="captureModal = false">
      <div class="form-grid">
        <div class="field full">
          <label class="required">拍摄文件</label>
          <input type="file" accept="image/*,video/*" capture="environment" @change="onPick">
          <p class="text-muted" style="font-size:12px;margin-top:4px">存证类拍摄将自动叠加厂名/时间/拍摄人水印，服务端时间与哈希为准。</p>
        </div>
        <div class="field"><label class="required">业务类型</label><FilterSelect v-model="captureForm.biz_type" :options="bizOptions" /></div>
        <div class="field"><label>关联单据号</label><input v-model.trim="captureForm.biz_id" placeholder="可绑定后补"></div>
        <div class="field full"><label>备注</label><input v-model.trim="captureForm.remark"></div>
      </div>
      <template #footer>
        <button class="button secondary" @click="captureModal = false">取消</button>
        <button class="button" :disabled="uploading" @click="uploadCapture">{{ uploading ? '上传中…' : '上传并登记' }}</button>
      </template>
    </ModalDialog>

    <!-- 绑定封存 -->
    <ModalDialog v-if="sealModal" title="绑定单据并封存" @close="sealModal = false">
      <div class="form-grid">
        <div class="field"><label class="required">业务类型</label><FilterSelect v-model="sealForm.biz_type" :options="bizOptions" /></div>
        <div class="field"><label class="required">业务单据号</label><input v-model.trim="sealForm.biz_id"></div>
      </div>
      <p class="text-muted" style="font-size:13px">封存后证据只读、禁止修改或删除，可导出且含水印。</p>
      <template #footer><button class="button secondary" @click="sealModal = false">取消</button><button class="button" @click="doSeal">封存</button></template>
    </ModalDialog>

    <!-- 摄像头片段封存 -->
    <ModalDialog v-if="cameraSealModal" title="摄像头片段封存" @close="cameraSealModal = false">
      <div class="form-grid">
        <div class="field full"><label class="required">摄像头</label><FilterSelect v-model="cameraSealForm.camera_id" :options="cameraOptions" /></div>
        <div class="field"><label class="required">开始时间</label><input v-model="cameraSealForm.start" type="datetime-local"></div>
        <div class="field"><label class="required">结束时间</label><input v-model="cameraSealForm.end" type="datetime-local"></div>
        <div class="field"><label class="required">业务类型</label><FilterSelect v-model="cameraSealForm.biz_type" :options="bizOptions" /></div>
        <div class="field"><label>业务单据号</label><input v-model.trim="cameraSealForm.biz_id"></div>
        <div class="field full"><label>备注</label><input v-model.trim="cameraSealForm.remark"></div>
      </div>
      <template #footer><button class="button secondary" @click="cameraSealModal = false">取消</button><button class="button" @click="doCameraSeal">封存</button></template>
    </ModalDialog>
  </section>
</template>
