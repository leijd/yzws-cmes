import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

export function useAttachmentUrl(attachmentId: Ref<string | null | undefined>) {
  const url = ref('')
  const loading = ref(false)
  watch(attachmentId, async (id, previous) => {
    if (url.value) URL.revokeObjectURL(url.value)
    url.value = ''
    if (!id) return
    loading.value = true
    try {
      const response = await fetch(`/api/v1/attachments/${id}/content`, { headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}` } })
      if (response.ok) url.value = URL.createObjectURL(await response.blob())
    } finally {
      loading.value = false
    }
  }, { immediate: true })
  onBeforeUnmount(() => { if (url.value) URL.revokeObjectURL(url.value) })
  return { url, loading }
}
