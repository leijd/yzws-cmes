const pad = (n: number) => String(n).padStart(2, '0')

/** 统一日期显示格式：yyyy-MM-dd（本地时区） */
export function formatDate(value: number | string | Date | null | undefined): string {
  if (value === null || value === undefined || value === '') return ''
  const date = value instanceof Date ? value : typeof value === 'number' ? new Date(value / 1000) : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

/** 后端微秒时间戳按 yyyy-MM-dd 显示 */
export function formatMicros(micros?: number | null): string {
  return micros ? formatDate(new Date(Number(micros) / 1000)) : ''
}

/** 当天日期：yyyy-MM-dd（本地时区） */
export function todayISO(): string {
  const d = new Date()
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
