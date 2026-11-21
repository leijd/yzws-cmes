-- 产品工序拓扑图（DAG）：工序节点之间的串行/并行/汇合边。
-- from_id / to_id 均为 product_process.id（产品版本内的工序实例），随产品版本快照。
CREATE TABLE IF NOT EXISTS product_process_edge (
  tenant_id       TEXT    NOT NULL,
  id              TEXT    NOT NULL,
  product_id      TEXT    NOT NULL,
  product_version BIGINT  NOT NULL,
  from_id         TEXT    NOT NULL,
  to_id           TEXT    NOT NULL,
  created_at      BIGINT,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, product_id, product_version, from_id, to_id)
);
CREATE INDEX IF NOT EXISTS idx_ppe_product ON product_process_edge(tenant_id, product_id, product_version);

-- sequence_no 仅作显示/兼容排序，不再强制唯一：并行分支可共享同一层级序号
ALTER TABLE product_process DROP CONSTRAINT IF EXISTS product_process_tenant_id_product_id_product_version_sequence_no_key;

-- 工序节点在拓扑画布上的坐标（毫米/逻辑像素），供图形化布局持久化
ALTER TABLE product_process ADD COLUMN IF NOT EXISTS node_x INTEGER NOT NULL DEFAULT 0;
ALTER TABLE product_process ADD COLUMN IF NOT EXISTS node_y INTEGER NOT NULL DEFAULT 0;

-- 工单快照记录每道工序的直接前驱节点 id 列表（product_process.id），报工前道校验据此判定
ALTER TABLE work_order_process_snapshot ADD COLUMN IF NOT EXISTS prev_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
