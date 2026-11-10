import { toDataURL } from 'qrcode'
import { api } from './api'

/** 三个模块入口的扫码登录：二维码前端生成，票据状态由后端在设备间同步 */
export type QrApp = 'zhisuan' | 'couhe' | 'sansheng'

export const QR_APP_NAMES: Record<QrApp, string> = {
  zhisuan: '一智算', couhe: '二撮合', sansheng: '三生万',
}
export const QR_APP_COLORS: Record<QrApp, string> = {
  zhisuan: '#1d4ed8', couhe: '#0f766e', sansheng: '#7c3aed',
}
export const QR_APP_LOGIN: Record<QrApp, string> = {
  zhisuan: '/login', couhe: '/login/matcher', sansheng: '/login/sansheng',
}

/** 二维码内用单字母模块码，显著缩短 URL、降低二维码密度，提升手机识别率 */
export const QR_APP_SHORT: Record<QrApp, string> = {
  zhisuan: 'z', couhe: 'c', sansheng: 's',
}
const SHORT_TO_APP: Record<string, QrApp> = { z: 'zhisuan', c: 'couhe', s: 'sansheng' }
export function qrAppFromShort(value: string): QrApp | null {
  return SHORT_TO_APP[value] || null
}

export type QrTicketStatus = 'pending' | 'scanned' | 'confirmed' | 'cancelled' | 'expired'

export interface QrTicket {
  ticket: string
  app: QrApp
  app_name: string
  expires_in: number
}
export interface QrTicketState {
  status: QrTicketStatus
  app: QrApp
  app_name: string
  expires_in: number
  /** 手机端已完成真实身份校验时返回，PC 端用它一次性换取 JWT */
  login_ticket?: string
}

export function createLoginQr(app: QrApp): Promise<QrTicket> {
  return api<QrTicket>('/public/login-qr/create',
    { method: 'POST', body: JSON.stringify({ app }) }, { silent: true })
}
export function getQrStatus(ticket: string): Promise<QrTicketState> {
  return api<QrTicketState>(`/public/login-qr/status?ticket=${encodeURIComponent(ticket)}`, {}, { silent: true })
}
export function postQrAction(action: 'scan' | 'confirm' | 'cancel', ticket: string): Promise<unknown> {
  return api(`/public/login-qr/${action}`,
    { method: 'POST', body: JSON.stringify({ ticket }) }, { silent: true })
}

/* ===== 微信扫码登录（演示版微信身份：手机浏览器本地持久 device_id） ===== */
const WECHAT_DEVICE_KEY = 'clothp_wechat_device_id'

/** 取本机微信身份标识；首次扫码时生成并永久保存在当前手机浏览器。 */
export function wechatDeviceID(): string {
  let value = localStorage.getItem(WECHAT_DEVICE_KEY) || ''
  if (!value) {
    value = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : `wx-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`
    localStorage.setItem(WECHAT_DEVICE_KEY, value)
  }
  return value
}

export interface WechatBoundAccount {
  tenant_code: string
  tenant_name: string
  phone: string
  user_name: string
}
export interface WechatStatus {
  bound: boolean
  account?: WechatBoundAccount
}

/** 本机微信身份是否已绑定厂账号：已绑定时后续扫码免密码、免验证码。 */
export function getWechatStatus(deviceID: string): Promise<WechatStatus> {
  return api<WechatStatus>(`/public/login-qr/wechat-status?device_id=${encodeURIComponent(deviceID)}`, {}, { silent: true })
}

export interface WechatLoginInput {
  ticket: string
  device_id: string
  phone?: string
  code?: string
  /** 普通员工首次微信登录：厂管理员提供的密码 */
  password?: string
  /** 可选 5 位厂标识：同一手机号绑定多个厂时用于指定 */
  short_code?: string
}

/** 手机端完成微信身份校验：首次需手机号+验证码（+首次密码），以后只凭已绑定 device_id。 */
export function wechatLogin(input: WechatLoginInput): Promise<{ status: string; ticket: string }> {
  return api('/public/login-qr/wechat-login',
    { method: 'POST', body: JSON.stringify({ ...input, node_id: 'web-local' }) }, { silent: true })
}

/** PC 端一次性取走扫码登录结果（真实 JWT 会话）。 */
export function exchangeQrLogin(loginTicket: string): Promise<{
  access_token: string
  refresh_token?: string
  password_change_required?: boolean
  permissions?: string[]
  tenant?: { code: string; name: string }
}> {
  return api('/public/login-qr/exchange',
    { method: 'POST', body: JSON.stringify({ login_ticket: loginTicket }) }, { silent: true })
}

/**
 * 二维码统一指向公网体验域名，而不是当前页面的 origin。
 * 用 localhost / 127.0.0.1 打开首页时，手机扫到的地址必须能被手机访问。
 */
export const QR_BASE_URL = 'https://seal-laid-field-meyer.trycloudflare.com'

/** 后端在线：二维码指向手机确认页，确认后 PC 端自动登录 */
export function qrLoginUrl(ticket: string, app: QrApp): string {
  return `${QR_BASE_URL}/q?t=${encodeURIComponent(ticket)}&a=${QR_APP_SHORT[app]}`
}
/** 后端离线：二维码仍可扫，直接打开该模块登录页 */
export function moduleLoginUrl(app: QrApp): string {
  return `${QR_BASE_URL}${QR_APP_LOGIN[app]}?from=qr&app=${encodeURIComponent(app)}`
}

/** 生成带静区的真实二维码（不同模块用各自主题色） */
export function makeQrImage(payload: string, dark: string, width = 320): Promise<string> {
  return toDataURL(payload, {
    errorCorrectionLevel: 'M',
    margin: 2,
    width,
    color: { dark, light: '#ffffffff' },
  })
}

export function formatQrCountdown(seconds: number): string {
  const value = Math.max(0, seconds)
  return `${String(Math.floor(value / 60)).padStart(2, '0')}:${String(value % 60).padStart(2, '0')}`
}
