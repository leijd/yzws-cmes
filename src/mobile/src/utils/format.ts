/** 展示格式化工具 */
export function fmtNum(v: unknown): string {
  const n = Number(v);
  return Number.isFinite(n) ? String(n) : "0";
}

export function fmtMoney(v: unknown, unit = "元"): string {
  const n = Number(v);
  if (!Number.isFinite(n)) return `-- ${unit}`;
  return `${n.toFixed(2)} ${unit}`;
}

/* 报工审核状态文案（旧页面使用） */
export const reviewStatusText: Record<string, string> = {
  none: "无需审批",
  pending: "待审批",
  approved: "已通过",
  rejected: "已驳回",
  corrected: "已纠错",
};

export function fmtDate(v: unknown): string {
  if (!v) return "--";
  let d: Date | null = null;
  if (typeof v === "number") {
    // 后端时间戳可能是微秒
    d = new Date(v > 1e12 ? Math.floor(v / 1000) : v);
  } else {
    d = new Date(String(v).replace(" ", "T"));
  }
  if (!d || Number.isNaN(d.getTime())) return String(v);
  const p = (x: number) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

export function fmtDateTime(v: unknown): string {
  if (!v) return "--";
  let d: Date | null = null;
  if (typeof v === "number") {
    d = new Date(v > 1e12 ? Math.floor(v / 1000) : v);
  } else {
    d = new Date(String(v).replace(" ", "T"));
  }
  if (!d || Number.isNaN(d.getTime())) return String(v);
  const p = (x: number) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** 状态文案与标签颜色 */
export function statusTag(status: string): { text: string; cls: string } {
  const map: Record<string, { text: string; cls: string }> = {
    draft: { text: "草稿", cls: "tag-gray" },
    pending: { text: "待处理", cls: "tag-orange" },
    submitted: { text: "已提交", cls: "tag-blue" },
    approved: { text: "已审批", cls: "tag-green" },
    released: { text: "已下发", cls: "tag-blue" },
    in_progress: { text: "进行中", cls: "tag-blue" },
    running: { text: "进行中", cls: "tag-blue" },
    paused: { text: "已暂停", cls: "tag-orange" },
    completed: { text: "已完成", cls: "tag-green" },
    done: { text: "已完成", cls: "tag-green" },
    cancelled: { text: "已取消", cls: "tag-gray" },
    canceled: { text: "已取消", cls: "tag-gray" },
    confirmed: { text: "已确认", cls: "tag-green" },
    rejected: { text: "已驳回", cls: "tag-red" },
    voided: { text: "已作废", cls: "tag-gray" },
    paid: { text: "已发放", cls: "tag-green" },
    unpaid: { text: "未发放", cls: "tag-orange" },
    active: { text: "启用", cls: "tag-green" },
    disabled: { text: "停用", cls: "tag-gray" },
    read: { text: "已读", cls: "tag-gray" },
    unread: { text: "未读", cls: "tag-orange" },
  };
  return map[status] || { text: status || "--", cls: "tag-gray" };
}

/** 用户主键/状态徽标 */
export function userStatus(status: string): { text: string; cls: string } {
  return status === "active" ? { text: "启用", cls: "tag-green" } : { text: "停用", cls: "tag-gray" };
}

/* 旧页面兼容别名 */
export { fmtDate as formatDate, fmtDateTime as formatDateTime };

