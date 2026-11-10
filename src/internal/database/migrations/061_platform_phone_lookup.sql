-- 平台侧手机号归属受控查询权限点（客服核实"报来的手机号是否注册用户"）。
-- 口径：只返回归属（厂/工号/姓名/身份），不返回完整手机号；每次查询强制平台审计。
INSERT INTO platform_permission(code,module,name,kind,description,sort_no,admin_only)
VALUES ('phone.lookup','安全审计','手机号归属查询','action','核实报来的手机号是否为本系统用户（任何身份）；返回归属不返回完整号码，全量审计',960,FALSE)
ON CONFLICT DO NOTHING;
