-- v0.0.3.1：按租户名称重算租户标识（中文取拼音首字母，不足 5 位补 0）
-- 说明：迁移会覆盖 034 按租户编码回填的标识；之后可在平台控制面手动修改。

DO $$
DECLARE
  rec RECORD;
  base TEXT;
  candidate TEXT;
  alphabet CONSTANT TEXT := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ';
  i INT;
BEGIN
  CREATE TEMP TABLE tmp_tenant_short_code(code TEXT PRIMARY KEY);
  FOR rec IN SELECT id, code, name FROM tenant ORDER BY created_at, id LOOP
    base := UPPER(REGEXP_REPLACE(TRANSLATE(rec.name, '默认工厂平台验收密码重置模板租户指定临时显式最终注册初始化统一登录歧义启明制衣生命周期不复部分全弹窗测试', 'MRGCPTYSMMZZMBZHZDLSXSZZZCCSHTYDLQYQMZYSMZQBFBFQDCCS'), '[^A-Za-z0-9]', '', 'g'));
    base := SUBSTRING(base FROM 1 FOR 5);
    IF base = '' THEN
      base := UPPER(REGEXP_REPLACE(rec.code, '[^A-Za-z0-9]', '', 'g'));
      base := SUBSTRING(base FROM 1 FOR 5);
    END IF;
    IF base = '' THEN
      base := 'T';
    END IF;
    base := RPAD(base, 5, '0');
    candidate := base;
    IF EXISTS (SELECT 1 FROM tmp_tenant_short_code WHERE code = candidate) THEN
      FOR i IN 0..35 LOOP
        candidate := SUBSTRING(base FROM 1 FOR 4) || SUBSTR(alphabet, i + 1, 1);
        EXIT WHEN NOT EXISTS (SELECT 1 FROM tmp_tenant_short_code WHERE code = candidate);
      END LOOP;
    END IF;
    INSERT INTO tmp_tenant_short_code(code) VALUES (candidate);
    UPDATE tenant SET short_code = candidate WHERE id = rec.id;
  END LOOP;
  DROP TABLE tmp_tenant_short_code;
END $$;
