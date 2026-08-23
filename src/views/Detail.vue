<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import { getServers, getHistory, type AggRow, type ServerView } from '@/api'
import { formatNet, pingStats } from '@/utils/format'
import NavBar from '@/components/NavBar.vue'

const route = useRoute()
const router = useRouter()
const serverId = route.params.id as string

const history = ref<AggRow[]>([])
const current = ref<ServerView | null>(null)
const timeRange = ref('24h')
const serverName = ref(serverId)

const chartEls = ['cpu', 'mem', 'disk', 'net', 'ping']
const charts: Record<string, any> = {}
let timer: number | undefined

const rangeMs: Record<string, number> = {
  '1h': 3600_000,
  '24h': 86_400_000,
  '7d': 7 * 86_400_000,
  '30d': 30 * 86_400_000,
}

async function loadHistory() {
  try {
    const to = new Date()
    const from = new Date(Date.now() - rangeMs[timeRange.value])
    const r = await getHistory(serverId, {
      from: from.toISOString(),
      to: to.toISOString(),
      limit: 4320,
    })
    history.value = r.metrics || []
    renderCharts()
  } catch {
    ElMessage.error('加载历史数据失败')
  }
}

// 当前实时快照（轮询）
async function loadCurrent() {
  try {
    const r = await getServers()
    const s = (r.servers || []).find((x) => x.id === serverId)
    if (s) {
      current.value = s
      if (s.name) serverName.value = s.name
    }
  } catch {
    /* ignore */
  }
}

function renderCharts() {
  if (history.value.length === 0) return
  const times = history.value.map((m) => new Date(m.ts * 1000).toLocaleString('zh-CN', { hour12: false }))

  lineChart('cpu', 'CPU 使用率 (%)', times, history.value.map((m) => +m.cpu_avg.toFixed(2)))
  lineChart('mem', '内存使用率 (%)', times, history.value.map((m) => +m.mem_avg.toFixed(2)))
  lineChart('disk', '磁盘使用率 (%)', times, history.value.map((m) => +m.disk_avg.toFixed(2)))
  lineChart(
    'net',
    '网络流速',
    times,
    [
      { name: '入站', data: history.value.map((m) => +(m.net_in_avg / 1024).toFixed(1)) },
      { name: '出站', data: history.value.map((m) => +(m.net_out_avg / 1024).toFixed(1)) },
    ],
    'KB/s'
  )

  // ping 聚合：每个样本的有效节点 min/avg/max
  const pingMin: number[] = []
  const pingAvg: number[] = []
  const pingMax: number[] = []
  for (const m of history.value) {
    const st = pingStats(m.ping_nodes)
    pingMin.push(st.min ?? 0)
    pingAvg.push(st.avg ?? 0)
    pingMax.push(st.max ?? 0)
  }
  lineChart('ping', 'Ping 延迟 (ms)', times, [
    { name: '最低', data: pingMin },
    { name: '平均', data: pingAvg },
    { name: '最高', data: pingMax },
  ])
}

function lineChart(key: string, title: string, times: string[], data: any, unit = '') {
  const el = document.getElementById(`chart-${key}`)
  if (!el) return
  if (!charts[key]) charts[key] = echarts.init(el)
  const series = Array.isArray(data) && data[0]?.data
    ? data.map((s: any) => ({ name: s.name, type: 'line', smooth: true, showSymbol: false, data: s.data }))
    : [{ type: 'line', smooth: true, showSymbol: false, areaStyle: { opacity: 0.08 }, data }]
  charts[key].setOption(
    {
      title: { text: title, left: 'center', textStyle: { fontSize: 14 } },
      tooltip: { trigger: 'axis' },
      legend: series.length > 1 ? { bottom: 0 } : undefined,
      grid: { left: 50, right: 20, top: 40, bottom: 40 },
      xAxis: { type: 'category', data: times, axisLabel: { fontSize: 10 } },
      yAxis: { type: 'value', name: unit },
      series,
    },
    true
  )
}

const latestPings = () => {
  if (history.value.length === 0) return []
  const last = history.value[history.value.length - 1]
  return (last.ping_nodes || []).map((v, i) => ({
    node: i + 1,
    ms: v == null || v <= 0 || v > 1000 ? null : +v.toFixed(1),
  }))
}

