// 后端服务地址配置
// - H5：留空，走相对路径（同源 /api/v1，由反向代理转发到后端），换服务器或域名都不需要重新打包
// - App（APK）：没有同源概念，必须写死绝对地址
//
// 注意：App 端打包时 uni-app 不会注入 .env 里的变量（会被编译成 {}），
//       所以 APK 的后端地址以 APP_DEFAULT_API_BASE 为准；换服务器改这里再重新出包。
//
// 106.52.168.135 上有两套并行后端，端口不能混用：
//   :18370 = 旧系统（库 clothpm，不含短信/体验/门户等接口，调用会 404）
//   :18390 = 本项目 v0.02（库 yzws-v1，接口齐全）  ← APK 必须用这个
const APP_DEFAULT_API_BASE = "http://106.52.168.135:18390";

let base = (import.meta.env.VITE_API_BASE_URL as string | undefined) || "";

// #ifndef H5
if (!base) base = APP_DEFAULT_API_BASE;
// #endif

export const API_BASE_URL = base;
