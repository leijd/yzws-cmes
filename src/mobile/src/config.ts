// 后端服务地址配置
// - H5：留空，走相对路径（同源 /api/v1，由反向代理转发到后端），换服务器或域名都不需要重新打包
// - App（APK）：没有同源概念，必须写死绝对地址
//
// 注意：App 端打包时 uni-app 不会注入 .env 里的变量（会被编译成 {}），
//       所以 APK 的后端地址以 APP_DEFAULT_API_BASE 为准；换服务器改这里再重新出包。
//
// 所有环境（开发/生产）后端统一监听 18390；旧系统 clothpm（18370）已停用。
const APP_DEFAULT_API_BASE = "http://106.52.168.135:18390";

let base = (import.meta.env.VITE_API_BASE_URL as string | undefined) || "";

// #ifndef H5
if (!base) base = APP_DEFAULT_API_BASE;
// #endif

export const API_BASE_URL = base;
