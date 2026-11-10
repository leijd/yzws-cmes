import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './styles.css'

const app = createApp(App).use(router)

/* 等首次路由解析完成再挂载：否则 App 初始化时 route.meta 为空，
   会把 /portal、/login 等独立页误判为业务页去请求 /me 等接口 */
router.isReady().then(() => app.mount('#app'))
