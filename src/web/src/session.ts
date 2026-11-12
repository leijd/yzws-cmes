/**
 * 平台注销（退出登录）：
 * 1. 调 POST /auth/logout 让服务端吊销当前会话（访问令牌与刷新令牌同时失效），
 *    并吊销该手机号在本厂的「体验进入令牌」（服务端记录吊销时刻，旧令牌立即作废）；
 * 2. 清本机保存的登录凭证：MES 令牌、待补录手机号标记、体验会话（含 entering 令牌与初始密码）。
 *    体验会话不清会留下后门：退出后直接在地址栏输入 /trial/home 仍能免密进入该厂工作台；
 * 3. 回门户入口页（与移动端退出后回到门户登录页保持一致）。
 * 网络失败也照常退出，避免用户被卡在工作页面。
 */
import { api } from './api'
import { clearNeedPhone } from './phoneGate'
import { clearTrialSession, saveTrialChallenge } from './trial'

export function hasSession(): boolean {
  return !!localStorage.getItem('clothp_access_token')
}

/** 清本机所有登录相关缓存（令牌 + 体验会话 + 验证码进度 + 补录标记） */
export function clearLocalCredentials() {
  localStorage.removeItem('clothp_access_token')
  localStorage.removeItem('clothp_refresh_token')
  localStorage.removeItem('clothp_platform_access_token')
  clearTrialSession()
  saveTrialChallenge(null)
  clearNeedPhone()
}

export async function logoutToLogin(): Promise<void> {
  try {
    if (hasSession()) await api('/auth/logout', { method: 'POST' }, { silent: true })
  } catch {
    /* 会话已过期/网络异常：本地照常清理 */
  }
  clearLocalCredentials()
  window.location.href = '/portal'
}

/** 带二次确认的注销（工作页面右上角「退出」用） */
export async function confirmLogout(message = '退出后将注销当前账号，需要重新登录，确认退出？'): Promise<void> {
  if (!window.confirm(message)) return
  await logoutToLogin()
}
