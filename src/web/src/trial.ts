import { api } from './api'

/**
 * 免费体验登录：手机号 → 短信验证码（3 分钟内有效）→ 验证通过进入一智算演示主页。
 *
 * 后端 /auth/trial/* 未部署或短信网关未配置时自动降级为演示模式：
 * 验证码本地生成并展示在界面上，流程仍可完整体验；配置短信网关后走真实短信。
 */
const CHALLENGE_KEY = 'clothp_trial_challenge'
const SESSION_KEY = 'clothp_trial_session'

export const TRIAL_CODE_TTL_MS = 3 * 60 * 1000
export const TRIAL_RESEND_MS = 60 * 1000

export interface TrialChallenge {
  phone: string
  step: 'phone' | 'code'
  expiresAt: number
  resendAt: number
  demoCode: string
  smsSent: boolean
}

export type TrialVia = 'sms' | 'qr' | 'account' | 'guest'

export interface TrialSession {
  phone: string
  ticket: string
  expiresAt: number
  /** 顶部展示名：账号密码登录时是用户名 */
  displayName?: string
  via?: TrialVia
  /** 一键开通的真实体验厂：厂编码/名称与管理员初始凭据（仅保存在本机） */
  tenantCode?: string
  trialName?: string
  adminLogin?: string
  adminPassword?: string
  /** 体验厂进入令牌：点击“开始体验”时换取真实 MES 会话 */
  entryToken?: string
  /** 体验厂审批状态：pending / approved / rejected */
  status?: string
}

export function isValidPhone(phone: string) { return /^1[3-9]\d{9}$/.test(phone) }
export function maskPhone(phone: string) { return phone.replace(/^(\d{3})\d{4}(\d{4})$/, '$1****$2') }
export function formatCountdown(msLeft: number) {
  const seconds = Math.max(0, Math.ceil(msLeft / 1000))
  return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}

