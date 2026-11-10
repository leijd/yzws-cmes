# YZWS-MES 详细设计 v0.02

| 项 | 内容 |
|---|---|
| 版本 | v0.02（设计稿，2026-11-10 按代码校对修订） |
| 日期 | 2026-11-08（修订 2026-11-10） |
| 依据 | `req/20261107-1-数据库设计.docx`（厂商部分）、`req/20261107-讨论.txt` |
| 范围 | 厂商（制衣厂）域数据库设计；制衣厂注册登录与注册用户主页、体验用户注册登录与体验用户主页、游客主页的前后端设计 |
| 代码基线 | `src/`（Go 后端）+ `src/web/`（Vue3 前端）；迁移已推进至 `052`，注册审批、体验门户、扫码登录、多厂切换等均已实现（清单见 6.2，待实现见 6.3） |

---

## 1. 系统背景与多租户架构

- 本平台为多租户（SaaS）制衣 MES。**一个制衣厂（tenant）即一个数据隔离单位**。
- **平台运维人员**负责维护系统（版本发布、订阅、线索跟进、租户生命周期），**不参与租户业务**；平台身份不能读写制衣厂业务数据（确需排障须审批并记审计）。
- 制衣厂使用系统的三个前置条件：①签订订阅协议；②交付订阅费；③确认订阅版本与功能。三者齐备才允许从"体验"转为"付费"。
- 客户获取漏斗：**游客 → 潜在客户线索（visitor_lead）→ 免费体验（体验制衣厂）→ 转化成功（正式制衣厂）**，转化状态：好奇 → 感兴趣 → 参与 → 促销 → 成功/失败。

### 1.1 四类身份一览

| 身份 | 载体/存储 | 验证方式 | 登录后主页 | 落库 |
|---|---|---|---|---|
| 平台管理员 | `platform_admin` + `platform_session` | 账密（Argon2id） | `/platform/tenants` | 是 |
| 制衣厂注册用户（付费/体验厂成员） | `user_account` + `auth_session`（JWT） | 账密（unified-login） | `/workbench/A03` | 是 |
| 体验用户（潜在客户，开通体验厂） | 体验制衣厂 `user_account` + 体验会话 | 手机验证码 / 账密 / 扫码 | `/trial/home`（体验主页） | 是（本版起落库） |
| 游客 | 前端本地会话 `clothp_trial_session(via=guest)` | **什么都不验证** | `/trial/home?via=guest`（基础版·数据版） | 否 |

---

## 2. 数据库设计（PostgreSQL）

### 2.1 连接与约定

- 连接地址参考同级 `v-0.01` 项目，**数据库名称：`yzws-v1`**（已在 `src/config.yaml` 中配置）：

```yaml
database:
  host: 106.52.168.135
  port: 18432
  name: yzws-v1
  user: postgres
  password: "******"        # 见 config.yaml
  sslmode: disable
```

- DSN：`postgres://<user>:<pwd>@106.52.168.135:18432/yzws-v1?sslmode=disable`（库名含连字符，URL 路径段合法，无需额外处理）。
- 迁移机制沿用 `internal/database/database.go`：`//go:embed migrations/*.sql`，按文件名字典序在事务中执行，`schema_migration` 表记录 `version/checksum`；`reconcile.go` 启动时对 schema 自修复（缺失的表/列/索引/约束/触发器自动补全）。本设计落地迁移：**`043`–`052`**（清单见附录 A；046–052 为 v0.02 增补）。

设计约定（与 docx 1.2 一致）：

| 项 | 约定 |
|---|---|
| 主键 | TEXT（UUIDv7），应用层生成 |
| 时间 | BIGINT（UTC 微秒）；日期字段（协议起止）用 TEXT `yyyy-MM-dd` |
| 金额/评分 | NUMERIC(20,6) |
| 隔离 | 制衣厂业务表一律含 `tenant_id` 隔离键，唯一索引为 `(tenant_id, 业务键)`，外键为 `(tenant_id, 从表ID)→(tenant_id, 主表ID)` 复合外键 |
| 公共列 | 可变业务表含 created_at/by(/node)、updated_at/by(/node)、version |
| 平台自有表 | 不含 tenant_id，与制衣厂业务数据隔离 |

### 2.2 表总览（本设计 16 张表 + 后续增补 3 张表 + 既有引用表）

| # | 表名 | 中文名 | 归属 | 含 tenant_id | 落库迁移 | 性质 |
|---|---|---|---|---|---|---|
| 1 | `system_release` | 系统版本发布表 | 平台 | 否 | 043 | 新增 |
| 2 | `visitor_lead` | 访客/潜在客户表 | 平台 | 否（引用键） | 043 | 新增 |
| 3 | `subscription_agreement` | 订阅协议表 | 平台 | 引用键 | 043 | 新增 |
| 4 | `subscription_payment` | 订阅费交付表 | 平台 | 引用键 | 043 | 新增 |
| 5 | `tenant_subscription` | 制衣厂订阅状态表 | 平台 | UNIQUE | 043/044 | 新增 |
| 6 | `tenant` | 制衣厂主表（扩展） | 平台主数据 | —（自身即厂） | 001/034/043/044 | 扩展 |
| 7 | `tenant_crypto_key` | 制衣厂数据加密密钥表 | 平台代管 | 是 | 043 | 新增 |
| 8 | `org_unit` | 车间/产线/班组组织表（扩展） | 制衣厂 | 是 | 001/045 | 扩展 |
| 9 | `line_resource` | 生产线资源配置表 | 制衣厂 | 是 | 045 | 新增 |
| 10 | `mgmt_level` | 管理级别表 | 制衣厂 | 是 | 045 | 新增 |
| 11 | `user_profile_template` | 用户资料模板表 | 制衣厂 | 是 | 045 | 新增 |
| 12 | `process_standard` | 生产标准工序表主表 | 制衣厂 | 是 | 045 | 新增 |
| 13 | `process_standard_line` | 生产标准工序表明细 | 制衣厂 | 是 | 045 | 新增 |
| 14 | `process_standard_confirm` | 工序表两确认记录表 | 制衣厂 | 是 | 045 | 新增 |
| 15 | `process_standard_grant` | 工序表查看授权表 | 制衣厂 | 是 | 045 | 新增 |
| 16 | `capacity_profile` / `ai_profile` | 产能评估表 / AI定制表 | 制衣厂 | 是 | —（预留） | **本期不建表**（见 2.10） |
| 17 | `trial_extension` | 体验延期申请表 | 平台（体验厂引用） | 是 | 047 | 新增 |
| 18 | `tenant_portal` | 制衣厂门户内容表 | 制衣厂 | 是（tenant_id 唯一） | 048/050/052 | 新增 |
| 19 | `platform_admin_permission` | 平台辅助人员权限表 | 平台 | 否 | 046 | 新增 |

列级扩展（随 6.2 增补功能落地）：`tenant.expedite_requested_at`（047 加急标记）；`auth_session.login_phone`（049 登录手机号，多厂切换）；`tenant_portal.logo_data/logo_content_type`（050 logo 存库）；`tenant.contact_wechat/contact_qq/contact_alipay`（051 辅助联系方式）；`tenant_portal.system_name`（052 系统名称）。

既有引用表（沿用，见 2.9）：`employee`、`user_account`、`role`、`user_role`、`role_permission`、`user_scope`、`auth_session`、`customer`、`project`、`process_definition`、`feature_setting`、平台自有表（`platform_admin`/`platform_session`/`platform_setting`/`platform_audit_log`/`platform_module`/`platform_role_template`）。

> 以下字段字典为**完整口径**，与迁移脚本一致；与原 docx 草案的差异在"落地说明"中显式标注。除特别注明外，各表均含公共列：`created_at/by(/node)、updated_at/by(/node)、version`（BIGINT 微秒 / TEXT / BIGINT 乐观锁）。

### 2.3 平台运营表（5 张，平台管理员维护，制衣厂用户不可见）

#### 2.3.1 system_release — 系统版本发布表

系统名、版本、订阅客户说明、链接同表；订阅协议与订阅状态通过 `version_no` 逻辑引用本表。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | TEXT | PK | 版本记录 ID（UUIDv7） |
| system_name | TEXT | NOT NULL | 系统名称 |
| version_no | TEXT | NOT NULL, UNIQUE(uk_release_version) | 版本号（语义化，如 v0.0.4.2），全局唯一 |
| channel | TEXT | NOT NULL, CHECK(stable/beta) | 版本渠道：稳定版/体验版 |
| release_notes | TEXT |  | 订阅客户说明：功能变更、修复与升级须知 |
| download_link | TEXT |  | 安装包/发行包下载链接 |
| apk_link | TEXT |  | 移动端 APK 下载链接 |
| doc_link | TEXT |  | 操作手册/文档链接 |
| publish_link | TEXT |  | 对外发布页链接（主页"吸引潜在客户"入口落地页） |
| status | TEXT | CHECK(draft/published/deprecated) | 草稿/已发布/已废弃 |
| published_at | BIGINT |  | 发布时间 |
| remark | TEXT |  | 内部备注 |
| created_at/by、version |  | NOT NULL | 公共列（无 node 列） |

版本引用不做物理外键：版本废弃后历史协议/订阅记录仍可追溯。

#### 2.3.2 visitor_lead — 访客/潜在客户表

管理走访、电话、制衣车推荐、用户裂变、线上自助等渠道线索；支撑五状态转化漏斗与免费体验开通；主页转化入口直接写入本表。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | TEXT | PK | 线索 ID |
| lead_no | TEXT | NOT NULL, UNIQUE(uk_lead_no) | 线索编号：XK+年月日+三位流水（如 XK20261107001） |
| factory_name | TEXT | NOT NULL | 潜在制衣厂名称 |
| contact_name | TEXT |  | 联系人姓名 |
| contact_phone | TEXT |  | 联系电话（平台资产） |
| address | TEXT |  | 厂地址 |
| source | TEXT | NOT NULL DEFAULT 'online', CHECK(visit/phone/referral/fission/online) | 来源：走访/电话拜访/制衣车推荐/用户裂变/线上自助 |
| referrer_lead_id | TEXT | FK→visitor_lead.id | 推荐人线索 ID（裂变渠道，可空） |
| referrer_tenant_id | TEXT | FK→tenant.id | 推荐人所在制衣厂（老带新激励，可空） |
| wechat_unionid | TEXT |  | 微信开放标识 |
| intent_status | TEXT | NOT NULL DEFAULT 'curious', CHECK(curious/interested/engaged/promotion/won/lost) | 转化漏斗：好奇→感兴趣→参与→促销→成功/失败；只前进不回退 |
| lost_reason | TEXT |  | 失败原因；**CHECK(intent_status<>'lost' OR lost_reason IS NOT NULL)** |
| trial_tenant_id | TEXT | FK→tenant.id | 免费体验开通的体验制衣厂；未开通为空 |
| trial_started_at | BIGINT |  | 体验开通时间 |
| trial_expires_at | BIGINT |  | 体验到期时间 |
| assigned_admin_id | TEXT |  | 平台跟进管理员 |
| last_follow_at | BIGINT |  | 最近跟进时间 |
| follow_remark | TEXT |  | 跟进记录摘要 |
| converted_tenant_id | TEXT | FK→tenant.id | 转化成功后创建的正式制衣厂 ID（won 时回填） |
| remark | TEXT |  | 备注 |
| 公共列 |  |  | created_at/by、updated_at/by、version（无 node 列） |

