<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getServers, createServer, deleteServer, changePassword,
  getPingNodes, createPingNode, updatePingNode, deletePingNode,
  type ServerView, type PingNode,
} from '@/api'
import { useAuthStore } from '@/stores/auth'
import NavBar from '@/components/NavBar.vue'

const router = useRouter()
const auth = useAuthStore()

const activeTab = ref('servers')

// --- 被控管理 ---
const servers = ref<ServerView[]>([])
const addVisible = ref(false)
const addForm = ref({ id: '', name: '', ip: '' })

// --- 延迟节点管理 ---
const nodes = ref<PingNode[]>([])
const nodeVisible = ref(false)
const nodeEditing = ref(false)
const nodeForm = ref({ name: '', ip: '', port: 80 })

// --- 修改密码 ---
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

async function loadNodes() {
  try {
    const r = await getPingNodes()
    nodes.value = r.nodes || []
  } catch (e: any) {
    if (e?.response?.status === 401) {
      auth.loggedIn = false
      localStorage.removeItem('pm_admin')
      router.push('/login')
    } else {
      ElMessage.error('加载延迟节点失败')
    }
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

// --- 延迟节点操作 ---

const nodeEditingId = ref(0)

function openNodeDialog(row?: PingNode) {
  if (row) {
    nodeEditing.value = true
    nodeEditingId.value = row.id
    nodeForm.value = { name: row.name, ip: row.ip, port: row.port }
  } else {
    nodeEditing.value = false
    nodeEditingId.value = 0
    nodeForm.value = { name: '', ip: '', port: 80 }
  }
  nodeVisible.value = true
}

function validateIPv4(ip: string): boolean {
  const parts = ip.split('.')
  if (parts.length !== 4) return false
  return parts.every((p) => {
    if (!/^\d{1,3}$/.test(p)) return false
    const n = Number(p)
    return n >= 0 && n <= 255
  })
}

async function submitNode() {
  const f = nodeForm.value
  if (!f.name.trim() || !f.ip.trim()) {
    ElMessage.warning('名称与 IP 必填')
    return
  }
  if (!validateIPv4(f.ip.trim())) {
    ElMessage.warning('IP 必须是合法的 IPv4 地址（仅支持 V4）')
    return
  }
  if (!f.port || f.port < 1 || f.port > 65535) {
    ElMessage.warning('端口须在 1-65535 之间')
    return
  }
  try {
    if (nodeEditing.value) {
      await updatePingNode(nodeEditingId.value, { ...f })
    } else {
      await createPingNode({ ...f })
    }
    ElMessage.success(nodeEditing.value ? '已更新' : '已添加')
    nodeVisible.value = false
    loadNodes()
  } catch (e: any) {
    if (e?.response?.status === 401) {
      router.push('/login')
    } else {
      ElMessage.error('保存失败')
    }
  }
}

async function handleDeleteNode(row: PingNode) {
  try {
    await ElMessageBox.confirm(`确定删除延迟节点 "${row.name}" (${row.ip}) 吗？`, '警告', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deletePingNode(row.id)
    ElMessage.success('已删除')
    loadNodes()
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
          <el-button @click="pwVisible = true">修改密码</el-button>
        </div>
      </div>

      <el-tabs v-model="activeTab" @tab-change="(name: string) => { if (name === 'nodes') loadNodes() }">
        <!-- 被控管理 -->
        <el-tab-pane label="被控管理" name="servers">
          <div class="pane-head">
            <el-button type="primary" @click="addVisible = true">添加被控</el-button>
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
        </el-tab-pane>

        <!-- 延迟节点管理 -->
        <el-tab-pane label="延迟节点" name="nodes">
          <div class="pane-head">
            <el-button type="primary" @click="openNodeDialog()">添加节点</el-button>
            <span class="hint">节点清单由主控统一维护，被控通过 /api/ping-config 拉取（每 5 分钟热更新）</span>
          </div>
          <el-table :data="nodes" stripe style="width: 100%">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="name" label="名称" min-width="140" />
            <el-table-column prop="ip" label="IP" min-width="130" />
            <el-table-column prop="port" label="端口(TCP)" width="110" />
            <el-table-column label="操作" width="140" fixed="right">
              <template #default="{ row }">
                <el-button size="small" @click="openNodeDialog(row)">编辑</el-button>
                <el-button size="small" type="danger" @click="handleDeleteNode(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>

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

      <!-- 添加/编辑延迟节点 -->
      <el-dialog v-model="nodeVisible" :title="nodeEditing ? '编辑延迟节点' : '添加延迟节点'" width="460px">
        <el-form label-width="110px">
          <el-form-item label="名称" required>
            <el-input v-model="nodeForm.name" placeholder="如 阿里DNS / 腾讯DNS" />
          </el-form-item>
          <el-form-item label="IP" required>
            <el-input v-model="nodeForm.ip" placeholder="仅支持 IPv4，如 223.5.5.5" />
          </el-form-item>
          <el-form-item label="TCP 端口" required>
            <el-input-number v-model="nodeForm.port" :min="1" :max="65535" />
            <span class="hint">PING_TYPE=tcp 时探测该端口（如 80/443）</span>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="nodeVisible = false">取消</el-button>
          <el-button type="primary" @click="submitNode">确定</el-button>
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
.pane-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.hint {
  font-size: 12px;
  color: #9ca3af;
}
</style>
