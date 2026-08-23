<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ElMessage } from 'element-plus'

const router = useRouter()
const auth = useAuthStore()

async function doLogout() {
  await auth.logout()
  ElMessage.success('已退出登录')
  router.push('/login')
}
</script>

<template>
  <div class="navbar">
    <div class="brand" @click="router.push('/')">
      <span class="logo">📡</span> ProMonitor 服务器探针
    </div>
    <div class="links">
      <el-link type="primary" :underline="false" @click="router.push('/')">监控列表</el-link>
      <el-link v-if="auth.loggedIn" type="primary" :underline="false" @click="router.push('/admin')">管理后台</el-link>
      <el-link v-if="auth.loggedIn" type="info" :underline="false" @click="doLogout">退出</el-link>
      <el-link v-else type="primary" :underline="false" @click="router.push('/login')">登录</el-link>
    </div>
  </div>
</template>

<style scoped>
.navbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 56px;
  padding: 0 24px;
  background: #1f2937;
  color: #fff;
  margin-bottom: 20px;
}
.brand {
  font-size: 18px;
  font-weight: 600;
  cursor: pointer;
  user-select: none;
}
.logo {
  margin-right: 6px;
}
.links {
  display: flex;
  gap: 18px;
  align-items: center;
}
.links :deep(.el-link) {
  color: #cbd5e1;
  font-size: 14px;
}
.links :deep(.el-link:hover) {
  color: #fff;
}
</style>
