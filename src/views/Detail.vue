<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import { getMetrics } from '@/api'

const route = useRoute()
const serverId = Number(route.params.id)
const metrics = ref<any[]>([])
const timeRange = ref('24h')
const chartInstances = {} as any

const loadMetrics = async () => {
  try {
    const endTime = new Date().toISOString()
    let startTime: string

    switch (timeRange.value) {
      case '1h':
        startTime = new Date(Date.now() - 3600000).toISOString()
        break
      case '7d':
        startTime = new Date(Date.now() - 7 * 24 * 3600000).toISOString()
        break
      default:
        startTime = new Date(Date.now() - 24 * 3600000).toISOString()
    }

    const result: any = await getMetrics(serverId, { start_time: startTime, end_time: endTime })
    metrics.value = result
    renderCharts()
  } catch (e) {
    ElMessage.error('加载数据失败')
  }
}

const renderCharts = () => {
  if (metrics.value.length === 0) return

  const times = metrics.value.map((m: any) => new Date(m.timestamp).toLocaleTimeString())

  // CPU图表
  renderLineChart('cpu', 'CPU使用率 (%)', times, metrics.value.map((m: any) => m.cpu_usage))

  // 内存图表
  renderLineChart('memory', '内存使用率 (%)', times, metrics.value.map((m: any) => m.memory_usage))

  // 磁盘图表
  renderLineChart('disk', '磁盘使用率 (%)', times, metrics.value.map((m: any) => m.disk_used_percent))

  // 网络流量图表
  renderLineChart('network', '网络流速 (KB/s)', times,
    [
      { name: '入方向', data: metrics.value.map((m: any) => m.network_in) },
      { name: '出方向', data: metrics.value.map((m: any) => m.network_out) }
    ]
  )

  // 延迟图表（取北京电信为例）
  renderLineChart('ping', '网络延迟 (ms) - 北京电信', times, metrics.value.map((m: any) => m.ping_beijing_telecom))
}

const renderLineChart = (key: string, title: string, times: string[], dataOrSeries: any) => {
  const el = document.getElementById(`chart-${key}`)
  if (!el) return

  if (!chartInstances[key]) {
    chartInstances[key] = echarts.init(el)
  }

  const chart = chartInstances[key]
  let series: any[]

  if (Array.isArray(dataOrSeries) && dataOrSeries[0]?.data) {
    series = dataOrSeries.map((s: any) => ({
      name: s.name,
      type: 'line',
      data: s.data,
      smooth: true
    }))
  } else {
    series = [{
      type: 'line',
      data: dataOrSeries,
      smooth: true
    }]
  }

  chart.setOption({
    title: { text: title, left: 'center', textStyle: { fontSize: 14 } },
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'category', data: times },
    yAxis: { type: 'value' },
    series
  })
}

const handleTimeRangeChange = () => {
  loadMetrics()
}

onMounted(() => {
  loadMetrics()

  window.addEventListener('resize', () => {
    Object.values(chartInstances).forEach((chart: any) => chart?.resize())
  })
})
</script>

<template>
  <div class="detail-container">
    <div class="header">
      <el-button @click="$router.back()">← 返回</el-button>
      <h2>服务器详情</h2>
      <div class="time-range">
        <el-radio-group v-model="timeRange" @change="handleTimeRangeChange">
          <el-radio-button label="1h">1小时</el-radio-button>
          <el-radio-button label="24h">24小时</el-radio-button>
          <el-radio-button label="7d">7天</el-radio-button>
        </el-radio-group>
      </div>
    </div>

    <div class="charts-grid">
      <div class="chart-card">
        <div id="chart-cpu" style="width: 100%; height: 300px;"></div>
      </div>
      <div class="chart-card">
        <div id="chart-memory" style="width: 100%; height: 300px;"></div>
      </div>
      <div class="chart-card">
        <div id="chart-disk" style="width: 100%; height: 300px;"></div>
      </div>
      <div class="chart-card">
        <div id="chart-network" style="width: 100%; height: 300px;"></div>
      </div>
      <div class="chart-card full-width">
        <div id="chart-ping" style="width: 100%; height: 300px;"></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.detail-container {
  padding: 20px;
  max-width: 1400px;
  margin: 0 auto;
}

.header {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 20px;
}

.header h2 {
  flex: 1;
  font-size: 20px;
}

.time-range {
  margin-left: auto;
}

.charts-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
}

.chart-card {
  background: #fff;
  border-radius: 8px;
  padding: 15px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.full-width {
  grid-column: 1 / -1;
}

@media (max-width: 768px) {
  .charts-grid {
    grid-template-columns: 1fr;
  }
}
</style>