索引：`ix_lead_status(intent_status,last_follow_at)`、`ix_lead_source(source,created_at)`、`ix_lead_referrer(referrer_tenant_id)`。

#### 2.3.3 subscription_agreement — 订阅协议表（使用条件①）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | TEXT | PK | 协议 ID |
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（引用键） |
| agreement_no | TEXT | NOT NULL, UNIQUE(uk_agreement_no) | 协议编号：XY+年月+三位流水 |
| version_no | TEXT | NOT NULL | 订阅的系统版本（引用 system_release.version_no） |
| channel | TEXT | DEFAULT 'formal', CHECK(trial/formal) | 体验协议/正式订阅协议 |
| signed_at | BIGINT |  | 签订时间 |
| start_date / end_date | TEXT | NOT NULL | 订阅起止日期（yyyy-MM-dd）；到期未续签则订阅状态转到期 |
| terms | JSONB |  | 协议条款快照：功能清单、账号数、价格、保密与数据条款 |
| attachment_id | TEXT |  | 协议扫描件附件 ID |
| status | TEXT | DEFAULT 'draft', CHECK(draft/signed/expired/terminated) | 待签订/生效中/到期/提前终止 |
| terminated_reason | TEXT |  | 提前终止原因 |
| remark | TEXT |  | 备注 |

索引：`ix_agreement_tenant(tenant_id,status,start_date,end_date)`；同一厂有效协议区间不重叠由应用事务保证。

#### 2.3.4 subscription_payment — 订阅费交付表（使用条件②）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | TEXT | PK | 付款记录 ID |
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（引用键） |
| agreement_id | TEXT | NOT NULL, FK→subscription_agreement.id | 对应订阅协议 |
| amount | NUMERIC(20,6) | NOT NULL, CHECK(amount>=0) | 订阅费金额 |
| currency | TEXT | NOT NULL DEFAULT 'CNY' | 币种 |
| period_start / period_end | TEXT | NOT NULL | 费用覆盖的订阅周期起止 |
| pay_method | TEXT |  | 收款方式：对公转账/微信/支付宝/现金 |
| paid_at | BIGINT |  | 实际到账时间 |
| voucher_attachment_id | TEXT |  | 收款凭证附件 ID |
| status | TEXT | DEFAULT 'pending', CHECK(pending/confirmed/overdue/refunded) | 待付/已确认到账/逾期/已退款 |
| confirmed_by | TEXT |  | 确认到账的平台管理员 |
| confirmed_at | BIGINT |  | 确认时间 |
| remark | TEXT |  | 备注 |

索引：`ix_payment_tenant(tenant_id,status,paid_at)`、`ix_payment_agreement(agreement_id)`。规则：pending→confirmed 单向流转、confirmed 后禁止修改金额（应用事务保证）。

#### 2.3.5 tenant_subscription — 制衣厂订阅状态表（使用条件③，一厂一行）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | TEXT | PK | 订阅状态 ID |
| tenant_id | TEXT | NOT NULL, UNIQUE(uk_subscription_tenant), FK→tenant.id | 制衣厂ID（一厂一行） |
| agreement_id | TEXT | FK→subscription_agreement.id | 当前生效协议；**可空**（平台线上审批启用时可后补书面协议，迁移 044 放宽） |
| edition | TEXT | NOT NULL, CHECK(basic/standard/pro) | 订阅版本：基础版/标准版/专业版 |
| **user_limit** | INTEGER | CHECK(user_limit IS NULL OR user_limit>0) | **订阅规模：本厂"启用用户"数量上限（NULL 不限）**；由后端在创建/启用用户、员工开通账号时强制校验 |
| features | JSONB | NOT NULL DEFAULT '[]' | 确认订阅的功能清单（模块编码数组）；体验厂按体验进度点亮（体验到=1，未到=0） |
| customer_type | TEXT | DEFAULT 'trial', CHECK(trial/paid) | 客户类型（与 tenant.customer_type 一致） |
| status | TEXT | DEFAULT 'trial', CHECK(trial/active/expired/suspended) | 体验中/付费生效/到期/停用 |
| health_status | TEXT | CHECK(NULL/activated/stable/active_user) | 付费使用状态：激活→稳定→积极（本期预留） |
| trial_ends_at | BIGINT |  | 体验到期时间 |
| activated_at | BIGINT |  | 付费激活时间 |
| expires_at | BIGINT |  | 订阅到期时间 |
| features_confirmed_by | TEXT |  | 功能确认人（厂长/经理或平台管理员） |
| features_confirmed_at | BIGINT |  | 功能确认时间（条件③） |
| last_evaluated_at | BIGINT |  | 最近使用状态评估时间 |
| remark | TEXT |  | 备注 |

索引：`ix_subscription_status(customer_type,status,expires_at)`（到期扫描）。生效规则（事务保证）：trial→active 须 ①signed 协议 ②confirmed 付款 ③features 已确认；到期由后台任务置 expired。**对 docx 偏离**：`agreement_id` 放宽为可空（线上自助注册→平台审批的流程先激活订阅、协议后补）。

### 2.4 制衣厂主表与加密设计

#### 2.4.1 tenant — 制衣厂主表

全部制衣厂业务数据的隔离根。一级字段：编码、名称、客户类型、状态、二维码、管理账户、注册时间、备注；辅助信息：地址、联系方式。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | TEXT | PK | 制衣厂ID（UUIDv7），被全部业务表引用 |
| code | TEXT | NOT NULL, UNIQUE | 制衣厂编码，全平台唯一 |
| short_code | TEXT | NOT NULL, UNIQUE（迁移 034） | 5 位短码：二维码内容与工号前缀（如 ABC01-000001） |
| name | TEXT | NOT NULL, UNIQUE | 制衣厂名称，全平台唯一 |
| timezone | TEXT | NOT NULL DEFAULT 'Asia/Shanghai' | 时区（001 基础列） |
| customer_type | TEXT | NOT NULL DEFAULT 'trial', CHECK(trial/paid) | 客户类型：体验/付费（迁移 043）；以 tenant_subscription 为准同步 |
| status | TEXT | NOT NULL, CHECK(active/disabled) | 启用/停用（001 基础列） |
| **registration_status** | TEXT | NOT NULL DEFAULT 'approved', CHECK(pending/approved/rejected) | **注册审批状态（迁移 044）**：pending 待平台确认（无登录账号）/ approved 已启用 / rejected 已驳回 |
| qr_code_content | TEXT | UNIQUE（部分索引） | 唯一二维码内容：短码+HMAC 签名（应用层生成），一厂一码 |
| admin_login_name | TEXT | UNIQUE（部分索引） | 厂管理账户登录名；**允许为空**（避免存量迁移失败与双写漂移，认证链路仍走 user_account） |
| admin_password_verifier | TEXT |  | 厂管理账户密码 Argon2id 验证器快照，绝不明文 |
| admin_employee_id | TEXT |  | 厂管理账户关联员工档案（可空） |
| **approved_at** | BIGINT |  | 平台管理员确认启用时间（迁移 044） |
| **approved_by** | TEXT |  | 确认启用的平台管理员（迁移 044） |
| **expedite_requested_at** | BIGINT |  | 体验注册加急申请时间（迁移 047；同一厂一次，审批/驳回后清除） |
| registered_at | BIGINT |  | 注册时间（回填 created_at） |
| contact_name | TEXT |  | 辅助信息：联系人 |
| contact_phone | TEXT |  | 辅助信息：联系电话（设计为加密存储、脱敏展示；**本期明文存储**，落地状态见 2.4.3） |
| contact_wechat / contact_qq / contact_alipay | TEXT |  | 辅助联系方式：微信/QQ/支付宝（迁移 051，注册与审批联系用） |
| address | TEXT |  | 辅助信息：厂地址 |
| region | TEXT |  | 所在国/地区（区域统计与收费地区匹配） |
| scale | TEXT | CHECK(NULL/micro/small/medium/large) | 厂规模；micro/small 走动态产线配置 |
| settings | JSONB | NOT NULL DEFAULT '{}' | 编号、精度、离线等非敏感参数（001 基础列） |
| remark | TEXT |  | 备注 |
| created_at、version |  | NOT NULL | 公共列（001 基础列，无 node 列） |

索引：`uk_tenant_code(code)`、`uk_tenant_qr(qr_code_content)`、`uk_tenant_admin_login(admin_login_name)`（均为唯一/部分唯一索引）。

#### 2.4.2 tenant_crypto_key — 制衣厂数据加密密钥表

每厂一个数据加密主密钥（DEK），被平台 KMS 主密钥包装后落库，数据库不出现明文密钥；密钥只用于本厂数据。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| id | TEXT | PK | 密钥记录 ID |
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（密钥与厂一一对应，禁止跨厂使用） |
| key_version | BIGINT | NOT NULL, UNIQUE(tenant_id,key_version) | 密钥版本号，每厂从 1 递增 |
| algorithm | TEXT | NOT NULL DEFAULT 'AES-256-GCM' | 加密算法标识 |
| wrapped_key | TEXT | NOT NULL | 被 KMS 包装后的 DEK（Base64），禁止明文入库 |
| key_wrap_ref | TEXT | NOT NULL | 包装所用 KMS 主密钥引用（unwrap 与轮换审计用） |
| status | TEXT | DEFAULT 'active', CHECK(active/rotated/revoked) | 当前生效/已轮换/已吊销 |
| activated_at | BIGINT | NOT NULL | 生效时间 |
| rotated_at | BIGINT |  | 轮换时间 |
| created_by / created_at | TEXT / BIGINT |  | 创建/轮换操作人、时间 |

