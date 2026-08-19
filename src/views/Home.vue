<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getServers, createServer, deleteServer } from '@/api'

const router = useRouter()
const servers = ref<any[]>([])
const dialogVisible = ref(false)
const form = ref({
  name: '',
  provider: '',
  billing_cycle: 'monthly',
  price: 0
})
const generatedSecret = ref('')

const loadServers = async () => {
  try {
    const result: any = await getServers()
    servers.value = result
  } catch (e) {
    ElMessage.error('加载服务器列表失败')
  }
}

const handleAdd = () => {
  form.value = { name: '', provider: '', billing_cycle: 'monthly', price: 0 }
  generatedSecret.value = ''
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!form.value.name) {
    ElMessage.warning('请输入服务器名称')
    return
  }
  try {
    const result: any = await createServer(form.value)
    generatedSecret.value = result.shared_secret
    ElMessage.success('服务器添加成功，请保存密钥')
    loadServers()
  } catch (e) {
    ElMessage.error('添加失败')
  }
}

const handleDelete = async (id: number, name: string) => {
  try {
    await ElMessageBox.confirm(`确定要删除 "${name}" 吗？`, '警告', {
      type: 'warning'
    })
    await deleteServer(id)
    ElMessage.success('删除成功')
    loadServers()
  } catch (e) {
    // 用户取消
  }
}

const goToDetail = (id: number) => {
  router.push(`/detail/${id}`)
}

const formatStatus = (status: string) => {
  return status === 'online' ? '在线' : '离线'
}

const formatTime = (time: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

onMounted(() => {
  loadServers()
})
</script>

<template>
  <div class="home-container">
    <div class="header">
      <h1>服务器性能监控</h1>
      <el-button type="primary" @click="handleAdd">添加被控</el-button>
    </div>

    <el-table :data="servers" style="width: 100%" stripe>
      <el-table-column prop="name" label="名称" width="150" />
      <el-table-column prop="provider" label="服务商" width="120" />
      <el-table-column label="付费周期" width="100">
        <template #default="{ row }">
          {{ row.billing_cycle === 'monthly' ? '月付' : row.billing_cycle === 'quarterly' ? '季付' : '年付' }}
        </template>
      </el-table-column>
      <el-table-column prop="price" label="价格(元)" width="100" />
      <el-table-column label="状态" width="80">
        <template #default="{ row }">
          <el-tag :type="row.status === 'online' ? 'success' : 'info'">
            {{ formatStatus(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="最后上报" width="180">
        <template #default="{ row }">
          {{ formatTime(row.last_seen) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" fixed="right" width="200">
        <template #default="{ row }">
          <el-button size="small" @click="goToDetail(row.id)">详情</el-button>
          <el-button size="small" type="danger" @click="handleDelete(row.id, row.name)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 添加服务器对话框 -->
    <el-dialog v-model="dialogVisible" title="添加被控服务器" width="500px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="服务器名称" required>
          <el-input v-model="form.name" placeholder="例如: 阿里云-杭州" />
        </el-form-item>
        <el-form-item label="服务商">
          <el-input v-model="form.provider" placeholder="例如: 阿里云、腾讯云" />
        </el-form-item>
        <el-form-item label="付费周期">
          <el-select v-model="form.billing_cycle" style="width: 100%">
            <el-option label="月付" value="monthly" />
            <el-option label="季付" value="quarterly" />
            <el-option label="年付" value="yearly" />
          </el-select>
        </el-form-item>
        <el-form-item label="价格(元)">
          <el-input-number v-model="form.price" :min="0" :precision="2" style="width: 100%" />
        </el-form-item>
      </el-form>

      <div v-if="generatedSecret" class="secret-box">
        <p><strong>预共享密钥（请妥善保存）：</strong></p>
        <el-input v-model="generatedSecret" readonly />
      </div>

      <template #footer>
        <el-button @click="dialogVisible = false">关闭</el-button>
        <el-button v-if="!generatedSecret" type="primary" @click="handleSubmit">生成</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.home-container {
  padding: 20px;
  max-width: 1200px;
  margin: 0 auto;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header h1 {
  font-size: 24px;
  color: #333;
}

.secret-box {
  margin-top: 20px;
  padding: 15px;
  background: #f5f7fa;
  border-radius: 4px;
}

.secret-box p {
  margin-bottom: 10px;
  color: #e6a23c;
}
</style>
