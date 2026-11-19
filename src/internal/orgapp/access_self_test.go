package orgapp

import "testing"

func TestOrgScopeSQLSelfOnly(t *testing.T) {
	t.Run("纯self范围按本人过滤而非AND FALSE", func(t *testing.T) {
		a := &Access{SelfOnly: true, SelfEmployeeID: "emp-1"}
		cond, args := a.OrgScopeSQL("e", 1)
		if cond != " AND (e.id=$1)" {
			t.Fatalf("cond = %q, want %q", cond, " AND (e.id=$1)")
		}
		if len(args) != 1 || args[0] != "emp-1" {
			t.Fatalf("args = %v, want [emp-1]", args)
		}
	})

	t.Run("self范围无员工ID仍返回AND FALSE", func(t *testing.T) {
		a := &Access{SelfOnly: true}
		cond, args := a.OrgScopeSQL("e", 1)
		if cond != " AND FALSE" || len(args) != 0 {
			t.Fatalf("cond = %q args = %v, want AND FALSE / no args", cond, args)
		}
	})

	t.Run("team范围仍按组织过滤且叠加self本人", func(t *testing.T) {
		a := &Access{SelfOnly: true, SelfEmployeeID: "emp-1", TeamIDs: []string{"team-1"}}
		cond, args := a.OrgScopeSQL("e", 1)
		want := " AND (e.team_org_id IN ($1))"
		if cond != want {
			t.Fatalf("cond = %q, want %q", cond, want)
		}
		if len(args) != 1 || args[0] != "team-1" {
			t.Fatalf("args = %v, want [team-1]", args)
		}
	})

	t.Run("全厂范围不加条件", func(t *testing.T) {
		a := &Access{All: true}
		cond, args := a.OrgScopeSQL("e", 1)
		if cond != "" || len(args) != 0 {
			t.Fatalf("cond = %q args = %v, want empty", cond, args)
		}
	})
}

func TestScopeFromPermissions(t *testing.T) {
	if got := scopeFromPermissions(map[string]bool{"workshop:read": true, "workshop:write": true}); got != "factory" {
		t.Fatalf("可维护车间的自定义角色应推导为 factory，实际 %q", got)
	}
	if got := scopeFromPermissions(map[string]bool{"workshop:read": true}); got != "factory" {
		t.Fatalf("有查看车间权限的角色应能查看车间列表，实际范围 %q", got)
	}
	if got := scopeFromPermissions(nil); got != "" {
		t.Fatalf("无权限时范围应为空，实际 %q", got)
	}
}

func TestAllowEmployeeOrgSelfOnly(t *testing.T) {
	a := &Access{SelfOnly: true, SelfEmployeeID: "emp-1"}

	if !a.AllowEmployeeOrg(nil, nil, nil, "emp-1") {
		t.Fatal("本人数据应允许访问")
	}
	if a.AllowEmployeeOrg(nil, nil, nil, "emp-2") {
		t.Fatal("他人数据应拒绝访问")
	}
	if a.AllowEmployeeOrg(nil, nil, nil) {
		t.Fatal("缺少员工ID时应拒绝访问")
	}
}
