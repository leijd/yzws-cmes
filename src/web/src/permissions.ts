import { computed, reactive } from 'vue'
import { api } from './api'

const state = reactive({ permissions: {} as Record<string, boolean>, loaded: false, loading: null as Promise<void> | null })

function load(): Promise<void> {
  if (state.loaded) return Promise.resolve()
  if (!state.loading) {
    state.loading = api<{ actions: Record<string, boolean> }>('/me/permissions', {}, { silent: true })
      .then(data => { state.permissions = data.actions || {} })
      .catch(() => { state.permissions = {} })
      .finally(() => { state.loaded = true })
  }
  return state.loading
}

export function usePermissions() {
  load()
  const has = (code: string) => !!state.permissions[code]
  return { has, permissions: computed(() => state.permissions), ready: computed(() => state.loaded) }
}