索引：`ix_tenant_key_active(tenant_id,status)`。每厂同时最多一个 active（事务保证）。**本期实现**：无独立 KMS 时以本地主密钥（由 JWTSecret 派生）AES-256-GCM 包装，`key_wrap_ref='local-master:v1'`；接入 KMS 后仅替换包装实现，表结构不变。

#### 2.4.3 关键信息加密字段范围

| 表（归属） | 加密字段 | 说明 |
|---|---|---|
| customer（制衣厂） | contact_name、phone、address | 制衣厂客户信息属商业秘密 |
| project（制衣厂） | customer_unit_price 等商务字段 | 款号、数量等生产信息不加密 |
| employee（制衣厂） | phone、id_card_no、address、bank_card、bank_name、emergency_contact、emergency_phone、birth_date、nation、native_place | 工号 employee_no、姓名 name、性别 gender 不加密 |
| process_standard / process_standard_line（制衣厂） | line 级全部业务字段 | 工序表是厂的技术内核，整行加密 |
| ~~registration_request（制衣厂）~~ | — | **本表未建**：注册申请信息直接落 `tenant`（registration_status='pending'），无独立注册申请表 |
| user_account / tenant | password_verifier 为 Argon2id 验证器 | 密码永不落明文 |

实现方式：字段级 AES-256-GCM（每厂 DEK），密文以 `enc:` 前缀区分；解密仅服务端按权限点执行；列表默认脱敏（138****5678、证件/银行卡掩码），明细查看记审计日志。

**落地状态（2026-11-10 校对）**：本期仅完成 `tenant_crypto_key` 的 DEK 生成与包装落库（平台审批通过时，`ensureTenantCryptoKey`）；**字段级加密尚未落地**——代码中无 `enc:` 密文读写，employee.phone、tenant.contact_phone 等敏感字段当前为明文存储，脱敏展示未实现。上表为设计目标口径，实现时按本节方案补齐。

### 2.5 组织与人员域表（制衣厂业务数据，含 tenant_id 隔离键）

#### 2.5.1 org_unit — 车间/产线/班组组织表（001 建表 + 045 扩展）

层级：制衣厂 → 车间（含外包）→ 生产线 → 班组 → 人员（employee）。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（隔离键） |
| id | TEXT | PK(tenant_id,id) | 组织 ID |
| parent_id | TEXT |  | 父组织，同厂；根为厂本身 |
| org_type | TEXT | NOT NULL, CHECK(department/workshop/line/team) | 部门/车间/生产线/班组 |
| code / name | TEXT | NOT NULL, UNIQUE(tenant_id,code) | 组织编码/名称，同厂唯一 |
| manager_employee_id | TEXT |  | 负责人（车间主任/线长/组长） |
| path | TEXT | NOT NULL | 物化路径（禁止直接修改，组织移动走事务） |
| **workshop_nature** | TEXT | NOT NULL DEFAULT 'own', CHECK(own/outsourced) | 仅 workshop：自有车间/外包车间（045） |
| **outsourced_partner** | TEXT |  | 外包方名称（外包车间必填）（045） |
| **line_nature** | TEXT | NOT NULL DEFAULT 'fixed', CHECK(fixed/dynamic) | 仅 line：固定产线/动态产线（小微厂按订单/工序表动态配置）（045） |
| **established_at** | BIGINT |  | 车间/产线设立时间（045） |
| **capacity_note** | TEXT |  | 产能说明（正式产能评估见预留表）（045） |
| status | TEXT | DEFAULT 'active' | 启停用 |
| 公共列 |  |  | 含 created_node/updated_node |

索引：`uk_org(tenant_id,code)`；规则：line 的父级必须为同厂 workshop（应用事务保证）；dynamic 产线的人员/设备/技术配置由 line_resource 按工单动态生成。

#### 2.5.2 line_resource — 生产线资源配置表（045）

一条生产线至少一组对应的人员、设备、生产技术（工序表+质量表+货期完成情况表）。固定产线长期配置；小微厂（scale=micro/small）动态配置——按订单/工序表决定每条线的人员设备，一单一配。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（隔离键） |
| id | TEXT | PK(tenant_id,id) | 配置 ID |
| line_org_id | TEXT | NOT NULL, FK→org_unit(tenant_id,id) | 所属生产线（org_type=line，同厂） |
| config_type | TEXT | DEFAULT 'fixed', CHECK(fixed/dynamic) | 固定配置/按订单动态配置 |
| work_order_id | TEXT |  | 动态配置关联的工单（dynamic 必填，fixed 为空） |
| manager_employee_id | TEXT |  | 管理人员（线长/组长） |
| member_employee_ids | JSONB | DEFAULT '[]' | 生产人员清单（employee ID 数组，含线外工、后整人员归属） |
| equipment_ids | JSONB | DEFAULT '[]' | 设备清单（编号数组；设备台账随设备管理模块另建） |
| process_standard_id | TEXT |  | 生产技术·工序表：使用的生产标准工序表 |
| quality_ref | JSONB | DEFAULT '{}' | 生产技术·质量表：质检/抽查记录来源快照 |
| delivery_ref | JSONB | DEFAULT '{}' | 生产技术·货期完成情况表：计划 vs 实际快照 |
| valid_from / valid_to | BIGINT |  | 配置生效区间；dynamic 随订单完工失效 |
| status | TEXT | DEFAULT 'active', CHECK(active/expired) | 生效中/已失效 |
| remark | TEXT |  | 备注 |

索引：`ix_line_resource_line(tenant_id,line_org_id,status)`、`ix_line_resource_order(tenant_id,work_order_id)`。同一产线同一时间只有一个 active 配置（事务保证）。

#### 2.5.3 mgmt_level — 管理级别表（045）

角色之外的纵向层级（L1 厂级→L4 个人级）：管理级别决定"能看多广"（数据范围），角色决定"能做什么"（权限点）。四级标准数据在两处预置：迁移 045 为存量厂补种；`auth.SeedTenant` 为新厂开通时自动创建。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（级别定义各厂独立） |
| id | TEXT | PK(tenant_id,id) | 级别 ID |
| level_code | TEXT | NOT NULL, UNIQUE(tenant_id,level_code) | L1_FACTORY / L2_WORKSHOP / L3_LINE / L4_SELF |
| level_name | TEXT | NOT NULL | 厂级/车间级/产线级/个人级 |
| description | TEXT |  | 该级别的默认数据范围 |
| sort_no | INTEGER | NOT NULL DEFAULT 0 | 排序，越小级别越高 |
| status | TEXT | DEFAULT 'active', CHECK(active/disabled) | 启停用 |

授权计算顺序：先按角色权限点过滤功能，再按管理级别 + `user_scope` 数据范围过滤数据（组织/字段级，deny 优先）。`user_account` 新增列 **`mgmt_level_id`**（FK→mgmt_level(tenant_id,id)，可空，空=仅按角色权限）。

#### 2.5.4 user_profile_template — 用户资料模板表（045）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（隔离键） |
| id | TEXT | PK(tenant_id,id) | 模板 ID |
| template_type | TEXT | NOT NULL, CHECK(standard/custom), UNIQUE(tenant_id,template_type) | 标准板（开通时全用户一次全自动设定）/ 自定义模板（厂管理账户修改保存） |
| name | TEXT | NOT NULL | 模板名称 |
| fields | JSONB | NOT NULL DEFAULT '[]' | 字段定义数组 `[{field_code,field_name,required,encrypted,visible_level,dict_type,sort_no,remark}]`；field_code 对应 employee 列或扩展字段；visible_level 引用 mgmt_level |
| is_active | BOOLEAN | NOT NULL DEFAULT TRUE | 是否生效；标准板停用后回退系统内置默认定义 |
| applied_count | INTEGER | NOT NULL DEFAULT 0 | 已应用人数 |
| remark | TEXT |  | 备注 |

应用规则：开通时自动将 standard 应用到全厂用户（事务内逐人生成扩展字段快照）；encrypted=true 字段写入前按 2.4 密钥加密。

### 2.6 工序表域（技术内核，制衣厂业务数据）

#### 2.6.0 process_definition — 公共工序库表（001 建表，沿用）

全厂共享的标准工序主数据，是生产标准工序表的取数来源：`tenant_id`、`id`、`process_code`（同厂唯一）、`name`、`default_unit_price`、`default_report_ratio`、`default_department_id`、`standard_work_seconds`、`defect_config`、`equipment`、`work_instruction`（027 增）、`process_category`（027 增：车缝/锁钉/后整/包装）、`status`。对 docx 命名偏离：列名为 `process_code`/`default_unit_price`（沿用既有实现），语义一致。

#### 2.6.1 process_standard — 生产标准工序表主表（045）

生命周期：draft 草稿 → pending_confirm 待线长确认 → standard 生产标准 ⇄ revising 修订中（重新两确认）→ superseded 被新版本替代。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id | TEXT | NOT NULL, FK→tenant.id | 制衣厂ID（隔离键） |
| id | TEXT | PK(tenant_id,id) | 工序表 ID |
| sheet_no | TEXT | NOT NULL, UNIQUE(tenant_id,sheet_no) | 工序表编号：GZB+年月+三位流水 |
| product_ref | TEXT |  | 关联款式（款号+名称，做模板时按款式复用） |
| project_ref | TEXT |  | 关联项目/订单编号（可空） |
| version_no | BIGINT | NOT NULL DEFAULT 1 | 版本号，同款式递增 |
| revision | BIGINT | NOT NULL DEFAULT 0 | 修订次：成为标准后每次修改递增 |
| status | TEXT | DEFAULT 'draft', CHECK(draft/pending_confirm/standard/revising/superseded) | 状态单向流转；同厂同款式/项目同时最多一张 standard（事务保证） |
| created_by | TEXT | NOT NULL | 制订人（工艺员 user_account.id） |
| copied_from_id | TEXT | FK→process_standard(tenant_id,id) | 模板来源（允许拿旧款标准表增删改后另存，禁止直接拷贝） |
| confirmed_round | INTEGER | NOT NULL DEFAULT 0 | 已完成确认轮次（每轮两确认） |
| effective_at | BIGINT |  | 成为生产标准的时间 |
| superseded_by | TEXT | FK→process_standard(tenant_id,id) | 替代本表的新表 ID |
| remark | TEXT |  | 备注 |

索引：`ix_sheet_product(tenant_id,product_ref,status)`、`ix_sheet_project(tenant_id,project_ref)`。

