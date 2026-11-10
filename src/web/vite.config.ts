import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { loadEnv } from 'vite'

/**
 * 前端所有 API 请求都是相对路径 /api/v1...（见 src/api.ts），后端端口只在两处体现：
 * 1) 开发模式：Vite 代理目标，可通过环境变量指定（优先级：shell 环境变量 > .env 文件 > 默认值）：
 *      PowerShell:  $env:API_TARGET='http://localhost:18390'; npm run dev
 *      或在 web/.env.local 写  API_TARGET=http://localhost:18390
 * 2) 生产模式：同域部署/反向代理，前端不感知后端端口，无需任何配置。
 */
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = process.env.API_TARGET || env.API_TARGET || 'http://localhost:18370'
  return {
    plugins: [vue()],
    server: { port: 18379, proxy: { '/api': apiTarget } },
  }
})