function randomCode() { return String(Math.floor(100000 + Math.random() * 900000)) }
function randomTicket() {
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
  }
  return `demo-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export interface SendCodeResult { expiresAt: number; demoCode: string; smsSent: boolean }

/** 后端不可达时允许本地演示；已配置真实服务商时禁止降级，避免绕过短信验证 */
async function demoModeAllowed(): Promise<boolean> {
  try {
    const result = await api<{ demo_mode: boolean }>('/auth/trial/sms-mode', {}, { silent: true })
    return !!result.demo_mode
  } catch {
    return true
  }
}

export async function sendTrialCode(phone: string): Promise<SendCodeResult> {
  try {
    const result = await api<{ expires_in: number; sms_sent: boolean; demo_code?: string }>(
      '/auth/trial/send-code',
      { method: 'POST', body: JSON.stringify({ phone }) },
      { silent: true },
    )
    return {
      expiresAt: Date.now() + (result.expires_in || 180) * 1000,
      demoCode: result.sms_sent ? '' : (result.demo_code || ''),
      smsSent: result.sms_sent,
    }
  } catch (error) {
    if (!(await demoModeAllowed())) throw error
    /* 后端或短信网关不可用时，降级为本地演示验证码 */
    return { expiresAt: Date.now() + TRIAL_CODE_TTL_MS, demoCode: randomCode(), smsSent: false }
  }
}

export interface VerifyResult { ok: boolean; message?: string; ticket?: string; expiresIn?: number }

/** 注册/体验开通共用的验证码发送（后端校验在开通时一次性完成） */
export async function sendRegisterCode(phone: string): Promise<SendCodeResult> {
  const result = await api<{ expires_in: number; sms_sent: boolean; demo_code?: string }>(
    '/public/tenant-register/send-code',
    { method: 'POST', body: JSON.stringify({ phone }) },
    { silent: true },
  )
  return {
    expiresAt: Date.now() + (result.expires_in || 180) * 1000,
    demoCode: result.sms_sent ? '' : (result.demo_code || ''),
    smsSent: result.sms_sent,
  }
}

export interface TrialTenantInfo {
  code: string
  name: string
  short_code: string
  admin_login_name: string
  /** 微秒时间戳；体验订阅到期时间 */
  trial_ends_at: number
  returning?: boolean
}

export interface TrialStartResult {
  code: string
  name: string
  short_code: string
  admin_login_name?: string
  /** 老用户凭手机号+验证码直接进入时不再返回密码 */
  initial_password?: string
  returning?: boolean
  message?: string
  trial_days: number
  trial_ends_at: number
  /** 体验厂审批状态：pending / approved；平台管理员通过后以手机号作为管理员账户 */
  status?: string
  registration_status?: string
  entry_token?: string
}

/** 一键免费体验：手机验证码通过后自动在制衣厂表开通体验厂（真实租户，30 天），并保存本机体验会话 */
export async function startTrial(phone: string, smsCode: string): Promise<TrialStartResult> {
  const result = await api<TrialStartResult>('/public/trial-register', {
    method: 'POST',
    body: JSON.stringify({ phone, sms_code: smsCode }),
  })
  /* 老用户：保留之前本机保存的管理员密码；新用户才写入初始密码 */
  const previous = readTrialSession()
  saveTrialSession({
    phone,
    ticket: 'trial-' + result.code,
    expiresAt: Math.floor((result.trial_ends_at || Date.now() + 30 * 86400000) / 1000),
    via: 'sms',
    displayName: result.name,
    tenantCode: result.code,
    trialName: result.name,
    adminLogin: result.admin_login_name || phone,
    adminPassword: result.initial_password || (previous?.tenantCode === result.code ? previous.adminPassword : undefined),
    entryToken: result.entry_token || (previous?.tenantCode === result.code ? previous.entryToken : undefined),
    status: result.registration_status || result.status || 'pending',
  })
  return result
}

/** 短信验证通过后回带的厂信息：trial=体验厂（一个手机号最多一个）；factory=该手机号绑定员工账号的制衣厂（可多个） */
export interface TrialTenantEntry {
  kind: 'trial' | 'factory'
  code: string
  name: string
  short_code: string
  admin_login_name?: string
  trial_ends_at?: number
  login_name?: string
  user_name?: string
  /** 体验厂进入令牌（审批通过后可直接进入 MES 工作台） */
  entry_token?: string
  status?: string
  registration_status?: string
}

export interface VerifyResult {
  ok: boolean
  message?: string
  ticket?: string
  expiresIn?: number
  /** 手机号已开通过体验厂时返回：验证码通过即直接进入该用户的主页（兼容旧字段） */
  tenant?: TrialTenantInfo
  /** 手机号关联的全部厂：1 个直接进入，多个弹出选厂列表 */
  tenants?: TrialTenantEntry[]
}

export async function verifyTrialCode(phone: string, code: string, demoCode: string): Promise<VerifyResult> {
  /* 始终走后端验证：即使是演示验证码，后端也知道该手机号名下的体验厂/制衣厂，
   * 本地比对会丢失「该手机号关联哪些厂」的信息（曾导致已审批的体验用户进了演示门户）。
   * 仅在后端不可达（网络错误）时才用演示验证码本地兜底。 */
  try {
    const result = await api<{ ticket: string; expires_in: number; tenant?: TrialTenantInfo; tenants?: TrialTenantEntry[] }>(
      '/auth/trial/verify',
      { method: 'POST', body: JSON.stringify({ phone, code }) },
      { silent: true },
    )
    return {
      ok: true, ticket: result.ticket, expiresIn: result.expires_in || 1800,
      tenant: result.tenant, tenants: result.tenants || (result.tenant ? [{
        kind: 'trial' as const, code: result.tenant.code, name: result.tenant.name,
        short_code: result.tenant.short_code, admin_login_name: result.tenant.admin_login_name,
        trial_ends_at: result.tenant.trial_ends_at, entry_token: result.ticket,
      }] : []),
    }
  } catch (e) {
    const message = (e as Error).message || ''
    /* 后端不可达且是演示验证码：本地兜底（仅能进入演示主页，无法识别厂） */
    if (demoCode && code === demoCode && /请求失败|Failed to fetch|NetworkError|网络/.test(message)) {
      return { ok: true, ticket: randomTicket(), expiresIn: 1800 }
    }
    return { ok: false, message: message || '验证失败，请稍后重试' }
  }
}

/** 手机号 + 制衣厂登录：短信验证通过的一次性 ticket + 厂编码 → 该厂绑定员工账号的真实会话 */
export async function phoneLogin(ticket: string, tenantCode: string, password = ''): Promise<{
  access_token: string
  refresh_token?: string
  password_change_required?: boolean
  /** 旧数据租户注册信息里没有手机号，需先补录 */
  need_contact_phone?: boolean
  permissions?: string[]
  tenant?: { code: string; name: string; contact_phone?: string }
}> {
  return api('/auth/phone-login', {
    method: 'POST',
    body: JSON.stringify({ ticket, tenant_code: tenantCode, password, node_id: 'web-local' }),
  })
}

/** 当前登录手机号名下的制衣厂（员工手机号或该厂注册联系电话命中） */
export interface PortalFactory {
  code: string
  name: string
  short_code: string
  login_name?: string
  user_name?: string
  /** 是否为当前正在浏览的制衣厂 */
  current?: boolean
}

export async function myFactories(): Promise<PortalFactory[]> {
  const result = await api<{ items: PortalFactory[] }>('/auth/my-factories', {}, { silent: true })
  return result.items || []
}

/** 切换到名下另一家制衣厂：换取该厂真实会话（手机号须已登记在该厂） */
export async function switchTenant(tenantCode: string): Promise<{
  access_token: string
  refresh_token?: string
  password_change_required?: boolean
  /** 目标厂注册信息里没有手机号且当前账号是厂管理员 */
  need_contact_phone?: boolean
  permissions?: string[]
  tenant?: { code: string; name: string; contact_phone?: string }
}> {
  return api('/auth/switch-tenant', {
    method: 'POST',
    body: JSON.stringify({ tenant_code: tenantCode, node_id: 'web-local' }),
  }, { silent: true })
}

/** 体验主页“开始体验”：凭进入令牌换取体验厂账号的真实 MES 会话（命中管理员则以管理员身份，命中员工则以该员工身份与权限进入） */
export async function trialEnter(input: {
  entry_token?: string
  ticket?: string
  phone?: string
  /** 普通员工首次进入：厂管理员提供的初始密码 */
  password?: string
  tenant_code: string
}): Promise<{
  access_token: string
  refresh_token?: string
  permissions?: string[]
  entry_token?: string
  /** 旧数据租户注册信息里没有手机号，需先补录 */
  need_contact_phone?: boolean
  tenant?: { code: string; name: string; customer_type?: string; contact_phone?: string }
}> {
  return api('/public/trial-enter', {
    method: 'POST',
    body: JSON.stringify({ ...input, node_id: 'web-local' }),
  }, { silent: true })
}

/** 按已授予的权限选择工作台首页（与账号登录一致） */
export function workspaceHome(permissions: string[] = []): string {
  const granted = new Set(permissions)
  const routes: Array<[string, string]> = [
    ['workbench:read', '/workbench/A03'], ['work_order:read', '/orders/C03'], ['task:read', '/tasks/D01'],
    ['report:read', '/reports/D06'], ['payroll:read', '/payroll/F09'], ['progress:read', '/progress/F01'],
    ['product:read', '/products/B01'], ['masterdata:read', '/masterdata/B06'],
    ['identity:read', '/identity/G05'], ['settings:read', '/settings/G01'],
  ]
  return routes.find(([permission]) => granted.has(permission))?.[1] || '/identity/G05'
}

/* ===== 加急审批与体验延期 ===== */

/** 审批中的体验厂：查看注册信息（审批中页面展示与修改；联系电话为登录手机号，不可改） */
export async function trialInfo(entryToken: string, tenantCode: string): Promise<{
  code: string; name: string; region: string; scale: string
  contact_name: string; contact_phone: string
  contact_wechat: string; contact_qq: string; contact_alipay: string
  address: string; remark: string
  registration_status: string; expedited: boolean
}> {
  return api(`/public/trial-info?entry_token=${encodeURIComponent(entryToken)}&tenant_code=${encodeURIComponent(tenantCode)}`, {}, { silent: true })
}

/** 审批中的体验厂：修改注册信息（联系电话不可修改） */
export async function trialUpdate(entryToken: string, tenantCode: string, form: {
  name: string; region: string; scale: string; contact_name: string
  contact_wechat: string; contact_qq: string; contact_alipay: string
  address: string; remark: string
}): Promise<{ message: string }> {
  return api('/public/trial-update', {
    method: 'POST',
    body: JSON.stringify({ entry_token: entryToken, tenant_code: tenantCode, ...form }),
  }, { silent: true })
}

/** 加急申请（审批中的体验厂；同一厂只能加急一次） */
export async function trialExpedite(entryToken: string, tenantCode: string): Promise<{ already: boolean; message: string }> {
  return api('/public/trial-expedite', {
    method: 'POST',
    body: JSON.stringify({ entry_token: entryToken, tenant_code: tenantCode }),
  }, { silent: true })
}

/** 延期申请（已审批通过的体验厂管理员） */
export async function trialExtend(entryToken: string, tenantCode: string, reason: string): Promise<{ message: string }> {
  return api('/public/trial-extend', {
    method: 'POST',
    body: JSON.stringify({ entry_token: entryToken, tenant_code: tenantCode, reason }),
  }, { silent: true })
}

export interface TrialExtensionRequest {
  id: string
  status: 'pending' | 'approved' | 'rejected'
  reason?: string | null
  review_reason?: string | null
  new_trial_ends_at?: number | null
  created_at?: number | null
  reviewed_at?: number | null
}

/** 延期状态查询（体验主页展示当前进度与最新截止时间） */
export async function trialExtendStatus(entryToken: string, tenantCode: string): Promise<{
  trial_ends_at?: number | null
  request?: TrialExtensionRequest | null
}> {
  return api(`/public/trial-extend/status?entry_token=${encodeURIComponent(entryToken)}&tenant_code=${encodeURIComponent(tenantCode)}`, {}, { silent: true })
}

/* ===== 验证码进程（刷新页面仍保留倒计时） ===== */
export function saveTrialChallenge(challenge: TrialChallenge | null) {
  try {
    if (challenge) sessionStorage.setItem(CHALLENGE_KEY, JSON.stringify(challenge))
    else sessionStorage.removeItem(CHALLENGE_KEY)
  } catch { /* 忽略隐私模式 */ }
}

export function readTrialChallenge(): TrialChallenge | null {
  try {
    const raw = sessionStorage.getItem(CHALLENGE_KEY)
    if (!raw) return null
    const value = JSON.parse(raw) as TrialChallenge
    if (!value || !isValidPhone(value.phone || '') || !value.expiresAt) return null
    return value
  } catch { return null }
}

/* ===== 体验会话 ===== */
export function saveTrialSession(session: TrialSession) {
  try { localStorage.setItem(SESSION_KEY, JSON.stringify(session)) } catch { /* 忽略隐私模式 */ }
}
export function readTrialSession(): TrialSession | null {
  try {
    const raw = localStorage.getItem(SESSION_KEY)
    if (!raw) return null
    const session = JSON.parse(raw) as TrialSession
    if (!session.phone || !session.ticket || !session.expiresAt || session.expiresAt <= Date.now()) {
      localStorage.removeItem(SESSION_KEY)
      return null
    }
    return session
  } catch { return null }
}
export function clearTrialSession() {
  try { localStorage.removeItem(SESSION_KEY) } catch { /* 忽略隐私模式 */ }
}
