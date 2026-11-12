import { ref } from "vue";
import { request } from "./request";

export interface PagedResult<T> {
  items: T[];
  next_cursor?: string;
  has_more?: boolean;
}

/** 游标分页列表：后端统一返回 {items, next_cursor, has_more} */
export function usePagedList<T = Record<string, unknown>>(
  path: string,
  params: Record<string, unknown> = {},
  options: { silent?: boolean } = {}
) {
  const items = ref<T[]>([]) as { value: T[] };
  const cursor = ref("");
  const hasMore = ref(false);
  const loading = ref(false);
  const loaded = ref(false);
  const error = ref("");

  function buildQuery(reset: boolean): string {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null && v !== "") q.set(k, String(v));
    }
    if (!reset && cursor.value) q.set("cursor", cursor.value);
    const s = q.toString();
    return s ? `${path}?${s}` : path;
  }

  async function load(reset = true): Promise<void> {
    if (loading.value) return;
    if (!reset && !hasMore.value && loaded.value) return;
    loading.value = true;
    error.value = "";
    try {
      const res = await request<PagedResult<T>>(buildQuery(reset), {
        silent: true,
      });
      const list = res.items || [];
      if (reset) items.value = list;
      else items.value = [...items.value, ...list];
      cursor.value = res.next_cursor || "";
      hasMore.value = !!res.has_more;
      loaded.value = true;
    } catch (e) {
      error.value = (e as Error).message || "加载失败";
      loaded.value = true;
    } finally {
      loading.value = false;
    }
  }

  return { items, hasMore, loading, loaded, error, load };
}
