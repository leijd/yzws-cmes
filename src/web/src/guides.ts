/**
 * 体验助手引导脚本：选择功能后按步骤打开页面（route）并高亮目标元素（游戏教学式动画箭头）。
 * 定位策略：targetText=按文本找侧边栏菜单项；selector=CSS 选择器（.page-header/.toolbar/.panel 等）；
 * 都找不到时提示框居中显示，不影响引导继续。
 *
 * 权限感知：step.permission = 该功能所需的权限点（与侧边栏导航一致）；
 * 助手会跳过当前账号无权限的步骤，并在引导结束后提示「添加用户并分配权限」或「切换有权限的账号登录」。
 */
export interface GuideStep {
  route?: string
  /** 按文本匹配侧边栏菜单项（如「项目与工单」） */
  targetText?: string
  /** 页面内 CSS 选择器 */
  selector?: string
  /** 该步骤所需权限点（缺权限时跳过并提示） */
  permission?: string
  /** 权限提示中显示的功能名 */
  label?: string
  title: string
  text: string
}

export interface Guide {
  id: string
  name: string
  desc: string
  steps: GuideStep[]
}

/** 权限点中文名（与侧边栏 /me/permissions 的 actions 对应） */
export const permissionNames: Record<string, string> = {
  'workbench:read': '工作台',
  'work_order:read': '项目与工单',
  'task:read': '任务与派工',
  'report:read': '报工与审批',
  'payroll:read': '工资单管理',
  'progress:read': '进度与统计',
  'identity:read': '用户角色权限',
  'settings:read': '功能设置',
}

export const guides: Guide[] = [
  {
    id: 'tour',
    name: '系统总览',
    desc: '快速认识工作台、导航与顶栏',
    steps: [
      { route: '/workbench/A03', permission: 'workbench:read', label: '工作台', title: '工作台', text: '这里是登录后的首页：今日工单、产能与待办一目了然。' },
      { targetText: '工作台', selector: '.sidebar', title: '左侧功能导航', text: '左侧按「生产管理 / 分析中心 / 基础资料 / 系统管理」分组，点击即可进入对应功能。您能看到哪些菜单取决于账号权限。' },
      { selector: '.topbar', title: '顶部状态栏', text: '顶部显示当前用户与中心节点连接状态，点击用户名可退出登录。' },
      { title: '引导完成', text: '点击右下角的悬浮球可以随时打开本助手，按流程一步一步带您操作。' },
    ],
  },
  {
    id: 'order',
    name: '新建工单',
    desc: '从项目与工单创建一张生产工单',
    steps: [
      { route: '/orders/C03', targetText: '项目与工单', permission: 'work_order:read', label: '项目与工单', title: '进入项目与工单', text: '点击左侧「项目与工单」菜单，这里管理订单与生产工单全流程。' },
      { route: '/orders/C03', permission: 'work_order:read', label: '项目与工单', selector: '.page-header', title: '新建工单', text: '点击页面右上角的新建按钮：填写款式、数量与交期后保存。' },
      { route: '/orders/C03', permission: 'work_order:read', label: '项目与工单', selector: '.panel', title: '工单列表', text: '新建的工单会出现在列表中，状态为待投产；后续扫菲报工会自动更新进度。' },
    ],
  },
  {
    id: 'report',
    name: '扫菲报工',
    desc: '任务派工与计件报工',
    steps: [
      { route: '/tasks/D01', targetText: '任务与派工', permission: 'task:read', label: '任务与派工', title: '进入任务与派工', text: '点击左侧「任务与派工」，查看各工序任务与工人的菲票。' },
      { route: '/tasks/D01', permission: 'task:read', label: '任务与派工', selector: '.page-header', title: '派工与报工', text: '在这里把任务派给员工；员工扫菲后计件数据自动汇总。' },
      { route: '/reports/D06', targetText: '报工与审批', permission: 'report:read', label: '报工与审批', title: '报工与审批', text: '报工记录在「报工与审批」中审核，审核通过后计入工资。' },
    ],
  },
  {
    id: 'payroll',
    name: '工资结算',
    desc: '计件工资核算与工资单',
    steps: [
      { route: '/payroll/F09', targetText: '工资单管理', permission: 'payroll:read', label: '工资单管理', title: '进入工资单管理', text: '点击左侧「工资单管理」，系统按审核通过的计件自动核算工资。' },
      { route: '/payroll/F09', permission: 'payroll:read', label: '工资单管理', selector: '.page-header', title: '生成与结算', text: '选择月份生成工资单，核对无误后即可结算发放。' },
      { route: '/mywage/H02', targetText: '我的工资', title: '员工自助查看', text: '员工在「我的工资」中查看自己的计件明细与结算结果。' },
    ],
  },
  {
    id: 'employee',
    name: '员工与账号',
    desc: '维护员工档案、添加用户并分配权限',
    steps: [
      { route: '/masterdata/B06', targetText: '员工与账号', title: '进入员工与账号', text: '点击左侧「员工与账号」，维护员工档案、工序技能与账号。' },
      { route: '/masterdata/B06', selector: '.page-header', title: '新增员工', text: '点击新增：填写姓名、工号与工序技能，保存后即可参与派工与计件。' },
      { route: '/identity/G05', targetText: '用户角色权限', permission: 'identity:read', label: '用户角色权限', title: '分配角色与权限', text: '在「用户角色权限」中为账号分配角色，控制可见页面与操作。' },
    ],
  },
]