onMounted(() => {
  loadHistory()
  loadCurrent()
  timer = window.setInterval(loadCurrent, 10000)
  window.addEventListener('resize', onResize)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
  window.removeEventListener('resize', onResize)
  Object.values(charts).forEach((c: any) => c?.dispose())
})
function onResize() {
  Object.values(charts).forEach((c: any) => c?.resize())
}

watch(timeRange, loadHistory)
</script>

<template>
  <div>
    <NavBar />
    <div class="container">
      <div class="head">
        <el-button @click="router.push('/')">← 返回</el-button>
        <h2>{{ serverName }}</h2>
        <span class="ip">{{ current?.ip }}</span>
        <div class="range">
          <el-radio-group v-model="timeRange">
            <el-radio-button label="1h">1 小时</el-radio-button>
            <el-radio-button label="24h">24 小时</el-radio-button>
            <el-radio-button label="7d">7 天</el-radio-button>
            <el-radio-button label="30d">30 天</el-radio-button>
          </el-radio-group>
        </div>
      </div>

      <!-- 当前实时快照 -->
      <div class="current" v-if="current">
        <div class="card"><div class="k">CPU</div><div class="v">{{ current.cpu != null ? current.cpu.toFixed(1) + '%' : '-' }}</div></div>
        <div class="card"><div class="k">内存</div><div class="v">{{ current.mem != null ? current.mem.toFixed(1) + '%' : '-' }}</div></div>
        <div class="card"><div class="k">磁盘</div><div class="v">{{ current.disk != null ? current.disk.toFixed(1) + '%' : '-' }}</div></div>
        <div class="card"><div class="k">入站</div><div class="v">{{ formatNet(current.net_in) }}</div></div>
        <div class="card"><div class="k">出站</div><div class="v">{{ formatNet(current.net_out) }}</div></div>
        <div class="card"><div class="k">Ping 均延迟</div><div class="v">{{ pingStats(current.pings).avg != null ? pingStats(current.pings).avg!.toFixed(0) + ' ms' : '-' }}</div></div>
      </div>

      <!-- 历史曲线 -->
      <div class="grid">
        <div class="chart-card"><div :id="`chart-cpu`" class="chart"></div></div>
        <div class="chart-card"><div :id="`chart-mem`" class="chart"></div></div>
        <div class="chart-card"><div :id="`chart-disk`" class="chart"></div></div>
        <div class="chart-card"><div :id="`chart-net`" class="chart"></div></div>
        <div class="chart-card full"><div :id="`chart-ping`" class="chart"></div></div>
      </div>

      <!-- 每节点 Ping 明细 -->
      <div class="ping-table">
        <h3>各节点 Ping 延迟（最新采样）</h3>
        <el-table :data="latestPings()" size="small" max-height="360" style="width: 100%">
          <el-table-column prop="node" label="节点" width="90" />
          <el-table-column label="延迟">
            <template #default="{ row }">
              <span :class="row.ms == null ? 'muted' : row.ms > 300 ? 'warn' : ''">
                {{ row.ms == null ? '无效/超时' : row.ms + ' ms' }}
              </span>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.container {
  max-width: 1400px;
  margin: 0 auto;
  padding: 0 20px 40px;
}
.head {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
}
.head h2 {
  font-size: 20px;
  margin: 0;
}
.head .ip {
  color: #909399;
  font-size: 13px;
}
.range {
  margin-left: auto;
}
.current {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 12px;
  margin-bottom: 18px;
}
.current .card {
  background: #f8fafc;
  border: 1px solid #eef2f7;
  border-radius: 8px;
  padding: 12px;
  text-align: center;
}
.current .k {
  color: #909399;
  font-size: 12px;
  margin-bottom: 6px;
}
.current .v {
  font-size: 18px;
  font-weight: 600;
  color: #1f2937;
}
.grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 16px;
}
.chart-card {
  background: #fff;
  border-radius: 8px;
  padding: 10px;
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.08);
}
.chart-card.full {
  grid-column: 1 / -1;
}
.chart {
  width: 100%;
  height: 300px;
}
.ping-table {
  margin-top: 22px;
}
.ping-table h3 {
  font-size: 16px;
  margin-bottom: 10px;
  color: #1f2937;
}
.muted {
  color: #c0c4cc;
}
.warn {
  color: #f56c6c;
  font-weight: 600;
}
@media (max-width: 900px) {
  .current {
    grid-template-columns: repeat(3, 1fr);
  }
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
