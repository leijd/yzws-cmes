/* 场景化菜单（设计 v0.02-002 6.12）：工作台 / 生产管理 / 基本资料 / 分析中心 / 系统管理。
 * 从 App.vue 抽出，供侧边栏与流程待办配置（拓扑节点=生产管理菜单项）共享。 */
export interface NavItem {
  path: string
  label: string
  icon: string
  group: string
  permission: string
}

export const nav: NavItem[] = [
  /* 工作台 */
  { path: '/workbench/A03', label: '工作台', icon: '⌂', group: '工作台', permission: 'workbench:read' },
  { path: '/intake/new', label: '新建项目向导', icon: '✚', group: '工作台', permission: 'intake:write' },
  { path: '/notifications/A04', label: '消息与会话', icon: '✉', group: '工作台', permission: '' },
  { path: '/evidence', label: '巡厂与证据', icon: '◉', group: '工作台', permission: 'video:view' },
  { path: '/attendance/screen', label: '考勤屏', icon: '▣', group: '工作台', permission: 'attendance:screen' },
  { path: '/attendance/records', label: '简单考勤', icon: '⏱', group: '工作台', permission: 'attendance:manage' },

  /* 生产管理：按生产链路顺序 */
  { path: '/orders/C03', label: '产品订单管理', icon: '▤', group: '生产管理', permission: 'work_order:read' },
  { path: '/materials/receipts', label: '物料入库', icon: '▥', group: '生产管理', permission: 'inventory:read' },
  { path: '/cutting/sheets', label: '裁床', icon: '▤', group: '生产管理', permission: 'cutting:read' },
  { path: '/markers/C11', label: '唛架管理', icon: '▤', group: '生产管理', permission: 'marker:read' },
  { path: '/print/C10', label: '菲票打印', icon: '⎙', group: '生产管理', permission: 'print:read' },
  { path: '/labels/C12', label: '扎标签', icon: '▥', group: '生产管理', permission: 'work_order:read' },
  { path: '/products/B01', label: '产品工序', icon: '◇', group: '生产管理', permission: 'product:read' },
  { path: '/tasks/D01', label: '生产工单管理', icon: '✓', group: '生产管理', permission: 'task:read' },
  { path: '/materials/outbound', label: '物料出库', icon: '⇅', group: '生产管理', permission: 'inventory:issue,inventory:issue_confirm' },
  { path: '/reports/D06?tab=report', label: '报工', icon: '◎', group: '生产管理', permission: 'report:read' },
  { path: '/reports/D06?tab=review', label: '审核报工', icon: '⊘', group: '生产管理', permission: 'report:read' },
  { path: '/quality/process-inspections', label: '线检', icon: '◉', group: '生产管理', permission: 'work_order:read' },
  { path: '/quality/E01', label: '总检', icon: '▣', group: '生产管理', permission: 'work_order:read' },
  { path: '/finished-receipts', label: '成品入库', icon: '▦', group: '生产管理', permission: 'work_order:read' },
  { path: '/shipments/E04', label: '发货', icon: '⇧', group: '生产管理', permission: 'shipment:read' },
  { path: '/rework/E03', label: '返工复检', icon: '⟳', group: '生产管理', permission: 'work_order:read' },
  { path: '/outsource/orders', label: '外协管理', icon: '⇄', group: '生产管理', permission: 'outsource:read' },

  /* 基本资料 */
  { path: '/masterdata/customers', label: '客户资料', icon: '◈', group: '基本资料', permission: 'masterdata:read' },
  { path: '/masterdata/B06', label: '员工信息', icon: '♙', group: '基本资料', permission: '' },
  { path: '/settings/workshops', label: '车间管理', icon: '⌂', group: '基本资料', permission: 'workshop:read' },
  { path: '/settings/lines', label: '生产线管理', icon: '▤', group: '基本资料', permission: 'line:read' },
  { path: '/settings/teams', label: '生产组管理', icon: '♟', group: '基本资料', permission: 'team:read' },
  { path: '/settings/users', label: '人员与组织', icon: '♙', group: '基本资料', permission: 'identity:read' },
  { path: '/materials/master', label: '仓库信息', icon: '▦', group: '基本资料', permission: 'warehouse:read' },
  { path: '/process-standards', label: '标准工序', icon: '☷', group: '基本资料', permission: 'process:read' },
  { path: '/settings/dictionaries', label: '数据字典', icon: '☰', group: '基本资料', permission: 'masterdata:read' },
  { path: '/settings/workflow-badges', label: '流程待办配置', icon: '↝', group: '系统管理', permission: 'settings:read' },

  /* 分析中心 */
  { path: '/progress/F01', label: '进度与统计', icon: '◫', group: '分析中心', permission: 'progress:read' },
  { path: '/payroll/F09', label: '工资单管理', icon: '¥', group: '分析中心', permission: 'payroll:read' },
  { path: '/analysis/salary', label: '员工薪水分析', icon: '¥', group: '分析中心', permission: 'salary:read' },
  { path: '/mywage/H02', label: '我的工资', icon: '¥', group: '分析中心', permission: '' },

  /* 系统管理 */
  { path: '/tenant/profile', label: '本厂信息', icon: '⌂', group: '系统管理', permission: '' },
  { path: '/portal/settings', label: '门户内容', icon: '▤', group: '系统管理', permission: '' },
  { path: '/identity/G05', label: '用户角色权限', icon: '⚷', group: '系统管理', permission: 'identity:read' },
  { path: '/identity/sensitive-requests', label: '敏感信息授权审批', icon: '✉', group: '系统管理', permission: 'identity:write' },
  { path: '/settings/G01', label: '功能设置', icon: '⚙', group: '系统管理', permission: 'settings:read' },
  { path: '/import-export/G09', label: '导入导出', icon: '⇅', group: '系统管理', permission: 'settings:read' },
  { path: '/backup/G10', label: '备份与同步', icon: '⛁', group: '系统管理', permission: 'settings:read' },
  { path: '/tenant/backups', label: '明文备份导出', icon: '⚿', group: '系统管理', permission: 'backup:export' },
  { path: '/audit/G08', label: '审计与字段', icon: '⌘', group: '系统管理', permission: 'settings:read' },
]

export const defaultHiddenPaths = ['/markers/C11', '/print/C10', '/labels/C12']

export function productionMenuItems(): NavItem[] {
  return nav.filter(item => item.group === '生产管理' && !defaultHiddenPaths.includes(item.path))
}
