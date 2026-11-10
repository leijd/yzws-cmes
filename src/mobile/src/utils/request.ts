import { API_BASE_URL } from "../config";

export interface ApiErrorBody {
  code: string;
  message: string;
  request_id?: string;
  details?: Record<string, unknown>;
}

export interface ApiError extends Error {
  code?: string;
  status?: number;
}

const TOKEN_KEY = "clothp_access_token";

export function getToken(): string {
  return (uni.getStorageSync(TOKEN_KEY) as string) || "";
}

export function setToken(token: string): void {
  uni.setStorageSync(TOKEN_KEY, token);
}

export function clearToken(): void {
  uni.removeStorageSync(TOKEN_KEY);
}

interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
  data?: unknown;
  /** 出错时不弹 toast */
  silent?: boolean;
  /** 401 时不自动跳登录页（登录接口本身用） */
  skipAuthRedirect?: boolean;
  /** 携带 Idempotency-Key 头（后端写接口要求） */
  idempotency?: boolean;
}

function uuid(): string {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === "x" ? r : (r & 0x3) | 0x8).toString(16);
  });
}

export function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  return new Promise((resolve, reject) => {
    const header: Record<string, string> = {
      "X-Request-Id": `mobile-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    };
    const token = getToken();
    if (token) header.Authorization = `Bearer ${token}`;
    if (options.data !== undefined) header["Content-Type"] = "application/json";
    if (options.idempotency) header["Idempotency-Key"] = uuid();

    uni.request({
      url: API_BASE_URL + "/api/v1" + path,
      method: (options.method || "GET") as "GET",
      data: options.data as never,
      header,
      success: (res) => {
        const body = (res.data ?? {}) as T & ApiErrorBody;
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(body as T);
          return;
        }
        if (res.statusCode === 401 && !options.skipAuthRedirect) {
          clearToken();
          uni.reLaunch({ url: "/pages/login/login" });
        }
        const error = new Error(
          body.message || `请求失败 (${res.statusCode})`
        ) as ApiError;
        error.code = body.code;
        error.status = res.statusCode;
        if (!options.silent) {
          uni.showToast({ title: error.message, icon: "none" });
        }
        reject(error);
      },
      fail: (err) => {
        if (!options.silent) {
          uni.showToast({ title: "网络异常，请检查网络连接", icon: "none" });
        }
        reject(new Error(err.errMsg || "网络异常"));
      },
    });
  });
}