#### 2.6.2 process_standard_line — 明细（045）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id / id | TEXT | PK(tenant_id,id) | 隔离键 / 明细行 ID |
| sheet_id | TEXT | NOT NULL, FK→process_standard(tenant_id,id) | 所属工序表（同厂） |
| seq_no | INTEGER | NOT NULL, UNIQUE(tenant_id,sheet_id,seq_no) | 工序序号：同表内唯一、不断号（触发器/事务保证） |
| process_id | TEXT | FK→process_definition(tenant_id,id) | 公共工序库工序（引用，不复制库数据） |
| process_code / process_name | TEXT | NOT NULL | 工序编码/名称快照（**加密存储**） |
| category | TEXT |  | 工序大类：车缝/锁钉/后整/包装（加密存储） |
| work_seconds | NUMERIC(20,6) |  | 标准工时秒（加密存储） |
| unit_price | NUMERIC(20,6) |  | 工序单价 元/件（加密存储） |
| equipment / instruction / remark | TEXT |  | 设备/作业说明/备注（加密存储） |

索引：`ix_sheet_line_process(tenant_id,process_id)`。standard 状态明细行禁止直接改，修改须整体升版（revision+1）并重新两确认。

#### 2.6.3 process_standard_confirm — 两确认记录表（045）

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id / id | TEXT | PK(tenant_id,id) | 隔离键 / 记录 ID |
| sheet_id | TEXT | NOT NULL, FK→process_standard(tenant_id,id) | 工序表（同厂） |
| revision | BIGINT | NOT NULL | 对应修订次 |
| round_no | INTEGER | NOT NULL | 确认轮次：1 首次制订，2 起为第 N 次修改 |
| confirm_seq | INTEGER | NOT NULL, CHECK(1/2) | 1 工艺员提交 / 2 线长确认 |
| confirmer_id | TEXT | NOT NULL | 确认人（user_account.id） |
| confirmer_role | TEXT | NOT NULL, CHECK(process_engineer/group_leader) | 工艺员/线长 |
| action | TEXT | NOT NULL, CHECK(submit/confirm/reject) | 提交/通过/驳回 |
| opinion | TEXT |  | 确认意见或驳回原因（驳回必填） |
| created_at | BIGINT | NOT NULL | 操作时间 |

索引：`ix_confirm_sheet(tenant_id,sheet_id,revision,round_no,confirm_seq)`。seq=2 confirm 通过时事务内置 status=standard、effective_at、confirmed_round+1；reject 退回 draft/revising。

#### 2.6.4 process_standard_grant — 查看授权表（045）

工艺员和线长只能查看自己指定（制订/确认）的工序表；查看他人指定的须厂长/经理授权（只读、按人按表按有效期）。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| tenant_id / id | TEXT | PK(tenant_id,id) | 隔离键 / 授权 ID |
| sheet_id | TEXT | NOT NULL, FK→process_standard(tenant_id,id) | 被授权的工序表 |
| grantee_user_id | TEXT | NOT NULL, FK→user_account(tenant_id,id) | 被授权人 |
| grantee_role | TEXT |  | 被授权人角色（冗余，便于审计） |
| granted_by | TEXT | NOT NULL, FK→user_account(tenant_id,id) | 授权人：仅厂长/经理（事务保证） |
| scope | TEXT | DEFAULT 'view', CHECK(view) | 授权范围：本期仅只读 |
| valid_from / valid_to | BIGINT |  | 有效期；空表示长期 |
| reason | TEXT |  | 授权事由 |
| revoked_at / revoked_by | BIGINT / TEXT |  | 撤销时间/人 |
| created_at | BIGINT | NOT NULL | 授权时间 |

索引：`ix_grant_sheet`、`ix_grant_user`。访问判定顺序：本人经手 > 有效授权 > 拒绝。

### 2.7 数据归属与隔离

- 全部数据分两类归属：**平台运营数据**（system_release/visitor_lead/subscription_*/平台自有表，平台管理员维护）与**制衣厂业务数据**（组织/人员/客户/订单/工序表等，厂内用户维护）。
- 每张制衣厂业务表必须含 `tenant_id` 隔离键；所有查询/更新/删除带制衣厂ID条件，由数据访问层统一强制；唯一约束一律 `(tenant_id, 业务键)`；外键一律 `(tenant_id, 从表ID)→(tenant_id, 主表ID)` 复合外键，数据库层阻止跨厂引用。
- 订阅协议/订阅费/订阅状态含 tenant_id 仅作"哪个厂"的引用键，由平台侧写入，制衣厂用户不可读写。
- 访客免费体验的业务数据存放在平台开通的**体验制衣厂（trial）**内，与正式厂同规则隔离；体验转正后该厂数据整体保留转正式。
- 平台管理员后台只读订阅状态类汇总；排障读取须审批并记 `platform_audit_log`。
- 边缘/离线节点只同步本厂数据，同步游标与单号段按厂隔离（`number_allocation`、`ownership_lease` 已有机制）。

### 2.8 角色与数据可见性基线

角色清单落平台角色模板/厂内角色体系（`platform_role_template` + 厂内 `role`，不新增表）：

| 角色编码 | 名称 | 数据范围（管理级别） | 主要权限 |
|---|---|---|---|
| factory_head | 厂长 | L1 全厂 | 全部查询与授权；订单客户信息可见；工序表查看授权人 |
| manager | 经理 | L1 全厂 | 同厂长，与厂长互为 AB 角 |
| workshop_director | 车间主任 | L2 本车间 | 本车间订单/产线/人员/设备/工序进度；不可见订单客户联系人 |
| group_leader | 组长 | L3 本线 | 承接订单进度、工序表（限授权/本人经手）、线检/终查/出库/任务表 |
| line_inspector | 线检员 | L3 本线 | 只读生产信息；填报工序质量检查、抽查表 |
| final_inspector | 终查员 | L3 本线 | 只读生产信息；填报部位/整件检查、抽查表 |
| offline_process_leader | 线外工负责人 | L3 本线 | 扫粉/定位/熨烫/包夹/开袋/贴袋/锁边等线外工序；只读本线进度 |
| finishing_leader | 后整负责人 | L3 本线 | 剪线/打扣/打钉/穿绳/打枣等后整工序；只读本线进度 |
| warehouse_keeper | 仓管员 | L2 本车间仓库 | 本车间仓储物流、出入库 |
| process_engineer | 工艺员 | L2 本车间 | 样板/制单/裁床单/货期/物料/设备；制订工序表草稿 |
| sewer | 车工 | L4 本人 | 扫码报工、个人工资明细 |
| statistician | 统计员 | L2 本车间 | 产量/工时/进度统计查询 |

字段级可见性（订单客户信息、厂家师傅信息、工资数据、员工敏感字段）通过 角色权限点 + 管理级别 + `user_scope`（allow/deny，deny 优先）三层组合实现；tenant_admin（固定管理员）仅负责用户/角色/权限管理，不参与业务数据。

### 2.9 既有引用表速览（沿用）

| 表 | 关键字段与说明 |
|---|---|
| employee | tenant_id；employee_no 工号（同厂唯一，登录账号即工号）；name/gender 明文；phone/id_card_no/address/bank_card/bank_name/emergency_*/birth_date/nation/native_place 加密；org_unit_id 同厂 FK；position/skill_grade/pay_mode 等 |
| user_account | tenant_id；employee_id 同厂 FK；login_name 同厂唯一；password_verifier Argon2id；wechat_unionid；status active/disabled/locked；password_change_required（首登强制改密）；offline_policy；last_login_at；**mgmt_level_id（045 增）** |
| role / user_role / role_permission | 角色编码同厂唯一；is_system 固定管理员标记；用户-角色-权限点关系（含生效区间） |
| user_scope | user_id；scope_type org/warehouse/self/field；scope_value；effect allow/deny（deny 优先） |
| auth_session | 刷新令牌哈希、权限快照版本、offline_expires_at（离线策略载体） |
| customer / project | 客户资料与订单项目主表（项目编号 QD+日期+流水、款号/款式分列、来料数/客户单价/货期/负责人等） |
| feature_setting | 厂内业务参数（报工自动审批、工序校验等，"功能设置"页维护） |

### 2.10 预留表设计（本期不建表，仅锁定口径）

#### capacity_profile — 产能评估表（预留）

制衣厂/车间/生产线/人员四级产能评估：生产标的、规模、质量、货期信誉、灵活能力构成评价标准，逐级分解/汇总。字段：`tenant_id`、`id`、`target_type`(factory/workshop/line/employee)、`target_id`、`period`（如 2026Q4）、`production_target`、`scale_score`、`quality_score`、`delivery_credit`、`flexibility_score`、`composite_score`（NUMERIC(5,2)）、`data_source` JSONB、`evaluated_at`、`evaluator`、`status`(draft/published)、`remark`、公共列；索引 `ix_capacity_target(tenant_id,target_type,target_id,period)`。

#### ai_profile — AI定制表（预留，原"评价表"）

不是考核评价，而是为小微厂生成简介推荐/可互动页面的 AI 提示词素材：管理人员评分维度（质量/货期/车工配合度）逐级汇总成厂/车间/产线画像。字段：`tenant_id`、`id`、`subject_type`(factory/workshop/line/manager)、`subject_id`、`order_quality_score`、`delivery_score`、`cooperation_score`、`income_index`、`growth_index`、`child_summary` JSONB、`prompt_profile` JSONB、`profile_text`、`interactive_page_ref`、`generated_by`(ai/manual)、`generated_at`、`version`、`status`(draft/published/offline)、`remark`；索引 `uk_ai_subject(tenant_id,subject_type,subject_id)`。数据来源于产能/质量/货期/工资的汇总投影。

### 2.11 状态机与约束分层汇总

| 对象 | 状态机 | 保证层 |
|---|---|---|
| visitor_lead | curious → interested → engaged → promotion → won / lost | 应用事务：只前进；won 回填 converted_tenant_id；lost 必填原因（CHECK） |
| tenant_subscription | trial → active → expired / suspended | 三条件（协议+付款+功能确认）事务保证；到期任务置 expired |
| tenant.registration_status | pending → approved / rejected | 应用事务 + CHECK；pending 厂无账号不可登录 |
| tenant_crypto_key | active → rotated / revoked | 每厂同时最多一个 active（事务） |
| process_standard | draft → pending_confirm → standard ⇄ revising → superseded | 两确认事务；standard 同厂唯一 |
| line_resource | active → expired | 同产线同时仅一个 active（事务） |
| subscription_payment | pending → confirmed（单向） | confirmed 后禁改金额（事务） |

