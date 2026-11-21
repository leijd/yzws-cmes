/* 通用流程待办气泡：可配置“上一步完成 → 下一步相关员工待处理记录数” */
CREATE TABLE workflow_badge_flow (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  code TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
  sort_no INTEGER NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,code)
);

CREATE TABLE workflow_badge_step (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  flow_id TEXT NOT NULL,
  step_code TEXT NOT NULL,
  name TEXT NOT NULL,
  menu_path TEXT NOT NULL DEFAULT '',
  permission_code TEXT NOT NULL DEFAULT '',
  recipient_role_codes JSONB NOT NULL DEFAULT '[]',
  count_kind TEXT NOT NULL DEFAULT 'none',
  trigger_event TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,flow_id,step_code),
  FOREIGN KEY (tenant_id,flow_id) REFERENCES workflow_badge_flow(tenant_id,id) ON DELETE CASCADE
);

CREATE TABLE workflow_badge_edge (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  flow_id TEXT NOT NULL,
  from_step_id TEXT NOT NULL,
  to_step_id TEXT NOT NULL,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,from_step_id,to_step_id),
  FOREIGN KEY (tenant_id,flow_id) REFERENCES workflow_badge_flow(tenant_id,id) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id,from_step_id) REFERENCES workflow_badge_step(tenant_id,id) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id,to_step_id) REFERENCES workflow_badge_step(tenant_id,id) ON DELETE CASCADE
);

COMMENT ON TABLE workflow_badge_flow IS '流程待办气泡：租户级流程定义';
COMMENT ON COLUMN workflow_badge_step.menu_path IS '待办对应的主菜单路径，前端按此路径显示气泡';
COMMENT ON COLUMN workflow_badge_step.permission_code IS '具有该权限的用户视为下一步相关人员';
COMMENT ON COLUMN workflow_badge_step.recipient_role_codes IS '下一步相关角色编码数组，与权限任一命中即可';
COMMENT ON COLUMN workflow_badge_step.count_kind IS '待处理记录数来源：material_issue_pending_confirm / project_confirmed_pending_receipt / material_receipt_draft / none';

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at)
VALUES ('mod-menu-workflow-badge','mod-menu-settings','menu','menu.workflow.badge','流程待办配置','/settings/workflow-badges','settings:read',111,1)
ON CONFLICT(code) DO NOTHING;

/* 默认流程：订单确认 → 仓管物料入库；出库登记 → 领料人确认 */
WITH f AS (
  INSERT INTO workflow_badge_flow(tenant_id,id,code,name,status,sort_no,created_at,created_by,updated_at,updated_by)
  SELECT t.id,'wbf-'||t.id||'-order-receipt','order_to_receipt','订单到物料入库','active',10,(extract(epoch from now())*1000000)::BIGINT,'migration',(extract(epoch from now())*1000000)::BIGINT,'migration'
  FROM tenant t
  ON CONFLICT(tenant_id,code) DO NOTHING
  RETURNING tenant_id,id
), s1 AS (
  INSERT INTO workflow_badge_step(tenant_id,id,flow_id,step_code,name,menu_path,permission_code,recipient_role_codes,count_kind,trigger_event,enabled,sort_no)
  SELECT tenant_id,'wbs-'||tenant_id||'-order-confirm',id,'order_confirm','订单确认','/orders/C03','project:write','[]','none','project.confirmed',TRUE,10 FROM f
  ON CONFLICT(tenant_id,flow_id,step_code) DO NOTHING
  RETURNING tenant_id,id
), s2 AS (
  INSERT INTO workflow_badge_step(tenant_id,id,flow_id,step_code,name,menu_path,permission_code,recipient_role_codes,count_kind,trigger_event,enabled,sort_no)
  SELECT f.tenant_id,'wbs-'||f.tenant_id||'-receipt-pending',f.id,'receipt_pending','物料入库待处理','/materials/receipts','inventory:receipt','["warehouse"]','project_confirmed_pending_receipt','project.confirmed',TRUE,20 FROM f
  ON CONFLICT(tenant_id,flow_id,step_code) DO NOTHING
  RETURNING tenant_id,id
)
INSERT INTO workflow_badge_edge(tenant_id,id,flow_id,from_step_id,to_step_id)
SELECT f.tenant_id,'wbe-'||f.tenant_id||'-order-receipt',f.id,s1.id,s2.id FROM f JOIN s1 ON s1.tenant_id=f.tenant_id JOIN s2 ON s2.tenant_id=f.tenant_id
ON CONFLICT(tenant_id,from_step_id,to_step_id) DO NOTHING;

WITH f AS (
  INSERT INTO workflow_badge_flow(tenant_id,id,code,name,status,sort_no,created_at,created_by,updated_at,updated_by)
  SELECT t.id,'wbf-'||t.id||'-outbound-confirm','outbound_confirm','物料出库到领取确认','active',20,(extract(epoch from now())*1000000)::BIGINT,'migration',(extract(epoch from now())*1000000)::BIGINT,'migration'
  FROM tenant t
  ON CONFLICT(tenant_id,code) DO NOTHING
  RETURNING tenant_id,id
), s1 AS (
  INSERT INTO workflow_badge_step(tenant_id,id,flow_id,step_code,name,menu_path,permission_code,recipient_role_codes,count_kind,trigger_event,enabled,sort_no)
  SELECT tenant_id,'wbs-'||tenant_id||'-outbound-issue',id,'outbound_issue','仓管出库','/materials/outbound','inventory:issue','[]','none','material_issue.created',TRUE,10 FROM f
  ON CONFLICT(tenant_id,flow_id,step_code) DO NOTHING
  RETURNING tenant_id,id
), s2 AS (
  INSERT INTO workflow_badge_step(tenant_id,id,flow_id,step_code,name,menu_path,permission_code,recipient_role_codes,count_kind,trigger_event,enabled,sort_no)
  SELECT f.tenant_id,'wbs-'||f.tenant_id||'-outbound-confirm',f.id,'outbound_confirm','领料确认','/materials/outbound','inventory:issue_confirm','["group_leader","line_leader"]','material_issue_pending_confirm','material_issue.created',TRUE,20 FROM f
  ON CONFLICT(tenant_id,flow_id,step_code) DO NOTHING
  RETURNING tenant_id,id
)
INSERT INTO workflow_badge_edge(tenant_id,id,flow_id,from_step_id,to_step_id)
SELECT f.tenant_id,'wbe-'||f.tenant_id||'-outbound-confirm',f.id,s1.id,s2.id FROM f JOIN s1 ON s1.tenant_id=f.tenant_id JOIN s2 ON s2.tenant_id=f.tenant_id
ON CONFLICT(tenant_id,from_step_id,to_step_id) DO NOTHING;
