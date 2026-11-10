ALTER TABLE role ADD COLUMN source TEXT NOT NULL DEFAULT 'tenant' CHECK(source IN('platform','tenant'));
ALTER TABLE role ADD COLUMN platform_template_id TEXT;
COMMENT ON COLUMN role.source IS '角色来源：platform为租户初始化时复制的平台模板，tenant为租户自定义角色';
COMMENT ON COLUMN role.platform_template_id IS '来源平台角色模板ID，租户自定义角色为空';

UPDATE role r SET source='platform', platform_template_id=t.id FROM platform_role_template t WHERE r.code=t.code;
