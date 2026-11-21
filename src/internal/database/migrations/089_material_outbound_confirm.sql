/* 物料出库：仓管登记后可选让领料人确认；确认前不扣库存、不生成库存流水 */
ALTER TABLE material_issue
  ADD COLUMN IF NOT EXISTS receiver_employee_id TEXT,
  ADD COLUMN IF NOT EXISTS confirm_status TEXT NOT NULL DEFAULT 'none',
  ADD COLUMN IF NOT EXISTS confirmed_at BIGINT,
  ADD COLUMN IF NOT EXISTS confirmed_by TEXT,
  ADD COLUMN IF NOT EXISTS confirmed_node TEXT,
  ADD COLUMN IF NOT EXISTS notified_at BIGINT;
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='fk_material_issue_receiver') THEN
    ALTER TABLE material_issue ADD CONSTRAINT fk_material_issue_receiver
      FOREIGN KEY (tenant_id, receiver_employee_id) REFERENCES employee(tenant_id,id);
  END IF;
END $$;

ALTER TABLE material_issue DROP CONSTRAINT IF EXISTS material_issue_status_check;
ALTER TABLE material_issue ADD CONSTRAINT material_issue_status_check
  CHECK(status IN ('posted','pending_confirm','reversed'));

CREATE INDEX IF NOT EXISTS ix_material_issue_receiver ON material_issue(tenant_id,receiver_employee_id,status,id);

COMMENT ON COLUMN material_issue.receiver_employee_id IS '领料人（通常为线长），为空表示仓管直接发放无需确认';
COMMENT ON COLUMN material_issue.confirm_status IS 'none 无需确认 / pending 待领取确认 / confirmed 已确认';

/* 领取物料：线长确认领料单 */
INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('inventory:issue_confirm','仓库管理','领取物料','action','领料人确认仓管开具的物料出库单',FALSE,99)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-material-outbound',NULL,'menu','menu.material.outbound','物料出库','/materials/outbound','inventory:issue',46,1),
('mod-inventory-issue-confirm','mod-menu-warehouse','button','button.inventory.issue.confirm','领取物料',NULL,'inventory:issue_confirm',99,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
WHERE p.code='inventory:issue_confirm'
ON CONFLICT(tenant_id,code) DO NOTHING;

/* 已有派工权限的岗位（通常线长/组长）平滑获得领取确认权限 */
WITH map(old_code,new_code) AS (VALUES ('task:assign','inventory:issue_confirm'))
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT DISTINCT rp.tenant_id,'rp-'||rp.role_id||'-'||substr(md5(np.code),1,8),rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id
JOIN map m ON m.old_code=op.code
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code=m.new_code
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin' AND p.code='inventory:issue_confirm'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-gl-pick','prt-group-leader','inventory:issue_confirm',1),
('prtp-ps-pick','prt-production-supervisor','inventory:issue_confirm',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;
