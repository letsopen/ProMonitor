<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { getServers, type ServerView } from '@/api'
import { formatNet, pingStats, timeAgo } from '@/utils/format'
import NavBar from '@/components/NavBar.vue'

const router = useRouter()
const servers = ref<ServerView[]>([])
const loading = ref(false)
let timer: number | undefined

async function load() {
  try {
    const r = await getServers()
    servers.value = r.servers || []
  } catch {
    // 轮询静默失败，避免打扰
  }
  loading.value = false
}

function goDetail(id: string) {
  router.push(`/detail/${id}`)
}

function pct(v: number | null): number {
  return v == null ? 0 : Math.min(100, Math.max(0, +v.toFixed(1)))
}

onMounted(() => {
  load()
  timer = window.setInterval(load, 10000) // 每 10s 轮询最新快照
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div>
    <NavBar />
    <div class="container">
      <div class="page-head">
        <h2>服务器监控列表</h2>
        <span class="hint">每 10 秒自动刷新 · 共 {{ servers.length }} 台</span>
      </div>

      <el-table :data="servers" v-loading="loading" stripe style="width: 100%">
        <el-table-column label="名称" min-width="160">
          <template #default="{ row }">
            <a class="srv-name" @click="goDetail(row.id)">{{ row.name || row.id }}</a>
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="IP" min-width="130" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
              {{ row.status === 1 ? '在线' : '离线' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="CPU" width="160">
          <template #default="{ row }">
            <el-progress :percentage="pct(row.cpu)" :color="pct(row.cpu) > 85 ? '#f56c6c' : '#409eff'" />
          </template>
        </el-table-column>
        <el-table-column label="内存" width="160">
          <template #default="{ row }">
            <el-progress :percentage="pct(row.mem)" :color="pct(row.mem) > 85 ? '#f56c6c' : '#67c23a'" />
          </template>
        </el-table-column>
        <el-table-column label="磁盘" width="160">
          <template #default="{ row }">
            <el-progress :percentage="pct(row.disk)" :color="pct(row.disk) > 85 ? '#f56c6c' : '#e6a23c'" />
          </template>
        </el-table-column>
        <el-table-column label="网络(入/出)" min-width="150">
          <template #default="{ row }">
            <span class="net">{{ formatNet(row.net_in) }} / {{ formatNet(row.net_out) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="硬件" min-width="160">
          <template #default="{ row }">
            <span class="hardware">
              {{ row.cpu_cores != null ? row.cpu_cores + 'C' : '-' }} /
              {{ row.mem_total_mb != null ? (row.mem_total_mb / 1024).toFixed(1) + 'G' : '-' }} /
              {{ row.disk_total_gb != null ? row.disk_total_gb + 'G' : '-' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="Ping 均延迟" width="120">
          <template #default="{ row }">
            <span :class="pingStats(row.pings).avg != null && pingStats(row.pings).avg! > 300 ? 'warn' : ''">
              {{ pingStats(row.pings).avg != null ? pingStats(row.pings).avg!.toFixed(0) + ' ms' : '-' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="更新" width="110">
          <template #default="{ row }">
            <span class="muted">{{ timeAgo(row.updated_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="90" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="goDetail(row.id)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && servers.length === 0" description="暂无被控上报，请先部署 Agent" />
    </div>
  </div>
</template>

<style scoped>
.container {
  max-width: 1280px;
  margin: 0 auto;
  padding: 0 20px 40px;
}
.page-head {
  display: flex;
  align-items: baseline;
  gap: 14px;
  margin-bottom: 16px;
}
.page-head h2 {
  font-size: 20px;
  color: #1f2937;
}
.hint {
  color: #909399;
  font-size: 13px;
}
.srv-name {
  color: #409eff;
  cursor: pointer;
  font-weight: 500;
}
.srv-name:hover {
  text-decoration: underline;
}
.net {
  font-size: 13px;
  color: #606266;
}
.hardware {
  font-size: 13px;
  color: #606266;
}
.muted {
  color: #909399;
  font-size: 13px;
}
.warn {
  color: #f56c6c;
  font-weight: 600;
}
</style>
