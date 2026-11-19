-- 生产现场摄像跟踪与证据存证域（设计 4.26）：摄像设备、证据片段、导出审计。
-- 数据库只存元数据与哈希，原始录像存 NVR/对象存储；封存证据只读防篡改。
-- 幂等：全部 IF NOT EXISTS / ON CONFLICT DO NOTHING。

CREATE TABLE IF NOT EXISTS camera_device (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  code TEXT NOT NULL, name TEXT NOT NULL,
  protocol TEXT NOT NULL DEFAULT 'rtsp' CHECK(protocol IN ('rtsp','gb28181','onvif')),
  channel_no TEXT, stream_ref TEXT,
  bind_org_id TEXT, bind_workstation TEXT, location_desc TEXT,
  no_recording_zone BOOLEAN NOT NULL DEFAULT FALSE,
  retention_days INTEGER NOT NULL DEFAULT 30,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','offline','disabled')),
  last_heartbeat_at BIGINT, remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE INDEX IF NOT EXISTS ix_camera_org ON camera_device(tenant_id,bind_org_id,status);

-- 证据片段表：source_type=camera 固定摄像头 / mobile 手机拍摄
CREATE TABLE IF NOT EXISTS evidence_clip (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  seal_no TEXT,
  source_type TEXT NOT NULL DEFAULT 'camera' CHECK(source_type IN ('camera','mobile')),
  camera_id TEXT,
  captured_by TEXT, capture_at BIGINT, device_info TEXT, gps_location TEXT,
  biz_type TEXT NOT NULL CHECK(biz_type IN ('quality','shipment','rework','payroll_dispute','security','outsourcing','general')),
  biz_id TEXT,
  start_at BIGINT, end_at BIGINT,
  capture_mode TEXT NOT NULL DEFAULT 'manual' CHECK(capture_mode IN ('manual','event','scheduled')),
  storage_key TEXT NOT NULL DEFAULT '',
  attachment_id TEXT,
  duration_sec INTEGER, file_size BIGINT,
  sha256 TEXT, watermark_text TEXT,
  status TEXT NOT NULL DEFAULT 'capturing' CHECK(status IN ('capturing','sealed','archived','expired')),
  sealed_by TEXT, sealed_at BIGINT, retention_until BIGINT, remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  UNIQUE(tenant_id,seal_no),
  FOREIGN KEY(tenant_id,camera_id) REFERENCES camera_device(tenant_id,id),
  CHECK((source_type='camera' AND camera_id IS NOT NULL) OR (source_type='mobile' AND captured_by IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS ix_evidence_biz ON evidence_clip(tenant_id,biz_type,biz_id);
CREATE INDEX IF NOT EXISTS ix_evidence_camera ON evidence_clip(tenant_id,camera_id,start_at);

-- 证据导出审计（一次性限时下载票据）
CREATE TABLE IF NOT EXISTS evidence_export_log (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  evidence_id TEXT NOT NULL,
  exported_by TEXT NOT NULL, ticket_hash TEXT NOT NULL,
  expires_at BIGINT NOT NULL, consumed_at BIGINT,
  created_at BIGINT NOT NULL, created_node TEXT NOT NULL,
  PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,evidence_id) REFERENCES evidence_clip(tenant_id,id)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_evidence_ticket ON evidence_export_log(tenant_id,ticket_hash);

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('video:view','巡厂与证据','查看现场画面','read','实时巡厂、录像时间轴与证据列表查看',FALSE,113),
('video:manage','巡厂与证据','维护摄像设备','write','摄像头建档、绑定与停用',FALSE,114),
('evidence:seal','巡厂与证据','证据封存','write','手机拍摄存证、录像片段封存与业务单据绑定',FALSE,115),
('evidence:export','巡厂与证据','证据导出','action','导出封存证据片段与证据清单（一次性限时下载）',FALSE,116)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-evidence',NULL,'menu','menu.evidence','巡厂与证据','/evidence','video:view',113,1),
('mod-video-manage','mod-menu-evidence','button','button.video.manage','维护摄像设备',NULL,'video:manage',114,1),
('mod-evidence-seal','mod-menu-evidence','button','button.evidence.seal','证据封存',NULL,'evidence:seal',115,1),
('mod-evidence-export','mod-menu-evidence','button','button.evidence.export','证据导出',NULL,'evidence:export',116,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
WHERE p.code IN ('video:view','video:manage','evidence:seal','evidence:export')
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin'
  AND p.code IN ('video:view','video:manage','evidence:seal','evidence:export')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-fm-ev1','prt-factory-manager','video:view',1),
('prtp-fm-ev2','prt-factory-manager','video:manage',1),
('prtp-fm-ev3','prt-factory-manager','evidence:seal',1),
('prtp-fm-ev4','prt-factory-manager','evidence:export',1),
('prtp-wh-ev1','prt-warehouse','evidence:seal',1),
('prtp-wh-ev2','prt-warehouse','video:view',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;