约束分层：主外键、CHECK、唯一索引由数据库保证；不可变、序号连续性由触发器/事务保证；状态流转、跨表一致性和权限由应用事务保证。

---

## 3. 前后端总体设计

### 3.1 技术基线

| 端 | 技术 | 说明 |
|---|---|---|
| 后端 | Go + pgx/v5 + chi 标准库 ServeMux | `internal/httpapi`：`Public()`（免鉴权）、`Handle()`（JWT+权限点）、`PlatformHandle()`；多租户隔离由 JWT claims 中的 tenant_id 强制 |
| 前端 | Vue3 + TS + Vite（无 UI 库） | `fetch` 封装 `src/api.ts`（baseURL `/api/v1`）；token 存 localStorage |
| 认证 | JWT HS256（claims: tenant_id, user_id, session_id, platform?）+ refresh rotation | 平台/租户两套会话已实现 |

### 3.2 前端路由与会话（现状 + 目标态）

现有公开路由（`src/web/src/router.ts`）：`/portal`（首页）、`/login`、`/trial/home`、`/qr-login`、`/s/:tenant/:code` 等；`/` → redirect `/portal`。

目标态新增/调整：

| 路由 | 页面 | meta | 说明 |
|---|---|---|---|
| `/portal` | PortalPage | public | 首页（已完成）：产品卡 + `#join` 免费体验/客户登录双 Tab |
| `/portal-v1` | PortalPageV1 | public | 首页另一版本（已实现，与 PortalPage 并存） |
| `/portal/home` | TrialHomePage（复用） | public | 制衣厂门户：体验用户与注册用户登录后统一入口（`?tenant=厂编码`） |
| `/register` | TenantRegisterPage | public | 制衣厂注册页（已实现，双模式，见 3.4.3） |
| `/login` | LoginPage | public | 登录页（已完成）：账号登录 + 极速注册/短信/扫码/游客 |
| `/login/matcher`、`/login/sansheng` | MatcherLoginPage / SanshengLoginPage | public | 二撮合 / 三生万 产品登录页（已实现） |
| `/trial/home` | TrialHomePage | public | 体验用户主页 / 游客主页（已实现：动态内容 + 功能点亮 + 进度条） |
| `/trial/pending` | TrialPendingPage | public | 体验注册审批中页面（审批时间线 / 修改注册信息 / 加急，已实现） |
| `/qr-login`、`/q` | QrLoginPage | public | 扫码登录（手机确认端，短链 `?t=ticket&a=模块`） |
| `/s/:tenant/:code` | ScanLandingPage | public | 扫菲/扎号公开落地页（菲票工序任务查询，已实现） |
| `/change-password` | ChangePasswordPage | 需登录 | 首登强制改密页（已实现） |
| `/complete-phone` | CompletePhonePage | 需登录 | 旧数据租户补录注册手机号（已实现，含 phoneGate 路由守卫） |
| `/trial/experience/:module` | TrialModulePage（新增） | public | **未实现**：当前由门户悬浮按钮直入工作台替代 |
| `/workbench/A03` | DashboardPage | 需登录 | 注册用户主页（工作台，已完成） |
| `/tenant/profile` | TenantProfilePage | 需登录 | 注册用户主页的"本厂信息/订阅状态"页（已实现） |
| `/portal/settings` | PortalSettingsPage | 需登录 | 门户内容维护（logo/系统名称/欢迎词，导航"门户内容"，已实现） |
| `/platform/modules`、`/platform/role-templates` | PlatformModulesPage / PlatformRoleTemplatesPage | platform | 平台功能模块 / 角色模板管理（已实现） |

会话存储约定：

| 身份 | 存储 | 守卫 |
|---|---|---|
| 租户/平台 | `clothp_access_token` / `clothp_platform_access_token` | router.beforeEach（已有） |
| 体验用户（本版起为后端签发 JWT） | `clothp_trial_session`（含 `access_token`、`tenant_id`、`factory_name`、`expires_at`、`via`） | TrialHomePage 挂载时校验 |
| 游客 | 同上，无 token，`via='guest'` | 无需验证 |

### 3.3 后端 API 契约

#### 3.3.1 制衣厂注册登录（已实现）

注册流程（v0.02 定稿）：**制衣厂自助注册 → 平台管理员审批启用 → 管理员首次登录改密 → 进入业务主页**。

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/v1/public/tenant-register/send-code` | 公开 | 注册手机验证码：6 位、3 分钟有效、60 秒内不可重发、最多 5 次错误。**当前未接入短信服务商，走演示模式：`sms_sent=false` 并在响应 `demo_code` 返回验证码，由注册页直接显示**（模拟短信验证）；接入服务商后改为真实短信，接口契约不变 |
| POST | `/api/v1/public/tenant-register` | 公开 | 制衣厂自助注册：**须提交 `sms_code`**（与 `contact_phone` 匹配的验证码，校验通过即失效）；生成厂编码/短码，创建 `tenant(registration_status='pending')` 与 `visitor_lead(source=online,intent_status=promotion)`；**不创建登录账号** |
| POST | `/api/v1/public/trial-register` | 公开 | **体验注册（与制衣厂注册同一套流程与页面，仅客户类型不同）**：同样须 `sms_code`；`phone`+`sms_code` 即可（厂名/联系人可空，自动生成"体验厂-短码"/"体验用户"）。**提交后为待审状态**：同事务创建 `tenant(customer_type='trial',registration_status='pending')` + `tenant_subscription(edition=basic,customer_type='trial',status='trial',features='{}',trial_ends_at=+30天)` + `visitor_lead(intent_status='promotion',trial_tenant_id)`；**此阶段不建管理员账号/种子数据**。响应 `status='pending'` 并签发本机 `entry_token`（30 天） |
| GET | `/api/v1/public/trial-content` | 公开 | 体验页动态内容（欢迎词/痛点/解决方案），读 `platform_setting.trial_page_content`，未配置时返回内置默认 |
| GET | `/api/v1/public/trial-features?tenant=厂编码` | 公开 | 体验功能列表（`platform_module` 一级菜单）+ 本厂点亮进度（`tenant_subscription.features`：体验到=1，否则=0）+ 订阅状态与到期时间 |
| POST | `/api/v1/public/trial-features/light` | 公开 | 点亮功能：`{tenant,code}` → `jsonb_set(features,{code},'1')`（只增不减，仅 `status='trial'` 可点亮） |
| PUT | `/api/v1/platform/settings/{key}` | 平台 | 平台参数编辑（版本化 + 审计；体验页内容即 `key=trial_page_content`）；平台控制面"体验页内容"页签表单化编辑 |
| GET/POST | `/api/v1/platform/assistants` | 平台管理员 | 平台辅助人员：列表（含权限目录）/创建（登录名、姓名、初始密码 ≥12 位、权限点集合） |
| PATCH/PUT/POST | `/api/v1/platform/assistants/{id}[/permissions\|/reset-password]` | 平台管理员 | 启停（停用即吊销会话）/ 权限点覆盖更新 / 重置密码（12 位临时密码仅显示一次） |
| GET | `/api/v1/platform/auth/me` | 平台 | 当前平台账号自省：id、login_name、role（super/assistant）、name、permissions |
| GET | `/api/v1/platform/tenant-registrations?status=pending\|approved\|rejected` | 平台 | 注册申请列表（默认待审核） |
| POST | `/api/v1/platform/tenant-registrations/{id}/approve` | 平台 | **审批启用**：确认订阅版本 `edition`（basic/standard/pro）与订阅规模 `user_limit`（启用用户数上限），分配管理员账户与初始密码（≥12位）。体验厂分支：保留 `customer_type='trial'`，以注册手机号作为管理员账户（平台留空时自动取 `contact_phone`），`tenant.registration_status='approved'`，`tenant_subscription` 改为 `trial` 并**从审批通过时开始计算 30 天**；管理员员工档案回填注册手机号；同事务完成种子数据（组织/员工/账号/角色权限/默认配置）、`tenant_crypto_key`（DEK）、lead 置 won、平台审计。正式厂分支仍为 `paid/active` |
| POST | `/api/v1/platform/tenant-registrations/{id}/reject` | 平台 | 驳回（必填原因）：`registration_status='rejected'`，lead 置 lost |
| POST | `/api/v1/auth/unified-login` | 公开 | **已有**，制衣厂登录入口 |
| GET | `/api/v1/tenant/profile` | 租户 token | 本厂资料 + 订阅状态 + 用户配额（used/limit） |
| PUT | `/api/v1/tenant/profile` | 租户 token + `identity:write` | 厂管理员修改辅助信息（联系人/电话/地址/地区/备注，乐观锁） |

`POST /api/v1/public/tenant-register` 请求/响应：

```jsonc
// 请求
{
  "name": "某某制衣厂",            // 全平台唯一
  "code": "YZWS001",              // 厂编码，留空由系统按名称拼音生成并去重
  "region": "CN-GD", "scale": "small",
  "contact_name": "张三", "contact_phone": "13800000000",
  "contact_wechat": "…", "contact_qq": "…", "contact_alipay": "…",  // 辅助联系方式（迁移 051，可空）
  "sms_code": "123456",            // 注册验证码（须与 contact_phone 匹配）
  "address": "…", "remark": "…"
}
// 响应 201：不发放 token，等待平台审批
{ "id": "…", "code": "YZWS001", "name": "某某制衣厂", "status": "pending", "message": "注册申请已提交，…" }
```

审批启用事务流程（`POST …/approve`，单事务）：

1. `SELECT … FOR UPDATE` 校验 `registration_status='pending'`（防重复审批）；
2. `auth.SeedTenant(ExistingTenantID=…)`：在已存在的租户行上落种——更新短码/时区，创建默认组织（生产部）、厂管理员员工档案、`user_account`（tenant_admin 角色，仅 identity 权限，首登强制改密）、mgmt_level 四级（L1–L4）、feature_setting；
3. `UPDATE tenant SET registration_status='approved', approved_at/by, admin_login_name/admin_password_verifier/admin_employee_id, expedite_requested_at=NULL`；customer_type 正式厂置 'paid'、体验厂保持 'trial'；
4. `UPSERT tenant_subscription`：正式厂（edition、user_limit、customer_type='paid'、status='active'、features_confirmed_by/at）；体验厂（edition、user_limit、customer_type='trial'、status='trial'、**trial_ends_at 从审批通过时重新计算 30 天**、保留已点亮 features），并回填管理员员工档案手机号、清除管理员首登强制改密（体验管理员以手机号/微信登录免密码）；
5. `INSERT tenant_crypto_key`（随机 32 字节 DEK，由本地主密钥（JWTSecret 派生）AES-256-GCM 包装后落库，key_wrap_ref='local-master:v1'；已存在 active 密钥时跳过）；
6. 关联 `visitor_lead` 置 `intent_status='won'`；写 `platform_audit_log`。

> 已知差异：第 6 步按 `converted_tenant_id` 匹配 lead，仅覆盖制衣厂注册（lead 写入 converted_tenant_id）；体验注册的 lead 以 `trial_tenant_id` 关联，`converted_tenant_id` 为空，审批通过后**不会被置 won**（仍停留 promotion），待后续修正。

**订阅规模限制**：`tenant_subscription.user_limit` 为本厂"启用账号"数量上限，在以下接口强制校验（超出返回 409 `USER_LIMIT_EXCEEDED`）：`POST /api/v1/users`（创建用户）、`PUT /api/v1/users/{id}/roles`（停用→启用）、`POST /api/v1/employees/{id}/account`（为员工开通账号）。未配置订阅状态的既有租户不受限；厂管理员可通过"本厂信息"页查看配额使用情况。

业务参数配置：厂内业务参数沿用 `feature_setting`（报工自动审批等，已有"功能设置"页），其余业务参数本期暂不实现。

#### 3.3.2 体验用户进入工作台与首次登录规则

体验页 `/trial/home` 右侧中部悬浮“开始体验”按钮（移动端改为右下角悬浮）。点击后按以下规则进入该体验用户的一智算（制衣厂 MES）工作台：

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/v1/public/trial-enter` | 公开 | 凭本机 `entry_token`（或短信 `ticket`）+ `tenant_code` 换取体验厂管理员真实 JWT；返回 `access_token/refresh_token/permissions/tenant` 与新的 `entry_token`。错误码：`TRIAL_PENDING` 待平台审批、`TRIAL_EXPIRED` 体验期结束、`NO_TRIAL_TENANT` 尚无体验厂、`INVALID_ENTRY_TOKEN` 凭证失效 |
| POST | `/api/v1/auth/trial/send-code` | 公开 | 体验/员工登录共用的短信验证码（6 位、3 分钟有效、60 秒重发、最多 5 次错误） |
| POST | `/api/v1/auth/trial/verify` | 公开 | 校验验证码并返回一次性 `ticket` 与该手机号关联的体验厂/制衣厂列表；体验厂待审时返回 `status=pending` 与 `entry_token` |
| POST | `/api/v1/auth/phone-login` | 公开 | `ticket` + `tenant_code` + `password?` 换取真实 JWT；普通员工首次登录缺密码返回 `PASSWORD_REQUIRED`，补密码重试成功后才作废 ticket |
| GET | `/api/v1/public/login-qr/wechat-status?device_id=` | 公开 | 本机微信身份是否已绑定厂账号；已绑定返回账号与厂信息 |
| POST | `/api/v1/public/login-qr/wechat-login` | 公开 | 微信扫码确认：首次 `device_id`+`phone`+`code`+`password?`；已绑定 `device_id` 直接登录；成功后把一次性 `login_ticket` 写入二维码票据 |
| POST | `/api/v1/public/login-qr/exchange` | 公开 | PC 端用 `login_ticket` 一次性取走扫码登录的真实 JWT |

