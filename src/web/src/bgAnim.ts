import { ref } from 'vue'

/**
 * 背景动效开关（全局偏好）
 * - 默认关闭，避免首页/登录页持续跑 canvas 动画占用 GPU
 * - 用户切换后写入 localStorage，跨页面、跨刷新保持一致
 */
const KEY = 'clothp_bg_anim'

function read(): boolean {
  try { return localStorage.getItem(KEY) === '1' } catch { return false }
}

export const bgAnimEnabled = ref(read())

export function setBgAnim(v: boolean) {
  bgAnimEnabled.value = v
  try { localStorage.setItem(KEY, v ? '1' : '0') } catch { /* 隐私模式下写入失败可忽略 */ }
}

export function toggleBgAnim() {
  setBgAnim(!bgAnimEnabled.value)
}
