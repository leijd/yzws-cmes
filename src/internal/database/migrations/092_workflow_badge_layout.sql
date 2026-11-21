/* 流程待办拓扑图：给步骤节点增加画布坐标（与产品工序拓扑一致） */
ALTER TABLE workflow_badge_step ADD COLUMN IF NOT EXISTS node_x INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workflow_badge_step ADD COLUMN IF NOT EXISTS node_y INTEGER NOT NULL DEFAULT 0;
