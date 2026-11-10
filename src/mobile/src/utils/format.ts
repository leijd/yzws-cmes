// 后端时间戳为微秒
export function formatDateTime(micros?: number | null): string {
  if (!micros) return "—";
  return new Date(micros / 1000).toLocaleString("zh-CN", { hour12: false });
}

export function formatDate(micros?: number | null): string {
  if (!micros) return "—";
  return new Date(micros / 1000).toLocaleDateString("zh-CN");
}

export const reviewStatusText: Record<string, string> = {
  pending: "待审批",
  approved: "已通过",
  rejected: "已驳回",
};
