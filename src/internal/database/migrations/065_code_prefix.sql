-- 编码前缀注册表：{类型名拼音首字母}-{5位流水} 自动编码的前缀分配。
-- 前缀为平台级资源（编码类型名全局唯一映射）：某类型先占用拼音首字母，
-- 后续同首字母类型追加数字后缀（车间=CJ，厂检=CJ 被占则 CJ1）。
-- 表无 tenant_id（平台级），catchup 不同步；两库由各自迁移保证一致，运行时新类型经双写注册。
CREATE TABLE IF NOT EXISTS code_prefix (
    prefix     TEXT PRIMARY KEY,
    owner_code TEXT NOT NULL UNIQUE,
    owner_name TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
COMMENT ON TABLE code_prefix IS '自动编码前缀注册：owner_code=编码类型（如 org.workshop），prefix=全局唯一前缀，冲突追加数字后缀';

-- 已知编码类型前缀种子（设计文档未特别规定编码格式的实体；工号/业务单号/租户短码有专门规则，不在此列）
INSERT INTO code_prefix(prefix, owner_code, owner_name, created_at) VALUES
    ('CJ',  'org.workshop',          '车间',   (extract(epoch from now())*1000000)::BIGINT),
    ('CX',  'org.line',              '产线',   (extract(epoch from now())*1000000)::BIGINT),
    ('BZ',  'org.team',              '班组',   (extract(epoch from now())*1000000)::BIGINT),
    ('KH',  'masterdata.customer',   '客户',   (extract(epoch from now())*1000000)::BIGINT),
    ('GYS', 'masterdata.supplier',   '供应商', (extract(epoch from now())*1000000)::BIGINT),
    ('CK',  'masterdata.warehouse',  '仓库',   (extract(epoch from now())*1000000)::BIGINT),
    ('WL',  'masterdata.material',   '物料',   (extract(epoch from now())*1000000)::BIGINT),
    ('ZDX', 'masterdata.dictionary', '字典项', (extract(epoch from now())*1000000)::BIGINT),
    ('CP',  'production.product',    '产品',   (extract(epoch from now())*1000000)::BIGINT),
    ('GX',  'production.process',    '工序',   (extract(epoch from now())*1000000)::BIGINT)
ON CONFLICT DO NOTHING;
