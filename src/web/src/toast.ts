import { reactive } from 'vue'

export interface ToastItem { id: number; message: string }

export const toasts = reactive<ToastItem[]>([])

let seq = 0

export function toastError(message: string, durationMs = 4500) {
  const id = ++seq
  toasts.push({ id, message })
  window.setTimeout(() => {
    const index = toasts.findIndex(item => item.id === id)
    if (index >= 0) toasts.splice(index, 1)
  }, durationMs)
}

/** 成功提示：与错误提示同一样式通道，绿色标识由文案前缀区分 */
export function toastSuccess(message: string, durationMs = 3500) {
  toastError('✔ ' + message, durationMs)
}