进入与登录规则：

1. 体验用户没有体验厂 → “开始体验”跳转体验类型制衣厂注册（`/register?mode=trial`，自动预填手机号）；提交后为 `registration_status='pending'`，等待平台管理员审批，不再即时开通。
2. 平台管理员审批通过后，注册手机号（或绑定该手机号的微信）即为体验厂管理员账户，并开始计算 30 天体验期；点击“开始体验”直接进入 MES 工作台，菜单按该账号权限显示（`permissions` → `workspaceHome`）。
3. 该厂**其他员工短信首次登录**：需要厂管理员提供的密码 + 本人手机验证码；成功后清除首登强改密标记并记录 `last_login_at`，以后再登录只需手机验证码。
4. 该厂**其他员工微信首次登录**：需要厂管理员提供的密码 + 手机验证码；成功后本机微信身份（`device_id`）绑定该账号，以后扫码只需一键确认，免密码/验证码。
5. 体验厂管理员本人使用手机号/微信登录免工厂密码；`entry_token` 有效期 30 天，仅对 `customer_type='trial'` 的厂有效，不能用于付费厂。
6. `device_id` 绑定为演示级微信身份实现（手机浏览器 localStorage 持久保存），接入微信开放平台后替换为真实 unionid/openid。

体验/游客主页动态内容（`platform_setting` 单键 `trial_page_content`，JSONB，平台管理端"体验页内容"页签可编辑）：

```
trial_page_content  JSONB  { welcome_title, welcome_text,           // 欢迎词（动态）
                             intro_title, pain_points[{title,desc}], // 功能介绍：点出客户痛点→强化痛处
                             solution_title, solution_text }         // 解决方案
```

未配置或欢迎词为空时返回内置默认内容（`defaultTrialContent`）。

#### 3.3.3 游客主页

- **后端不参与验证、不落库**：前端 `guestLogin()` 直接写本地会话（`via='guest'`）进 `/trial/home?via=guest`（LoginPage 已实现）。
- 游客可见范围：**基础版之数据版**——演示厂（平台预置的 sample tenant）只读数据：订单/工序/进度列表、样板查看；**无统计功能**（隐藏报表/统计模块）。
- 前端实现：TrialHomePage 按 `via==='guest'` 渲染游客视图；游客点任何功能 → 引导到体验注册（`via` 转化路径：guest → trial）。
- 转化埋点：游客点击"免费体验"→ 进入体验注册流，后端建 lead（intent_status='curious'→'interested'）。

### 3.4 前端页面设计

#### 3.4.1 首页（PortalPage，已完成，小改）

- 结构：顶部导航（产品/免费体验/客户登录）→ 三产品卡（一智算/二撮合/三生万）→ `#join` 区（免费体验/客户登录双 Tab）→ 底部。
- 改造点：①"免费体验"Tab 增加"注册制衣厂账号"入口链到 `/register`；②`enterApp()` 分流逻辑保留（有效租户 token→`/workbench/A03`；有效体验会话→`/trial/home`）。

#### 3.4.2 登录页（LoginPage，已完成，小改）

- 已有：账号登录（unified-login）、短信登录、扫码登录、游客浏览。
- 改造点：①"极速注册"与"免费体验一个月"入口统一指向 `/register?mode=trial`（体验注册与制衣厂注册同一套页面与流程，见 3.4.3）；②"制衣厂注册"链接到 `/register`；③登录成功按 `account_type` 分流（已有）。

#### 3.4.3 注册页（TenantRegisterPage，已实现，双模式）

同一页面/表单承载两种注册，仅文案与提交接口不同（`?mode=trial` 切换，页脚可互转）：

- **制衣厂注册（默认）**：厂名称*、厂编码（留空自动生成）、所在国/地区、厂规模、联系人*、联系电话*、**短信验证码***、地址、备注 → 提交后 `pending`，等平台审批启用。
- **体验注册（`?mode=trial`）**：隐藏厂编码（自动生成），厂名/联系人可空（自动生成"体验厂-短码"/"体验用户"），其余同上 → 提交后与制衣厂注册一样进入 **pending 待平台审批**，页面跳 `/trial/pending`（审批时间线，可**修改注册信息**——联系电话锁定为登录手机号，可**加急申请**）。审批通过后：以注册手机号作为体验厂管理员账户、落种子数据与 DEK、**从通过时开始计算 30 天体验期**；管理员本人手机号/微信/进入令牌登录免密码，初始密码供本厂其他员工首次短信/微信登录使用；进入后与正式厂使用完全相同的业务页面。同一手机号重复提交幂等返回既有体验厂（`returning=true`，待审/已通过均返回，并签发 `entry_token`）。
- **模拟短信验证（当前无短信服务商）**：点击"获取验证码"后，验证码直接显示在输入框下方（演示模式），60 秒倒计时重发、3 分钟有效；填码提交，后端校验通过后一次性失效。接入短信服务商后同一交互改为真实短信，无需改页面。

#### 3.4.4 注册用户主页（已有工作台 + 新增厂信息页）

- 主页 = `/workbench/A03` DashboardPage（已有）：按权限渲染工作台卡片。
- 新增 `/tenant/profile`：本厂资料卡（编码/短码/联系方式/地区/规模）、订阅状态卡（版本、功能清单、生效状态、到期倒计时、体验转正引导）、操作（修改辅助信息）。侧边栏增加"本厂信息"入口。（"重置二维码"依赖厂二维码签名，尚未实现，见 6.3 #5）

#### 3.4.5 体验用户主页（TrialHomePage，已实现）

- 挂载校验：无有效体验会话 → `/portal`（已有）。
- 结构（已实现，全部动态、平台控制面"体验页内容"页签编辑）：
  1. **欢迎词**（动态：`GET /public/trial-content` → welcome_title/welcome_text）；
  2. **功能介绍**（动态：intro_title + pain_points 痛点列表，点出客户痛点并强化痛处）；
  3. **解决方案**（动态：solution_title/solution_text）+ **三种选择**：**① 继续体验**（滚动进入功能体验区）/ **② 暂停，下次继续**（体验会话含点亮进度已存本机，回首页随时继续）/ **③ 退出体验**（清会话回首页）；
  4. **功能体验区**：按平台功能模块清单渲染（`GET /public/trial-features?tenant=厂编码`，来源 `platform_module` 一级菜单），右上角标注点亮进度 **1/0**；点击即点亮（`POST /public/trial-features/light`，只增不减），顶部统计"已点亮 n/总数"与体验剩余天数；
  5. 底部转化条：一键开通生成的管理员账号与初始密码（仅本机保存一次）、体验转正引导。
- 游客/扫码视图（无 tenantCode）：功能区仍渲染但点击提示开通体验厂，引导 `/register?mode=trial`。

#### 3.4.6 体验功能页（TrialModulePage，新增）

- 展示体验厂内对应模块的示例数据（体验厂预置演示订单/产品/工序），保持只读或受限写（如报工演示）；进入即上报体验进度点亮。

---

## 4. 状态机汇总

