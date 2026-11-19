import { createRouter, createWebHistory } from 'vue-router'
import { needPhoneGate } from './phoneGate'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/portal', component: () => import('./pages/PortalPage.vue'), meta: { public: true, standalone: true } },
    { path: '/portal-v1', component: () => import('./pages/PortalPageV1.vue'), meta: { public: true, standalone: true } },
    { path: '/trial/home', component: () => import('./pages/TrialHomePage.vue'), meta: { public: true, standalone: true } },
    /* 制衣厂门户：体验用户与注册用户登录后都进入这里（?tenant=厂编码） */
    { path: '/portal/home', component: () => import('./pages/TrialHomePage.vue'), meta: { public: true, standalone: true } },
    { path: '/trial/pending', component: () => import('./pages/TrialPendingPage.vue'), meta: { public: true, standalone: true } },
    { path: '/login', component: () => import('./pages/LoginPage.vue'), meta: { public: true, standalone: true } },
    /* 手机端登录入口：复用登录页短信模式（设计 6.1），避免维护第二套登录表单 */
    { path: '/mobile/login', redirect: { path: '/login', query: { mode: 'sms' } } },
    { path: '/register', component: () => import('./pages/TenantRegisterPage.vue'), meta: { public: true, standalone: true } },
    { path: '/login/matcher', component: () => import('./pages/MatcherLoginPage.vue'), meta: { public: true, standalone: true } },
    { path: '/login/sansheng', component: () => import('./pages/SanshengLoginPage.vue'), meta: { public: true, standalone: true } },
    { path: '/s/:tenant/:code', component: () => import('./pages/ScanLandingPage.vue'), meta: { public: true, standalone: true } },
    { path: '/qr-login', component: () => import('./pages/QrLoginPage.vue'), meta: { public: true, standalone: true } },
    /* 短链：二维码专用，参数 t=ticket、a=模块首字母 */
    { path: '/q', component: () => import('./pages/QrLoginPage.vue'), meta: { public: true, standalone: true } },
    { path: '/platform/login', component: () => import('./pages/PlatformLoginPage.vue'), meta: { public: true, standalone: true } },
    { path: '/change-password', component: () => import('./pages/ChangePasswordPage.vue'), meta: { standalone: true } },
    /* 旧数据租户补录注册手机号（需登录；不校验短信验证码） */
    { path: '/complete-phone', component: () => import('./pages/CompletePhonePage.vue'), meta: { standalone: true } },
    { path: '/platform/tenants', component: () => import('./pages/PlatformAdminPage.vue'), meta: { platform: true, standalone: true } },
    { path: '/platform/modules', component: () => import('./pages/PlatformModulesPage.vue'), meta: { platform: true, standalone: true } },
    { path: '/platform/role-templates', component: () => import('./pages/PlatformRoleTemplatesPage.vue'), meta: { platform: true, standalone: true } },
    { path: '/', redirect: '/portal' },
    { path: '/workbench/A03', component: () => import('./pages/DashboardPage.vue') },
    { path: '/settings/dictionaries', component: () => import('./pages/DictionaryPage.vue') },
    { path: '/masterdata/B06', component: () => import('./pages/MasterdataPage.vue') },
    { path: '/masterdata/customers', component: () => import('./pages/CustomersPage.vue') },
    { path: '/products/B01', component: () => import('./pages/ProductsPage.vue') },
    { path: '/markers/C11', component: () => import('./pages/MarkerPage.vue') },
    { path: '/print/C10', component: () => import('./pages/PrintCenterPage.vue') },
    { path: '/labels/C12', component: () => import('./pages/BundleLabelsPage.vue') },
    { path: '/materials/receipts', component: () => import('./pages/MaterialReceiptsPage.vue') },
    { path: '/materials/master', component: () => import('./pages/MaterialMasterPage.vue') },
    { path: '/rework/E03', component: () => import('./pages/ReworkPage.vue') },
    { path: '/notifications/A04', component: () => import('./pages/NotificationsPage.vue') },
    { path: '/import-export/G09', component: () => import('./pages/ImportExportPage.vue') },
    { path: '/backup/G10', component: () => import('./pages/BackupPage.vue') },
    /* 明文离线备份（设计 3.9；backup:export 权限） */
    { path: '/tenant/backups', component: () => import('./pages/TenantBackupPage.vue') },
    { path: '/audit/G08', component: () => import('./pages/AuditPage.vue') },
    { path: '/products/:id', component: () => import('./pages/ProductDetailPage.vue') },
    { path: '/identity/G05', component: () => import('./pages/IdentityPage.vue') },
    /* 租户管理员：敏感信息查看授权申请审批 */
    { path: '/identity/sensitive-requests', component: () => import('./pages/SensitiveViewRequestsPage.vue') },
    /* 车间/产线/人员管理（设计 6.2-6.4）：/settings/users 为人员与组织（UserListPage），生产组管理独立路径 */
    { path: '/settings/workshops', component: () => import('./pages/WorkshopListPage.vue') },
    { path: '/settings/lines', component: () => import('./pages/LineListPage.vue') },
    { path: '/settings/users', component: () => import('./pages/UserListPage.vue') },
    { path: '/settings/teams', component: () => import('./pages/ProductionTeamPage.vue') },
    /* 本厂信息（设计 6.7）：门户与管理页入口共用 */
    { path: '/tenant/profile', component: () => import('./pages/TenantProfilePage.vue') },
    /* 员工门户与手机岗位首页（设计 6.5/6.6） */
    { path: '/portal/employee', component: () => import('./pages/EmployeePortalPage.vue'), meta: { standalone: true } },
    { path: '/mobile/home', component: () => import('./pages/MobileJobHome.vue'), meta: { standalone: true } },
    { path: '/orders/C03', component: () => import('./pages/OrdersPage.vue') },
    { path: '/tasks/D01', component: () => import('./pages/TasksPage.vue') },
    { path: '/reports/D06', component: () => import('./pages/ReportsPage.vue') },
    { path: '/quality/E01', component: () => import('./pages/QualityPage.vue') },
    { path: '/quality/process-inspections', component: () => import('./pages/ProcessInspectionPage.vue') },
    { path: '/payroll/F09', component: () => import('./pages/PayrollPage.vue') },
    { path: '/mywage/H02', component: () => import('./pages/MyWagesPage.vue') },
    { path: '/myprofile/H03', component: () => import('./pages/MyProfilePage.vue') },
    { path: '/progress/F01', component: () => import('./pages/ProgressPage.vue') },
    /* 外协管理（设计 4.25/6.8） */
    { path: '/outsource/orders', component: () => import('./pages/OutsourcePage.vue') },
    /* 巡厂与证据（设计 4.26/6.9） */
    { path: '/evidence', component: () => import('./pages/EvidencePage.vue') },
    /* 简单考勤（设计 4.27/6.10）：考勤屏常亮展示、员工手机扫码登记、管理查询 */
    { path: '/attendance/screen', component: () => import('./pages/AttendanceScreenPage.vue'), meta: { standalone: true } },
    { path: '/attendance/scan', component: () => import('./pages/AttendanceScanPage.vue'), meta: { standalone: true } },
    { path: '/attendance/records', component: () => import('./pages/AttendanceRecordsPage.vue') },
    { path: '/settings/G01', component: () => import('./pages/SettingsPage.vue') },
    { path: '/portal/settings', component: () => import('./pages/PortalSettingsPage.vue') },
    /* 场景化菜单新增页面（设计 v0.02-002 6.12）：规划新建，均接入真实后端接口 */
    { path: '/intake/new', component: () => import('./pages/IntakeWizardPage.vue') },
    { path: '/cutting/sheets', component: () => import('./pages/CuttingSheetsPage.vue') },
    { path: '/shipments/E04', component: () => import('./pages/ShipmentsPage.vue') },
    { path: '/analysis/salary', component: () => import('./pages/SalaryAnalysisPage.vue') },
    { path: '/process-standards', component: () => import('./pages/ProcessStandardsPage.vue') },
    { path: '/finished-receipts', component: () => import('./pages/FinishedReceiptsPage.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/workbench/A03' },
  ],
})

