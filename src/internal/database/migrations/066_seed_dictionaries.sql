-- 下拉选项字典种子：民族/岗位/技能评级/单价档位（设计：下拉选项从数据库实际数据获取）。
-- 字典按租户隔离；本迁移为全部存量租户补默认项，新租户由 tenant seed 落同样的种子（auth/tenant_seed.go）。
-- code 与 employee 表单存储值对应：nation/position 存中文名（value=name），skill_grade/rate_level 存编码（value=code）。
INSERT INTO dictionary_item(tenant_id,id,dictionary_type,code,name,sort_no,created_at,created_by,created_node,updated_at,updated_by,updated_node)
SELECT t.id, 'dic-'||t.id||'-'||v.dtype||'-'||v.code, v.dtype, v.code, v.name, v.sort,
       (extract(epoch from now())*1000000)::BIGINT,'system','migration',
       (extract(epoch from now())*1000000)::BIGINT,'system','migration'
FROM tenant t CROSS JOIN (VALUES
  ('nation','汉','汉',1),('nation','壮','壮',2),('nation','回','回',3),('nation','苗','苗',4),('nation','瑶','瑶',5),('nation','其他','其他',9),
  ('position','车工','车工',1),('position','裁床师傅','裁床师傅',2),('position','组长','组长',3),('position','质检','质检',4),
  ('position','仓管','仓管',5),('position','整烫工','整烫工',6),('position','生产工艺员','生产工艺员',7),
  ('skill_grade','A','A 熟练',1),('skill_grade','B','B 合格',2),('skill_grade','C','C 学徒',3),
  ('rate_level','high','高档（系数 1.25）',1),('rate_level','mid','中档（系数 1.0）',2),('rate_level','low','低档（系数 0.85）',3)
) AS v(dtype,code,name,sort)
ON CONFLICT DO NOTHING;