| 对象 | 状态机 | 保证层 |
|---|---|---|
| 访客线索 visitor_lead | curious → interested → engaged → promotion → won / lost | 应用事务：只前进不回退；won 回填 converted_tenant_id；lost 必填 lost_reason |
| 订阅 tenant_subscription | trial → active → expired / suspended | trial→active 须协议 signed + 付款 confirmed + 功能确认三条件齐备；到期由后台任务置 expired |
| 付费使用状态 health_status | activated → stable → active_user | 本期预留（按登录频率/使用深度评估） |
| 体验会话 | 有效（≤30 天）→ 到期 → 引导转正/重新体验 | 当前仅读取时校验（trial-enter/微信登录比较 trial_ends_at）；到期扫描任务未实现（见 6.3） |
| tenant_subscription.features | 0 → 1（单功能粒度，体验点亮） | `POST /public/trial-features/light` 上报，仅体验厂（status='trial'），只增不减 |
| trial_extension 体验延期申请 | pending → approved / rejected | 同一厂同时仅一条 pending；批准须填写晚于当前截止的新 trial_ends_at，通过后同步 tenant_subscription（事务保证） |
| tenant 加急标记 | 空 → expedite_requested_at（仅 pending 可标记，审批/驳回清除） | `POST /public/trial-expedite`，同一厂一次 |

---

## 5. 安全设计要点

- 密码：Argon2id 验证器（`internal/auth/password.go`），注册/登录/改密统一走该实现；密码永不落明文。
- 注册接口防滥用：IP/账号限流（沿用 SMS 服务商限流与熔断思路）、密码强度校验、验证码频率限制（60s/次、3 分钟有效）。
- 唯一性并发：注册依赖 DB 唯一索引兜底（code/name/admin_login_name/lead_no），冲突返回 409 与字段级错误。
- 体验 token：`trial:true` claim + 后端按 permission 白名单校验；体验厂业务接口全部强制 tenant_id 过滤。
- 审计：注册、体验开通、进度点亮、平台编辑动态内容均写审计（租户侧 `audit_log` / 平台侧 `platform_audit_log`）。

---

## 6. 与现有代码衔接及实施清单

### 6.1 已完成（本次直接复用）

| 项 | 位置 |
|---|---|
| 首页（含免费体验/客户登录入口） | `src/web/src/pages/PortalPage.vue` |
| 登录页（账号/短信/扫码/游客） | `src/web/src/pages/LoginPage.vue` |
| 平台/租户统一登录、JWT 会话、改密 | `internal/auth`、`internal/httpapi/auth_routes.go` |
| 短信验证码（多服务商 + 演示模式；内置腾讯云 SendSms 直连：`TENCENT_*` 环境变量配齐自动启用） | `internal/httpapi/trial_routes.go`、`internal/sms`（`tencent.go`） |
| 体验主页雏形、体验会话（前端） | `TrialHomePage.vue`、`trial.ts`、`trialAuth.ts` |
| 租户开通种子（组织/管理员/权限） | `internal/auth/tenant_seed.go` |
| 租户短码 | 迁移 034/035 |

### 6.2 本次已实现

| 项 | 位置 | 状态 |
|---|---|---|
| 厂商域数据库迁移（7 表/扩展） | `internal/database/migrations/043_tenant_commerce.sql` | **已落地** |
| 注册审批与订阅规模迁移 | `internal/database/migrations/044_tenant_lifecycle.sql`（registration_status/approved_at/user_limit 等） | **已落地** |
| 厂内业务域迁移（组织扩展/产线配置/管理级别/资料模板/工序表四表） | `internal/database/migrations/045_org_process_design.sql`（含 mgmt_level 四级预置、user_account.mgmt_level_id） | **已落地** |
| 数据库指向 yzws-v1 | `src/config.yaml` | **已修改** |
| 制衣厂自助注册 / 平台审批启用 / 驳回 / 注册列表 API | `internal/tenantreg/tenantreg.go`（新包）+ `cmd/server/main.go` 接线 | **已实现** |
| `SeedTenant` 支持在已存在（待审批）租户上落种 | `internal/auth/tenant_seed.go`（`ExistingTenantID`） | **已实现** |
| 订阅规模限制（启用用户数上限） | `internal/production/identity.go` `checkUserQuota`（创建/启用用户、员工开账号三处强制） | **已实现** |
| 本厂资料与订阅状态 API | `internal/tenantreg/tenantreg.go`（GET/PUT `/tenant/profile`） | **已实现** |
| 租户列表输出注册审批状态 | `internal/platformadmin/platform.go` | **已实现** |
| 前端：制衣厂注册页（`/register`，公开） | `src/web/src/pages/TenantRegisterPage.vue` + `router.ts` + LoginPage 入口 | **已实现** |
| 前端：平台控制面"注册审批"页签（审批弹窗：版本/规模/管理员账户/初始密码；驳回） | `src/web/src/pages/PlatformAdminPage.vue` | **已实现** |
| 前端：本厂信息页（资料/订阅状态/用户配额/辅助信息编辑） | `src/web/src/pages/TenantProfilePage.vue`（`/tenant/profile`）+ 侧边栏入口 | **已实现** |
| 前端：工作台订阅信息横幅 | `src/web/src/pages/DashboardPage.vue` | **已实现** |
| 注册手机验证码（模拟短信：验证码由响应返回并在页面显示，注册须校验） | `internal/tenantreg/tenantreg.go`(`/public/tenant-register/send-code`)、`TenantRegisterPage.vue` | **已实现** |
| 体验注册与制衣厂注册统一（同一页面/流程，均提交平台审批；审批通过后体验厂落种子数据、以手机号作管理员账户、30 天体验期从通过时起算，功能页面与正式厂相同） | `internal/tenantreg/tenantreg.go`(`/public/trial-register`，审批时复用 `SeedTenant` 与标识分配助手)、`TenantRegisterPage.vue`(?mode=trial)、`LoginPage.vue`、`PortalPage.vue` | **已实现** |
| 一键免费体验（手机验证码校验即自动在制衣厂表开通体验厂记录：厂编码/短码/类型/订阅/lead 全自动，签发 30 天 `entry_token`；同手机号幂等返回既有厂；管理员账号/初始密码/种子数据/`tenant_crypto_key` 在平台审批通过时创建） | `internal/tenantreg/tenantreg.go`、`PortalPage.vue`（startTrial）、`trial.ts` | **已实现** |
| 体验功能列表点亮（体验到一个功能标 1，否则 0；进度存 `tenant_subscription.features`） | `/public/trial-features`、`/public/trial-features/light`、`TrialHomePage.vue` 功能体验区 | **已实现** |
| 体验页动态内容（欢迎词/痛点介绍/解决方案，平台后台改什么体验页显示什么）+ 三选择（继续/暂停/退出） | `TrialHomePage.vue`、`/public/trial-content`、`PlatformAdminPage.vue`"体验页内容"页签（`PUT /platform/settings/{key}`，key=trial_page_content） | **已实现** |
| 腾讯云短信接入（`TENCENT_SECRET_ID/SECRET_KEY/SDKAppID` 环境变量 + `TENCENT_SMS_SIGN_NAME/TEMPLATE_ID` 配齐即真发，多服务商路由自动纳入；未配齐保持演示模式） | `internal/sms/tencent.go`、`router.go` | **已实现** |
| 平台辅助人员角色（迁移 046：`platform_admin.role/name` + `platform_admin_permission`；登录返回角色与权限；平台接口按路由权限点门禁；辅助人员不能管理平台人员；控制面"辅助人员"页签管理创建/分配权限/启停/重置密码） | `internal/auth/platform.go`、`internal/httpapi/api.go`、`internal/platformadmin/assistants.go`、`PlatformAdminPage.vue`、`PlatformLoginPage.vue` | **已实现** |
| 体验用户悬浮助手（右下角悬浮开关；选择功能后按脚本逐步打开页面并高亮目标元素，动画箭头游戏教学式引导；**按当前用户权限过滤步骤**：越权功能自动跳过并在结束时提示「添加用户并分配权限」或「退出并切换账号登录」，整条无权限时直接进入开通引导） | `web/src/guides.ts`、`GuideAssistant.vue`（挂 App.vue 业务外壳，注入 `/me/permissions`） | **已实现** |
| 体验用户手机号记忆：短信登录验证通过即回带该手机号的体验厂信息，**直接进入其体验主页**（无需用户名密码）；一键体验对同一手机号幂等（不再重复建厂，返回既有体验厂，`returning=true`） | `/auth/trial/verify`（回带 `tenant`）、`/public/trial-register`（幂等返回）、`LoginPage.vue`、`trial.ts` | **已实现** |
| 手机号多厂导航：一个手机号**最多占用一个体验厂，可绑定多个制衣厂**；短信验证通过返回全部关联厂（`tenants[]`，体验厂 + 员工档案手机号绑定的制衣厂账号），单厂直接进入、多厂弹出**选厂列表**；选中制衣厂后凭一次性 ticket 调 `POST /auth/phone-login` 换取该厂绑定账号的真实会话，按权限进入其工作台主页 | `/auth/trial/verify`（`tenants[]`）、`POST /auth/phone-login`（ticket 一次性，30 分钟有效）、`auth.Service.LoginByPhone`（user_account JOIN employee.phone）、`LoginPage.vue` 选厂界面、`trial.ts` | **已实现** |
| 体验主页欢迎词 + 体验进度驱动内容：欢迎区展示平台可编辑的欢迎词、体验剩余天数与**体验进度条**（点亮功能比例），进度分阶段提示文案；提供「以正式用户身份登录完整系统」入口引导进入与收费用户一致的页面 | `TrialHomePage.vue`（`/public/trial-content` 欢迎词、`/public/trial-features` 点亮进度） | **已实现** |
| 体验主页右侧中部「开始体验」悬浮按钮：点击凭 30 天本机进入令牌换取体验厂管理员真实会话，**按权限进入 MES 工作台**；未注册体验厂 → 跳体验注册页（与正式注册同流程、平台管理员审批）；审核中 → 明确提示；凭证失效 → 全局 Toast 提示并带手机号跳回短信登录，验证通过后**自动回到体验主页并进入工作台**（`?enter=1` 闭环） | `POST /public/trial-enter`、`auth/trial_entry.go`（30 天 JWT 进入令牌）、`TrialHomePage.vue`（th-start-fab + startExperience + Toast 反馈）、`LoginPage.vue`（mode/phone 预填 + enter 回跳） | **已实现** |
| 体验注册审批中页面 + **加急申请**：提交体验注册后进入 `/trial/pending`（审批状态时间线）；加急落 `tenant.expedite_requested_at`（同一厂一次），平台管理员「注册审批」列表显示 🔥加急图标并置顶，审批/驳回后清除 | `POST /public/trial-expedite`、`TrialPendingPage.vue`、`PlatformAdminPage.vue`（registrations.expedited） | **已实现** |
| **申请延长体验**：体验厂管理员在体验主页提交延期申请（原因选填），平台管理员「体验延期」页签审批：批准时**填写/选择新的体验截止时间**（须晚于当前截止时间），通过后同步更新 `tenant_subscription.trial_ends_at`；驳回填写原因。体验主页实时展示申请进度与最新截止时间 | `trial_extension` 表（迁移 047）、`POST /public/trial-extend`、`GET /public/trial-extend/status`、`GET/POST /platform/trial-extensions*`、`TrialHomePage.vue` 延期卡片 | **已实现** |
| 审批中可**查看/修改注册信息**：`/trial/pending` 展开注册信息表单（名称/地区/规模/联系人/地址/备注可改，**联系电话锁定为体验用户登录手机号**），保存后平台管理员审批时看到最新内容；审批后锁定。平台管理员「注册审批」列表明确显示**用户类型（体验用户/付费用户）**与加急图标 | `GET /public/trial-info`、`POST /public/trial-update`（仅 pending 可改，SQL 不含 contact_phone）、`TrialPendingPage.vue` 信息表单、`PlatformAdminPage.vue` | **已实现** |
| **制衣厂门户（体验用户与注册用户共用）**：登录后统一进入 `/portal/home?tenant=厂编码`（复用体验主页布局）。顶部品牌显示**本厂 logo + 厂名**（无 logo 用平台默认），徽标按版本显示“免费体验版/正式版”；欢迎标题与欢迎语由**本厂管理员维护**（留空用平台默认：体验版“欢迎使用{厂名}·一智算制衣”，正式版“欢迎使用{厂名}生产管理系统”）。右侧悬浮按钮：体验用户「开始体验」、注册用户「进入工作」，点开同一 MES 工作台（按权限显示菜单）；概览卡片展示今日工单/在制订单/员工总数/累计工单 | `tenant_portal` 表（迁移 048）、`GET /public/tenant-portal`、`GET/PUT /tenant-portal/content`（本厂管理员或 settings 权限）、`TrialHomePage.vue`、`PortalSettingsPage.vue`（导航“门户内容”） | **已实现** |
| 平台管理入口修复（`/platform/login` 由 redirect 改为 PlatformLoginPage） | `src/web/src/router.ts` | **已实现** |
| Reconcile 解析修复（`ADD COLUMN IF NOT EXISTS` 列名误判为 `IF`） | `internal/database/reconcile.go` | **已实现** |
| **v0.02 增补（2026-11-10 校对补充，超出原设计稿的部分）：** | | |
| 门户切换制衣厂：登录时验证过的手机号（`auth_session.login_phone`，迁移 049）名下多厂，门户顶部下拉切换；切换后按该手机号在目标厂绑定的账号签发新会话 | `GET /auth/my-factories`、`POST /auth/switch-tenant`、`auth.Service.SwitchTenant/PhoneTenants/PrincipalPhones`、`TrialHomePage.vue`（厂下拉） | **已实现** |
| 企业 logo 上传存库：PNG/GIF ≤500KB 直接存 `tenant_portal.logo_data`（迁移 050），经公开接口按厂编码输出，不依赖静态目录；与 logo 网址二选一，可删除恢复平台默认 | `POST/DELETE /tenant-portal/logo`、`GET /public/tenant-logo`、`PortalSettingsPage.vue` | **已实现** |
| 门户系统名称自定义：`tenant_portal.system_name`（迁移 052），工作页面 logo 旁显示，留空用平台默认"一智算" | `GET /tenant/branding`、`PUT /tenant-portal/content`、`PortalSettingsPage.vue` | **已实现** |
| 注册辅助联系方式（微信/QQ/支付宝，迁移 051）：注册表单、平台审批列表、审批中修改（trial-update）、本厂信息页全链路 | `internal/tenantreg/tenantreg.go`、`trial_extras.go`、`TenantRegisterPage.vue`、`TenantProfilePage.vue`、`PlatformAdminPage.vue` | **已实现** |
| 旧数据租户补录注册手机号：厂管理员登录后补录本厂联系电话（不校验短信），补录前 phoneGate 路由守卫拦截业务页 | `POST /auth/complete-phone`、`CompletePhonePage.vue`、`phoneGate.ts` | **已实现** |
| 扫码登录全链路（三模块：一智算/二撮合/三生万）：建票→扫码→确认/取消→PC 轮询→一次性 exchange 取会话；手机端微信身份绑定后一键确认 | `login_qr_routes.go`（create/scan/confirm/cancel/status/exchange）、`QrLoginPage.vue`、`qrLogin.ts`、`/q` 短链 | **已实现** |
| 多产品入口页：首页 V1（`/portal-v1`）、二撮合/三生万登录页 | `PortalPageV1.vue`、`MatcherLoginPage.vue`、`SanshengLoginPage.vue` | **已实现** |
| 扫菲/扎号公开落地页：免登录按 `/{tenant}/{code}` 查询菲票工序任务 | `ScanLandingPage.vue`、`/s/:tenant/:code` | **已实现** |
| 短信运维视图：演示模式判定、各服务商今日用量/连续失败/熔断状态（需登录） | `GET /auth/trial/sms-mode`、`GET /auth/trial/sms-status`、`internal/sms/router.go` | **已实现** |
| 平台接口路由权限点门禁：辅助人员按 registrations/settings/audit/modules/role_templates/tenants 权限点放行，assistants.manage 仅超管 | `internal/httpapi/api.go`（platformRoutePermission） | **已实现** |

