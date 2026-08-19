import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  {
    path: '/',
    name: 'Home',
    component: () => import('@/views/Home.vue'),
    meta: { title: '服务器列表' }
  },
  {
    path: '/detail/:id',
    name: 'Detail',
    component: () => import('@/views/Detail.vue'),
    meta: { title: '服务器详情' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

export default router
