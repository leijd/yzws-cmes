/**
 * 「没有权限」解答知识库（平台管理员 / 制衣厂管理员）
 * 纯前端静态数据：不改动任何业务权限逻辑，供导航中的「权限问答 Bot」检索与展示。
 * 每条包含：现象、原因、解决步骤、相关权限点，便于按现象直接定位并解决问题。
 */
export type Audience = 'platform' | 'tenant' | 'both'

export interface PermissionRef { code: string; name: string }

export interface KnowledgeItem {
  id: string
  audience: Audience
  category: string
  question: string
  keywords: string[]
  symptom: string
  causes: string[]
  steps: string[]
  permissions?: PermissionRef[]
}

export const permissionKnowledge: KnowledgeItem[] = [
  {
    id: 'kb-403-meaning',
    audience: 'both',
    category: '权限基础',
    question: '「没有执行此操作的权限」是什么意思？',
    keywords: ['403', '没有执行此操作的权限', '无权限', 'FORBIDDEN', '报错', '权限不足'],
    symptom: '页面顶部出现红色提示，页面内也有错误条，列表显示「暂无数据」；或点击保存/新增按钮时弹出该提示。',
    causes: [
      '后端每个接口都声明了所需权限码（api.Handle 的第二个参数），当前账号的角色权限集合不含该权限码时返回 403。',
      '页面首屏常一次性请求多个接口（项目、仓库、供应商、员工等下拉数据 + 主列表），其中任意一个接口 403 会让整页加载失败，已取到的数据也会被丢弃。',
      '权限在每次请求时由服务端实时从数据库读取（user_role → role_permission → permission），不是登录时缓存，重新登录通常不解决问题。',
    ],
    steps: [
      '确认右上角当前登录账号，避免用错账号或切错制衣厂。',
      '浏览器按 F12 打开 Network，刷新页面，找到返回 403 的请求，记下其路径（例如 /api/v1/projects）。',
      '按「常见权限点对照」把接口路径对应到权限点与中文权限名。',
      '由管理员在「用户角色权限」中为该账号所属角色勾选权限并保存，然后刷新页面重试。',
      '若账号应为管理员，检查是否绑定了管理员角色、账号状态是否为启用。',
    ],
  },
  {
    id: 'kb-warehouse-required-permissions',
    audience: 'tenant',
    category: '仓库与质检',
    question: '仓管/质检角色进入「物料入库」和「质检与库存」需要哪些权限？',
    keywords: ['物料入库', '质检与库存', '仓管', '质检', '仓库', '库检', '查看客户项目', 'project:read', '必需权限', '权限清单'],
    symptom: '仓管/质检角色进入物料入库或质检与库存时整页提示「没有执行此操作的权限」、列表为空，即使已配置仓库管理或工单权限。',
    causes: [
      '这两个模块首屏都要读取项目下拉（GET /api/v1/projects），该接口要求「查看客户项目」(project:read)。',
      '「查看客户项目」(project:read) 与「查看项目与工单」(work_order:read) 是两个不同权限点，只配后者仍会 403。',
      '页面用 Promise.all 并行加载多个接口，任意一个 403 会导致整页加载失败并清空已取到的数据。',
      '因此仓管/质检角色进入这两个模块都必须具备「查看客户项目」权限。',
    ],
    steps: [
      '制衣厂管理员：「用户角色权限」→ 编辑仓管/质检角色 →「项目与工单」模块勾选「查看客户项目」(project:read) → 保存 → 刷新页面。',
      '同时确认该角色具备模块本身的权限：物料入库 inventory:read（写入库单另需 inventory:receipt）、质检与库存 work_order:read。',
      '确认辅助下拉权限：仓库/物料 warehouse:read、供应商与员工 masterdata:read；缺任意一项都可能出现 403。',
      '平台管理员：在「用户角色权限模板」中为仓管/质检模板预置 project:read，重新应用到租户；新租户初始化角色时即可带上。',
      '仍失败时按 F12 Network 逐个查看 403 请求，对照权限清单补齐。',
    ],
    permissions: [
      { code: 'project:read', name: '查看客户项目（两个模块必需）' },
      { code: 'inventory:read', name: '查看库存与出入库' },
      { code: 'inventory:receipt', name: '物料入库' },
      { code: 'work_order:read', name: '查看项目与工单' },
      { code: 'warehouse:read', name: '查看仓库与物料' },
      { code: 'masterdata:read', name: '查看员工与客户' },
    ],
  },
  {
    id: 'kb-warehouse-page',
    audience: 'tenant',
    category: '仓库与质检',
    question: '已配「查看项目与工单」，为什么物料入库、质检与库存仍提示没有权限？',
    keywords: ['物料入库', '质检与库存', '查看项目与工单', '维护工单', 'work_order:read', '整页报错', '暂无数据'],
    symptom: '角色已勾选「查看项目与工单」「维护工单」以及仓库管理权限，进入物料入库或质检与库存页面仍显示「没有执行此操作的权限」且列表为空。',
    causes: [
      '「查看项目与工单」是 work_order:read，「查看客户项目」是 project:read，两者是不同权限点。',
      '两个页面首屏的项目下拉调用 GET /api/v1/projects，校验 project:read；未勾选该项时该请求 403。',
      'Promise.all 中任意一个请求 403 会导致整个加载失败，页面只显示一条错误消息，主列表数据也被丢弃，看起来像整个模块无权限。',
    ],
    steps: [
      '给仓管/质检角色勾选「查看客户项目」(project:read) 并保存，刷新页面；权限实时加载，无需重新登录。',
      '按「仓管/质检角色进入这两个模块需要哪些权限」逐项核对完整权限清单。',
      '不便开放项目详情时，可改用具备 project:read 的账号操作，或由管理员评估后调整角色范围。',
    ],
    permissions: [
      { code: 'project:read', name: '查看客户项目' },
      { code: 'work_order:read', name: '查看项目与工单' },
      { code: 'inventory:read', name: '查看库存与出入库' },
    ],
  },
  {
    id: 'kb-permission-map',
    audience: 'both',
    category: '权限基础',
    question: '常见权限点与中文名称对照表',
    keywords: ['权限点', '权限码', '对照表', 'project:read', 'inventory:read', 'warehouse:read', 'identity:write'],
    symptom: '知道接口路径或看到权限码，但不知道角色配置界面里应勾选哪一项。',
    causes: [
      '权限码与配置界面中文名称一一对应，按「功能模块 → 权限类型」分组显示。',
      '同一模块内有多个相近名称，例如「查看项目与工单」和「查看客户项目」属于不同权限点，容易漏勾。',
    ],
    steps: [
      '在「用户角色权限」中打开对应角色，按模块查找中文名称，可用检索框搜索。',
      '勾选后点「保存」，再刷新出现问题的页面。',
      '权限点目录来自平台统一目录，制衣厂管理员只能分配，不能新增权限码。',
    ],
    permissions: [
      { code: 'work_order:read', name: '查看项目与工单' },
      { code: 'work_order:write', name: '维护工单' },
      { code: 'project:read', name: '查看客户项目' },
      { code: 'masterdata:read', name: '查看员工与客户' },
      { code: 'warehouse:read', name: '查看仓库与物料' },
      { code: 'warehouse:write', name: '维护仓库与物料' },
      { code: 'inventory:read', name: '查看库存与出入库' },
      { code: 'inventory:receipt', name: '物料入库' },
      { code: 'inventory:issue', name: '物料出库' },
      { code: 'identity:read', name: '查看用户角色权限' },
      { code: 'identity:write', name: '管理用户角色权限' },
    ],
  },
  {
    id: 'kb-role-not-effective',
    audience: 'tenant',
    category: '角色与账号',
    question: '角色权限已经勾选，为什么还是没有权限？',
    keywords: ['角色', '保存', '不生效', '绑定', '员工账号', '重新登录'],
    symptom: '在角色配置里勾选并保存了权限，但该员工账号仍提示没有权限或看不到菜单。',
    causes: [
      '角色权限与「员工账号绑定哪些角色」是两处配置，任一处未保存都不会生效。',
      '勾选的可能是另一个角色，而员工账号绑定的是别的角色。',
      '账号被停用、角色有效期过期，或登录的是另一个制衣厂/另一个账号。',
    ],
    steps: [
      '「员工信息」→ 编辑该员工 → 勾选正确角色 → 点「保存角色与状态」。',
      '「用户角色权限」→ 打开该角色 → 勾选权限 → 点「保存」。',
      '确认账号状态为「启用」、右上角登录账号与制衣厂正确，刷新页面。',
    ],
    permissions: [{ code: 'identity:write', name: '管理用户角色权限' }],
  },
  {
    id: 'kb-menu-hidden',
    audience: 'tenant',
    category: '角色与账号',
    question: '左侧菜单不显示某个模块，是权限问题吗？',
    keywords: ['菜单', '导航', '不显示', '隐藏', '模块'],
    symptom: '直接输入地址能打开页面，但左侧导航里找不到入口，或打开后接口报 403。',
    causes: [
      '左侧菜单按当前账号权限集合过滤，每个菜单项绑定一个 read 类权限码。',
      '菜单权限与页面内各接口权限不完全相同，能进页面不代表页面所需的所有下拉/写入接口都有权限。',
    ],
    steps: [
      '确认角色是否勾选该菜单对应的 read 权限（物料入库 inventory:read、物料管理 warehouse:read）。',
      '补齐页面内接口所需的其它权限点，特别是两个仓库/质检模块的 project:read。',
      '保存角色权限后刷新页面。',
    ],
  },
  {
    id: 'kb-login-verification',
    audience: 'tenant',
    category: '登录与验证',
    question: '登录验证方式相关设置在哪里？首次登录还需要密码吗？',
    keywords: ['登录验证', '短信验证', '密码验证', '微信', '首次登录', '初始密码'],
    symptom: '想为本人或员工开通/关闭额外验证方式，或遇到「该账号已开启密码验证，请输入密码」。',
    causes: [
      '短信验证码是基础验证，恒开且不允许关闭；「密码验证」「微信设备快捷登录」可按账号开关。',
      '默认只开短信，因此首次登录只需手机验证码，不再需要厂管理员提供的初始密码。',
      '只有账号被开通「密码验证」后，短信验证之外才需要输入账号密码。',
    ],
    steps: [
      '本人：「我的资料」→「登录验证」开关密码验证 / 微信设备快捷登录。',
      '制衣厂管理员：「员工信息」可对单个员工点「验证设置」，或勾选员工、或选择全部员工批量开通/关闭。',
      '关闭微信设备快捷登录会同时解绑已绑定设备。',
    ],
  },
  {
    id: 'kb-role-library',
    audience: 'tenant',
    category: '角色与账号',
    question: '从平台角色库补充角色时，为什么有些角色是灰色且不能取消勾选？',
    keywords: ['平台库', '补充角色', '灰色', '不能取消', '已存在', '角色编码'],
    symptom: '「从平台库补充角色」弹窗中，已存在的角色整行置灰、勾选框固定选中且不能取消。',
    causes: [
      '设计行为：平台角色库只用于补充本厂缺少的角色，已有角色的名称与权限保持原样，不允许覆盖或删除。',
      '同厂不允许两个同名或同编码的角色；角色编码由系统自动生成，不需要填写。',
    ],
    steps: [
      '只勾选本厂缺少的角色，点「补充勾选角色」。',
      '需要调整已有角色权限时，回到「用户角色权限」中编辑该角色。',
    ],
  },
  {
    id: 'kb-platform-template-invalid',
    audience: 'platform',
    category: '平台管理',
    question: '平台角色模板保存权限时报 INVALID_PERMISSION',
    keywords: ['平台管理员', '角色模板', 'INVALID_PERMISSION', '权限点', 'platform_module'],
    symptom: '在「用户角色权限模板」中勾选权限后保存失败，提示权限点不存在、已停用或仅限固定租户管理员使用。',
    causes: [
      '保存角色权限与模板权限时，后端按 platform_module 表校验权限点（status=active 且 is_admin_only=FALSE），不只是 platform_permission 目录。',
      '新增权限点若只写入 platform_permission，漏注册 platform_module，配置界面能显示但保存会被拒绝。',
    ],
    steps: [
      '检查「功能模块」页面中该权限点是否已注册且启用、非「仅管理员」。',
      '通过迁移同时补齐 platform_permission（中文名/说明）与 platform_module（菜单/按钮/API 记录）。',
      '补齐后重新保存模板权限；存量租户还需重新应用模板才会带上新权限。',
    ],
  },
  {
    id: 'kb-platform-warehouse-role',
    audience: 'platform',
    category: '平台管理',
    question: '平台侧如何保证仓管/质检角色不会卡在「没有权限」？',
    keywords: ['平台管理员', '仓管', '质检', '角色模板', '查看客户项目', 'project:read', '应用模板'],
    symptom: '新启用租户的仓管/质检员工进入物料入库、质检与库存时整页 403。',
    causes: [
      '这两个模块首屏依赖 GET /api/v1/projects（project:read「查看客户项目」），模板未预置该权限时新角色先天缺少。',
      '模板权限后续更新不会自动同步到已存在的租户角色。',
    ],
    steps: [
      '「用户角色权限模板」→ 编辑仓管/质检模板，在「项目与工单」模块勾选「查看客户项目」(project:read) 并保存。',
      '确认模板同时具备 work_order:read、inventory:read、warehouse:read、masterdata:read 等模块所需权限。',
      '在「应用到租户」中选择目标租户重新应用模板，存量角色才会带上新权限。',
      '指导制衣厂管理员在「员工信息」确认员工已绑定该角色、账号状态为启用。',
    ],
    permissions: [{ code: 'project:read', name: '查看客户项目' }],
  },
  {
    id: 'kb-platform-tenant-permissions',
    audience: 'platform',
    category: '平台管理',
    question: '新启用/已有租户的员工仍提示没有权限或看不到菜单',
    keywords: ['平台管理员', '租户', '应用模板', '初始化角色', '菜单不显示'],
    symptom: '租户审批启用后，员工登录看不到业务菜单，或操作时提示 403。',
    causes: [
      '租户角色由平台角色模板初始化，模板未包含的权限点不会自动授予员工。',
      '模板权限后续更新不会自动同步到已存在的租户角色。',
    ],
    steps: [
      '确认模板权限点齐全（参见仓管/质检模板条目）。',
      '在「应用到租户」中选择目标租户并勾选模板，重新应用以创建/更新角色与授权。',
      '让制衣厂管理员确认员工角色绑定与账号状态。',
      '新增权限点迁移需同时注册权限目录与 platform_module，并按需补齐存量租户 permission 记录。',
    ],
  },
  {
    id: 'kb-diagnose-network',
    audience: 'both',
    category: '权限基础',
    question: '如何快速找到到底是哪个接口没有权限？',
    keywords: ['排查', 'Network', '403', '接口', '定位', 'F12'],
    symptom: '页面只显示「没有执行此操作的权限」，看不出是哪个功能缺权限。',
    causes: ['页面首屏可能并行请求多个接口，前端统一捕获错误后只展示一条消息。'],
    steps: [
      '按 F12 → Network → 勾选 Preserve log，刷新页面。',
      '筛选状态码 403，查看失败请求 URL 与返回 code（FORBIDDEN）。',
      '把 URL 对应到权限点（例如 /api/v1/projects → project:read「查看客户项目」）。',
      '按对照表补勾权限后刷新；若仍 403，继续处理下一个失败请求。',
    ],
  },
  {
    id: 'kb-account-login',
    audience: 'both',
    category: '登录与验证',
    question: '账号密码登录与「密码验证」开关是什么关系？',
    keywords: ['工号登录', '账号密码登录', '密码验证', '短信'],
    symptom: '关闭了「密码验证」，但用工号+密码仍能登录。',
    causes: [
      '「密码验证」是短信登录之外的额外校验开关，只影响短信/体验/微信登录链路。',
      '工号（或手机号）+ 密码的账号登录是独立入口，不受此开关影响。',
    ],
    steps: [
      '如需禁止某人使用密码登录，应停用其账号，而不是关闭「密码验证」。',
      '如需加强短信登录安全性，为账号开通「密码验证」。',
    ],
  },
]

export const quickQuestions = [
  '物料入库需要哪些权限',
  '仓管质检角色配置',
  '已配工单权限仍提示没有权限',
  '常见权限点对照表',
  '平台模板保存报 INVALID_PERMISSION',
  '角色权限已勾选不生效',
]
