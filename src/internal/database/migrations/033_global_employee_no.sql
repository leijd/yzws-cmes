-- v0.0.3.1：员工工号全库唯一；历史重号统一改为 {租户标识}-{6位序号}

WITH base AS (
  SELECT e.tenant_id, e.id, upper(e.employee_no) AS no, e.created_at,
         upper(substring(regexp_replace(t.code, '[^A-Za-z0-9]', '', 'g') from 1 for 5)) AS prefix,
         row_number() OVER (PARTITION BY upper(e.employee_no) ORDER BY e.created_at, e.id) AS rn
  FROM employee e
  JOIN tenant t ON t.id = e.tenant_id
), need AS (
  SELECT tenant_id, id, prefix,
         row_number() OVER (PARTITION BY prefix ORDER BY created_at, id) AS seq
  FROM base
  WHERE rn > 1
)
UPDATE employee e
SET employee_no = COALESCE(NULLIF(n.prefix, ''), 'T') || '-' || lpad(n.seq::text, 6, '0')
FROM need n
WHERE e.tenant_id = n.tenant_id AND e.id = n.id;

-- 若仍有历史重号（极端情况），不阻塞启动；新工号由服务端校验全库唯一。
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM employee GROUP BY upper(employee_no) HAVING count(*) > 1) THEN
    CREATE UNIQUE INDEX IF NOT EXISTS ux_employee_no_global ON employee (upper(employee_no));
  END IF;
END $$;
