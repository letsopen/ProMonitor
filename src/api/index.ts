import request from '@/utils/request'

// 服务器管理
export const getServers = () => request.get('/api/servers')
export const createServer = (data: {
  name: string
  provider: string
  billing_cycle: string
  price: number
}) => request.post('/api/servers', data)
export const deleteServer = (id: number) => request.delete(`/api/servers/${id}`)

// 指标数据
export const getMetrics = (serverId: number, params: any) =>
  request.get(`/api/metrics/${serverId}`, { params })
export const getLatestMetric = (serverId: number) =>
  request.get(`/api/metrics/${serverId}/latest`)
