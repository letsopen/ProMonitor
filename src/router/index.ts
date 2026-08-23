import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  {
    path: '/',
    name: 'Home',
    component: () => import('@/views/Home.vue'),
    meta: { title: '服务器列表' },
  },
  {
    path: '/detail/:id',
    name: 'Detail',
    component: () => import('@/views/Detail.vue'),
    meta: { title: '服务器详情' },
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { title: '管理员登录' },
  },
  {
    path: '/admin',
    name: 'Admin',
    component: () => import('@/views/Admin.vue'),
    meta: { title: '管理后台' },
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 体验层守卫：管理页需登录态，否则跳登录（真正鉴权永远在服务端 RequireAdmin 中间件）
router.beforeEach((to) => {
  if (to.path === '/admin' && localStorage.getItem('pm_admin') !== '1') {
    return { path: '/login' }
  }
  return true
})

export default router
