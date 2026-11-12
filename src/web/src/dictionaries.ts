import { api } from './api'

/**
 * 下拉选项从数据库实际字典数据获取（GET /dictionaries/{type}/options），
 * 前端模块级缓存 60 秒（与后端缓存同 TTL，写操作后端已按租户+类型失效）。
 * valueKey：nation/position 存中文名（value=name），skill_grade/rate_level 存编码（value=code）。
 */
export interface DictOption { value: string; label: string }

const TTL_MS = 60 * 1000
const cache = new Map<string, { at: number; options: DictOption[] }>()

export async function dictOptions(type: string, valueKey: 'code' | 'name' = 'code'): Promise<DictOption[]> {
  const key = `${type}:${valueKey}`
  const hit = cache.get(key)
  if (hit && Date.now() - hit.at < TTL_MS) return hit.options
  try {
    const result = await api<{ items: { code: string; name: string }[] }>(`/dictionaries/${type}/options`, {}, { silent: true })
    const options = (result.items || []).map(item => ({ value: item[valueKey], label: item.name }))
    cache.set(key, { at: Date.now(), options })
    return options
  } catch {
    return hit?.options || [] /* 网络/权限异常时保留旧缓存，避免下拉被清空 */
  }
}