/* 补录注册手机号期间仍可访问：完善页本身、改密码、营销首页、注册与审批中页面 */
const PHONE_GATE_FREE = new Set(['/complete-phone', '/change-password', '/portal', '/portal-v1', '/register', '/trial/pending'])

router.beforeEach((to) => {
  if (to.meta.platform && !localStorage.getItem('clothp_platform_access_token')) return '/platform/login'
  if (!to.meta.public && !to.meta.platform && !localStorage.getItem('clothp_access_token')) return '/login'
  /* 旧数据租户的厂管理员还没补录注册手机号：先完善资料，再进门户/工作台 */
  if (needPhoneGate() && !to.meta.platform && !to.path.startsWith('/login') && !PHONE_GATE_FREE.has(to.path)) {
    return '/complete-phone'
  }
})

/* 浏览器标签标题：制衣厂门户与工作页面用中性名称，只有平台/营销页面显示产品名 */
router.afterEach((to) => {
  const path = to.path
  let title = '生产管理系统'
  if (path === '/portal' || path === '/portal-v1') title = '一智万商 · 企业 AI 经营平台'
  else if (path.startsWith('/platform')) title = '平台控制面'
  else if (path === '/login/matcher') title = '二撮合 · 登录'
  else if (path === '/login/sansheng') title = '三生万 · 登录'
  else if (path === '/qr-login' || path === '/q') title = '扫码登录'
  document.title = title
})
