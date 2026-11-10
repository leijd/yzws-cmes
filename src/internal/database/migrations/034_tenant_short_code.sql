-- v0.0.3.1：租户标识（5 位字母或数字），用于生成员工工号前缀

ALTER TABLE tenant ADD COLUMN IF NOT EXISTS short_code TEXT;

DO $$
DECLARE
  rec RECORD;
  base TEXT;
  candidate TEXT;
  alphabet CONSTANT TEXT := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ';
  i INT;
BEGIN
  CREATE TEMP TABLE tmp_tenant_short_code(code TEXT PRIMARY KEY);
  FOR rec IN SELECT id, code FROM tenant WHERE short_code IS NULL ORDER BY created_at, id LOOP
    base := UPPER(SUBSTRING(REGEXP_REPLACE(rec.code, '[^A-Za-z0-9]', '', 'g') FROM 1 FOR 5));
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

ALTER TABLE tenant ALTER COLUMN short_code SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_tenant_short_code ON tenant (short_code);
