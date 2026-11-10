// 后端服务地址配置
// - H5 开发调试：使用相对路径，走 vite 代理（vite.config.ts 中配置 target）
// - Android 真机/模拟器调试：必须配置 VITE_API_BASE_URL 为电脑的局域网 IP，
//   例如 VITE_API_BASE_URL=http://192.168.1.10:18370
// - 生产环境：配置为已备案的 HTTPS 域名
let base = (import.meta.env.VITE_API_BASE_URL as string | undefined) || "";

// #ifndef H5
if (!base) base = "http://localhost:18370";
// #endif

export const API_BASE_URL = base;
