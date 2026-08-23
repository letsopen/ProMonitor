<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getServers, createServer, deleteServer, changePassword, type ServerView } from '@/api'
import { useAuthStore } from '@/stores/auth'
import NavBar from '@/components/NavBar.vue'

const router = useRouter()
const auth = useAuthStore()

const servers = ref<ServerView[]>([])
const addVisible = ref(false)
const addForm = ref({ id: '', name: '', ip: '' })

const pwVisible = ref(false)
const pwForm = ref({ old: '', new: '' })

function requireLogin(): boolean {
  if (!auth.loggedIn) {
    ElMessage.warning('请先登录')
    router.push('/login')
    return false
  }
  return true
}

async function loadServers() {
  try {
    const r = await getServers()
    servers.value = r.servers || []
  } catch {
    ElMessage.error('加载失败')
  }
}

async function submitAdd() {
  if (!addForm.value.id || !addForm.value.name) {
    ElMessage.warning('被控 ID 与名称必填')
    return
  }
  try {
    await createServer({ ...addForm.value })
    ElMessage.success('已添加（被控需以相同 SERVER_ID 上报才会出现数据）')
    addVisible.value = false
    addForm.value = { id: '', name: '', ip: '' }
    loadServers()
  } catch (e: any) {
    if (e?.response?.status === 401) {
      auth.loggedIn = false
      localStorage.removeItem('pm_admin')
      router.push('/login')
    } else {
      ElMessage.error('添加失败')
    }
  }
}

async function handleDelete(row: ServerView) {
  try {
    await ElMessageBox.confirm(`确定删除 "${row.name || row.id}" 吗？该操作同时清除其历史数据。`, '警告', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteServer(row.id)
    ElMessage.success('已删除')
    loadServers()
  } catch (e: any) {
    if (e?.response?.status === 401) {
      router.push('/login')
    } else {
      ElMessage.error('删除失败')
    }
  }
}

async function submitPw() {
  if (!pwForm.value.old || !pwForm.value.new) {
    ElMessage.warning('请填写旧密码与新密码')
    return
  }
  try {
    await changePassword(pwForm.value.old, pwForm.value.new)
    ElMessage.success('密码已修改，请重新登录')
    pwVisible.value = false
    pwForm.value = { old: '', new: '' }
    await auth.logout()
    router.push('/login')
  } catch (e: any) {
    if (e?.response?.status === 401) {
      ElMessage.error('旧密码错误')
    } else {
      ElMessage.error('修改失败')
    }
  }
}

onMounted(() => {
  if (requireLogin()) loadServers()
})
</script>

<template>
  <div>
    <NavBar />
    <div class="container">
      <div class="head">
        <h2>管理后台</h2>
        <div class="actions">
          <el-button type="primary" @click="addVisible = true">添加被控</el-button>
          <el-button @click="pwVisible = true">修改密码</el-button>
        </div>
      </div>

      <el-table :data="servers" stripe style="width: 100%">
        <el-table-column label="名称" min-width="160">
          <template #default="{ row }">{{ row.name || row.id }}</template>
        </el-table-column>
        <el-table-column prop="id" label="被控 ID" min-width="160" />
        <el-table-column prop="ip" label="IP" min-width="130" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
              {{ row.status === 1 ? '在线' : '离线' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="danger" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 添加被控 -->
      <el-dialog v-model="addVisible" title="添加被控服务器" width="480px">
        <el-form label-width="90px">
          <el-form-item label="被控 ID" required>
            <el-input v-model="addForm.id" placeholder="Agent 的 SERVER_ID，需保持一致" />
          </el-form-item>
          <el-form-item label="名称" required>
            <el-input v-model="addForm.name" placeholder="展示名，如 阿里云-杭州" />
          </el-form-item>
          <el-form-item label="IP">
            <el-input v-model="addForm.ip" placeholder="可选，被控公网/内网 IP" />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="addVisible = false">取消</el-button>
          <el-button type="primary" @click="submitAdd">确定</el-button>
        </template>
      </el-dialog>

      <!-- 修改密码 -->
      <el-dialog v-model="pwVisible" title="修改管理员密码" width="420px">
        <el-form label-width="90px">
          <el-form-item label="旧密码" required>
            <el-input v-model="pwForm.old" type="password" show-password />
          </el-form-item>
          <el-form-item label="新密码" required>
            <el-input v-model="pwForm.new" type="password" show-password />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="pwVisible = false">取消</el-button>
          <el-button type="primary" @click="submitPw">确定修改</el-button>
        </template>
      </el-dialog>
    </div>
  </div>
</template>

<style scoped>
.container {
  max-width: 1100px;
  margin: 0 auto;
  padding: 0 20px 40px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.head h2 {
  font-size: 20px;
  color: #1f2937;
}
</style>
