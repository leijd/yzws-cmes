import { computed, ref } from 'vue'

/** 轻量撤销/重做栈：状态必须是可 JSON 序列化的普通数据。 */
export function createHistory<T>(limit = 50) {
  const past = ref<any[]>([])
  const future = ref<any[]>([])
  let pending: T | null = null
  const clone = (v: T): T => JSON.parse(JSON.stringify(v))
  const serialize = (v: T) => JSON.stringify(v)
  const canUndo = computed(() => past.value.length > 0)
  const canRedo = computed(() => future.value.length > 0)

  function begin(current: T) { pending = clone(current) }
  function commit(current: T) {
    if (pending === null) return
    const before = pending
    pending = null
    if (serialize(before) === serialize(current)) return
    past.value.push(before)
    if (past.value.length > limit) past.value.shift()
    future.value = []
  }
  function cancel() { pending = null }
  function undo(current: T): T | null {
    if (!past.value.length) return null
    const prev = past.value.pop() as T
    future.value.push(clone(current))
    return clone(prev)
  }
  function redo(current: T): T | null {
    if (!future.value.length) return null
    const next = future.value.pop() as T
    past.value.push(clone(current))
    return clone(next)
  }
  function reset() { past.value = []; future.value = []; pending = null }

  return { begin, commit, cancel, undo, redo, reset, canUndo, canRedo }
}