说明：后端启动时 `database.Migrate`（事务执行新迁移 + `schema_migration` 记录 checksum）与 `Reconcile`（对缺失的表/列/索引/触发器自动补全）共同保证"启动后自动补全数据表和字段"。

### 6.3 待实现（按优先级）

| # | 任务 | 涉及 |
|---|---|---|
| ~~1~~ | ~~体验流程落库改造~~ **已完成**：`/public/trial-register`（建体验厂 pending + lead + 订阅状态，见 3.3.1；管理员账号/种子数据/DEK 于平台审批通过时创建） | — |
| ~~2~~ | ~~体验页动态内容~~ **已完成**：`/public/trial-content` + 平台控制面"体验页内容"页签（platform_setting.trial_page_content） | — |
| ~~3~~ | ~~TrialHomePage 改造~~ **已完成**：三段动态内容、三选择、功能点亮（见 3.4.5） | — |
| 4 | 到期扫描任务（trial 到期→expired；协议到期→订阅 expired）——**仍未实现**：main.go 无任何定时任务，到期目前仅靠读取时校验（trial-enter、微信登录比较 trial_ends_at） | 后端定时任务 |
| 5 | 二维码签名（HMAC）与厂二维码落地接口——**仍未实现**：`tenant.qr_code_content` 列已建（043），但 Go 代码无生成/校验逻辑；现有 `/s/:tenant/:code` 是扫菲落地页，非厂二维码 | `internal/platform/shortcode.go` 扩展 |
| 6 | 厂内业务参数配置界面扩展（暂不实现的业务参数） | SettingsPage / feature_setting |
| 7 | 045 厂内业务表仅有迁移与种子，**无业务 API**：line_resource（产线配置）、mgmt_level 授权计算（user_account.mgmt_level_id 未使用）、user_profile_template（资料模板）、process_standard 四表（工序表两确认流程） | internal/production 或新包 |
| 8 | 字段级加密落地：敏感字段（employee.phone/id_card_no/bank_card、tenant.contact_phone、visitor_lead.contact_phone 等）按 2.4.3 以 `enc:` 前缀 AES-256-GCM 加密并脱敏展示；当前均为明文 | 数据访问层 + tenant_crypto_key 解包装 |

---

## 附录 A：本设计迁移文件清单

| 迁移 | 覆盖设计章节 | 内容 |
|---|---|---|
| `043_tenant_commerce.sql` | 2.3 / 2.4 | tenant 扩展（注册所需列）+ system_release、visitor_lead、subscription_agreement、subscription_payment、tenant_subscription、tenant_crypto_key |
| `044_tenant_lifecycle.sql` | 2.3.5 / 2.4.1 | tenant.registration_status/approved_at/by（注册审批）；tenant_subscription：agreement_id 放宽可后补、新增 user_limit（订阅规模） |
| `045_org_process_design.sql` | 2.5 / 2.6 | org_unit 扩展（workshop_nature 等 5 列）；line_resource；mgmt_level（存量厂预置 L1–L4）+ user_account.mgmt_level_id；user_profile_template；process_standard / _line / _confirm / _grant |
| `046_platform_assistants.sql` | 6.2 | platform_admin.role/name + platform_admin_permission（平台辅助人员与权限点） |
| `047_trial_expedite_extension.sql` | 6.2 | tenant.expedite_requested_at（加急标记）；trial_extension（体验延期申请） |
| `048_tenant_portal.sql` | 6.2 | tenant_portal（门户内容：logo 网址/欢迎词，tenant_id 唯一） |
| `049_auth_session_login_phone.sql` | 6.2 | auth_session.login_phone（登录时验证的手机号，门户切换制衣厂用） |
| `050_tenant_portal_logo.sql` | 6.2 | tenant_portal.logo_data/logo_content_type（logo 图片存库） |
| `051_tenant_contact_social.sql` | 2.4.1 | tenant.contact_wechat/contact_qq/contact_alipay（辅助联系方式） |
| `052_tenant_portal_system_name.sql` | 6.2 | tenant_portal.system_name（制衣厂系统名称） |
| `001/002/003/010/027/034/035…`（既有） | 2.9 | 核心/扩展/平台表、角色模板、工序库补列、租户短码等 |
| —（预留） | 2.10 | capacity_profile / ai_profile 本期不建表 |

## 附录 B：术语

| 术语 | 含义 |
|---|---|
| tenant / tenant_id | 制衣厂 / 制衣厂ID（数据隔离键） |
| trial | 体验（体验客户、体验制衣厂、体验协议） |
| visitor_lead | 访客/潜在客户线索（含用户裂变来源） |
| features | 订阅功能清单快照；体验厂按体验进度点亮（1=已体验） |
| health_status | 付费用户使用状态：激活→稳定→积极 |
| DEK / KMS | 数据加密主密钥 / 密钥管理服务（包装 DEK 后落库） |
