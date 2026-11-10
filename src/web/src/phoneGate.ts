/**
 * 旧数据租户补录注册联系电话。
 * 早期租户（体验/付费）注册信息里没有手机号，一直用账号密码登录。
 * 登录响应带 need_contact_phone=true 时记下标记，路由守卫据此先把厂管理员引导到 /complete-phone，
 * 补录完成（或退出登录）后清除标记。补录只登记号码，不发送也不校验短信验证码。
 */
export const NEED_PHONE_KEY = 'clothp_need_contact_phone'

/** 记住「待补录手机号」状态，值存厂编码（可能为空，用 1 占位） */
export function stashNeedPhone(tenantCode?: string) {
  try { localStorage.setItem(NEED_PHONE_KEY, tenantCode || '1') } catch { /* 忽略隐私模式 */ }
}

export function clearNeedPhone() {
  try { localStorage.removeItem(NEED_PHONE_KEY) } catch { /* 忽略隐私模式 */ }
}

/** 按本次登录结果同步补录标记：需要补录则记下厂编码，否则清掉历史残留标记（避免换账号后被误拦） */
export function syncNeedPhone(need: boolean | undefined, tenantCode?: string) {
  if (need) stashNeedPhone(tenantCode)
  else clearNeedPhone()
}

/** 待补录的厂编码（未记录时返回空串） */
export function pendingPhoneTenant(): string {
  try {
    const value = localStorage.getItem(NEED_PHONE_KEY) || ''
    return value === '1' ? '' : value
  } catch { return '' }
}

/** 是否处于「厂管理员待补录注册手机号」状态（必须同时有登录态） */
export function needPhoneGate(): boolean {
  try {
    return !!localStorage.getItem(NEED_PHONE_KEY) && !!localStorage.getItem('clothp_access_token')
  } catch { return false }
}
