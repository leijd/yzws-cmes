import { defineConfig } from "vite";
import uni from "@dcloudio/vite-plugin-uni";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [uni()],
  server: {
    // H5 开发调试时，/api 请求代理到本地后端，避免跨域
    proxy: {
      "/api": {
        target: process.env.VITE_PROXY_TARGET || "http://localhost:18370",
        changeOrigin: true,
      },
    },
  },
});
