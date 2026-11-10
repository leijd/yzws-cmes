import { api } from './api'

/**
 * 体验账号：用户名 + 密码登录，成功后进入用户体验页面。
 *
 * 后端 /public/trial-auth/* 可用时由服务端签发 30 分钟会话票据；
 * 后端未启动（本地预览）时自动降级为浏览器内演示账号，流程仍可完整体验。
 * 离线账号只保存在当前浏览器，且仅做演示用途的摘要混淆，切勿填写真实密码。
 */
const ACCOUNTS_KEY = 'clothp_trial_accounts'
const USERNAME_RE = /^[A-Za-z0-9_\u4e00-\u9fa5]{2,20}$/
const SESSION_SECONDS = 30 * 60

export const TRIAL_DEMO_USERNAME = 'demo'
export const TRIAL_DEMO_PASSWORD = 'demo123456'

export interface TrialAuthResult {
  username: string
  ticket: string
  expires_in: number
  /** 后端不可达时为 true：账号与本机会话仅存在于当前浏览器 */
  offline: boolean
}

interface LocalAccount { username: string; salt: string; hash: string }

export function validateTrialUsername(username: string): string {
  return USERNAME_RE.test(username.trim()) ? '' : '用户名需为 2-20 位字母、数字、下划线或中文'
}

export function validateTrialPassword(password: string): string {
  return [...password].length >= 6 ? '' : '密码至少 6 位'
}

/** 演示用的非加密摘要：只避免明文出现在 localStorage，不提供任何安全保证 */
function demoDigest(text: string): string {
  let h1 = 0x811c9dc5
  let h2 = 0x01000193
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i)
    h1 = Math.imul(h1 ^ c, 16777619) >>> 0
    h2 = Math.imul(h2 + c + i, 2246822519) >>> 0
  }
  return `${h1.toString(16).padStart(8, '0')}${h2.toString(16).padStart(8, '0')}`
}

function randomToken(prefix: string): string {
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    return `${prefix}-${Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')}`
  }
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function readAccounts(): LocalAccount[] {
  try {
    const raw = localStorage.getItem(ACCOUNTS_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter((item): item is LocalAccount =>
      !!item && typeof item.username === 'string' && typeof item.salt === 'string' && typeof item.hash === 'string')
  } catch { return [] }
}

function writeAccounts(accounts: LocalAccount[]) {
  try { localStorage.setItem(ACCOUNTS_KEY, JSON.stringify(accounts)) } catch { /* 忽略隐私模式 */ }
}

function ensureDemoAccount(): LocalAccount[] {
  const accounts = readAccounts()
  if (accounts.some(a => a.username === TRIAL_DEMO_USERNAME)) return accounts
  const salt = 'clothp-demo'
  accounts.push({ username: TRIAL_DEMO_USERNAME, salt, hash: demoDigest(`${salt}:${TRIAL_DEMO_PASSWORD}`) })
  writeAccounts(accounts)
  return accounts
}

function localSession(username: string): TrialAuthResult {
  return { username, ticket: randomToken('local'), expires_in: SESSION_SECONDS, offline: true }
}

async function localRegister(username: string, password: string): Promise<TrialAuthResult> {
  const accounts = ensureDemoAccount()
  if (accounts.some(a => a.username === username)) throw new Error('该用户名已被注册')
  const salt = randomToken('salt')
  accounts.push({ username, salt, hash: demoDigest(`${salt}:${password}`) })
  writeAccounts(accounts)
  return localSession(username)
}

async function localLogin(username: string, password: string): Promise<TrialAuthResult> {
  const accounts = ensureDemoAccount()
  const account = accounts.find(a => a.username === username)
  if (!account || account.hash !== demoDigest(`${account.salt}:${password}`)) {
    throw new Error('用户名或密码不正确')
  }
  return localSession(username)
}

async function requestTrialAuth(action: 'login' | 'register', username: string, password: string): Promise<TrialAuthResult> {
  try {
    const result = await api<{ username: string; ticket: string; expires_in: number }>(
      `/public/trial-auth/${action}`,
      { method: 'POST', body: JSON.stringify({ username, password }) },
      { silent: true },
    )
    return {
      username: result.username,
      ticket: result.ticket,
      expires_in: result.expires_in || SESSION_SECONDS,
      offline: false,
    }
  } catch (e) {
    const error = e as Error & { code?: string }
    /* 有业务错误码说明后端在线，如实展示；只有后端不可达才降级到本机会话 */
    if (error.code) throw error
    return action === 'register' ? localRegister(username, password) : localLogin(username, password)
  }
}

export function trialAuthLogin(username: string, password: string): Promise<TrialAuthResult> {
  return requestTrialAuth('login', username.trim(), password)
}
export function trialAuthRegister(username: string, password: string): Promise<TrialAuthResult> {
  return requestTrialAuth('register', username.trim(), password)
}
export function trialAuthMe(ticket: string): Promise<{ username: string }> {
  return api<{ username: string }>(`/public/trial-auth/me?ticket=${encodeURIComponent(ticket)}`, {}, { silent: true })
}
