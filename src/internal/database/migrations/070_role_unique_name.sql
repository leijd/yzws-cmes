-- 角色唯一性（每厂一份）与编码自动生成：
--   1) 同厂不允许同名：按 lower(btrim(name)) 归一后唯一；编码仍由 (tenant_id,code) 唯一约束保证。
--   2) 角色编码改由服务端生成（custom_001、custom_002…），前端不再填写，避免自拟编码重复。
-- 历史同名数据先加序号去重，否则唯一索引无法建立（两库各执行一遍，第二次已无重复，幂等）。

WITH ranked AS (
  SELECT tenant_id, id, name,
         row_number() OVER (PARTITION BY tenant_id, lower(btrim(name)) ORDER BY created_at, id) AS rn
  FROM role
)
UPDATE role r SET name = ranked.name || ' (' || ranked.rn || ')'
FROM ranked
WHERE r.tenant_id = ranked.tenant_id AND r.id = ranked.id AND ranked.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS ux_role_tenant_name ON role(tenant_id, lower(btrim(name)));

COMMENT ON INDEX ux_role_tenant_name IS '同厂角色名称唯一（忽略大小写与首尾空格）';
