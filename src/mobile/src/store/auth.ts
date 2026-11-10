import { reactive } from "vue";
import { request, getToken, setToken, clearToken } from "../utils/request";

export interface UserInfo {
  id: string;
  tenant_id: string;
  employee_id?: string;
  login_name: string;
  employee_name?: string;
}

interface LoginResult {
  account_type: "tenant" | "platform";
  access_token: string;
  refresh_token?: string;
  password_change_required?: boolean;
  permissions?: string[];
}

export const auth = reactive({
  user: null as UserInfo | null,
  permissions: {} as Record<string, boolean>,

  get hasSession(): boolean {
    return !!getToken();
  },

  hasPerm(code: string): boolean {
    return !!this.permissions[code];
  },

  async login(loginName: string, password: string): Promise<void> {
    const result = await request<LoginResult>("/auth/unified-login", {
      method: "POST",
      data: { login_name: loginName, password, node_id: "mobile-app" },
      silent: true,
      skipAuthRedirect: true,
    });
    if (result.account_type !== "tenant") {
      throw new Error("平台管理账号请使用网页端登录");
    }
    setToken(result.access_token);
    await Promise.all([this.fetchMe(), this.fetchPermissions()]);
  },

  async fetchMe(): Promise<void> {
    const res = await request<{ user: UserInfo }>("/me", { silent: true });
    this.user = res.user;
  },

  async fetchPermissions(): Promise<void> {
    try {
      const res = await request<{ actions: Record<string, boolean> }>(
        "/me/permissions",
        { silent: true }
      );
      this.permissions = res.actions || {};
    } catch {
      this.permissions = {};
    }
  },

  async logout(): Promise<void> {
    try {
      await request("/auth/logout", { method: "POST", silent: true });
    } catch {
      // 网络失败也继续清理本地会话
    }
    clearToken();
    this.user = null;
    this.permissions = {};
    uni.reLaunch({ url: "/pages/login/login" });
  },
});
