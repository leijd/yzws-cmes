/* 物料类型：物料主数据增加类型字段，并初始化物料类型字典 */
ALTER TABLE material ADD COLUMN IF NOT EXISTS material_type TEXT NOT NULL DEFAULT '';

INSERT INTO dictionary_item(tenant_id,id,dictionary_type,code,name,sort_no,created_at,created_by,created_node,updated_at,updated_by,updated_node)
SELECT t.id, 'dic-'||t.id||'-material_type-'||v.code, 'material_type', v.code, v.name, v.sort,
       (extract(epoch from now())*1000000)::BIGINT,'system','migration',
       (extract(epoch from now())*1000000)::BIGINT,'system','migration'
FROM tenant t CROSS JOIN (VALUES
  ('fabric','面料',1),('trim','辅料',2),('packaging','包装材料',3),('other','其他',9)
) AS v(code,name,sort)
ON CONFLICT DO NOTHING;
