import { defineStore } from 'pinia'
import { ref } from 'vue'
import { login as apiLogin, logout as apiLogout } from '@/api'

// 登录态仅前端体验层标记；真正鉴权永远在服务端（httpOnly Cookie）。
export const useAuthStore = defineStore('auth', () => {
  const loggedIn = ref(localStorage.getItem('pm_admin') === '1')

  async function login(username: string, password: string) {
    await apiLogin(username, password)
    loggedIn.value = true
    localStorage.setItem('pm_admin', '1')
  }

  async function logout() {
    try {
      await apiLogout()
    } catch {
      // 忽略后端错误，前端照常清除
    }
    loggedIn.value = false
    localStorage.removeItem('pm_admin')
  }

  return { loggedIn, login, logout }
})
