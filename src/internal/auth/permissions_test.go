package auth

import "testing"

func TestExpandImpliedPermissions(t *testing.T) {
	perms := map[string]bool{"workshop:write": true, "identity:read": true, "task:assign": true}
	expandImpliedPermissions(perms)
	if !perms["workshop:read"] {
		t.Fatal("workshop:write 应蕴含 workshop:read")
	}
	if !perms["workshop:write"] || !perms["identity:read"] || !perms["task:assign"] {
		t.Fatal("原有权限不应被移除")
	}
	if perms["task:assign:read"] {
		t.Fatal("非 :write 后缀的权限不应被展开")
	}
}
