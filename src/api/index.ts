import request from '@/utils/request'

export interface ServerView {
  id: string
  name: string
  ip: string
  status: number // 1=在线 0=离线
  cpu: number | null
  mem: number | null
  disk: number | null
  net_in: number | null
  net_out: number | null
  pings: number[] // 各节点延迟(ms)，无效为 null
  updated_at: string
}

export interface PingNode {
  id: number
  name: string
  ip: string // 仅 IPv4
  port: number // TCP 探测端口
}

export interface AggRow {
  server_id: string
  ts: number // unix 秒
  cpu_avg: number
  mem_avg: number
  disk_avg: number
  net_in_avg: number
  net_out_avg: number
  ping_nodes: number[]
}

export const getServers = () =>
  request.get('/api/servers') as Promise<{ servers: ServerView[] }>

export const getHistory = (
  id: string,
  params: { from?: string; to?: string; limit?: number }
) => request.get(`/api/servers/${id}/metrics`, { params }) as Promise<{ metrics: AggRow[] }>

export const login = (username: string, password: string) =>
  request.post('/api/admin/login', { username, password }) as Promise<{ ok: boolean }>

export const logout = () =>
  request.post('/api/admin/logout') as Promise<{ ok: boolean }>

export const changePassword = (oldP: string, newP: string) =>
  request.post('/api/admin/change-password', { old: oldP, new: newP }) as Promise<{ ok: boolean }>

export const createServer = (data: { id: string; name: string; ip: string }) =>
  request.post('/api/admin/servers', data) as Promise<{ ok: boolean }>

export const updateServer = (id: string, data: { name: string; ip: string }) =>
  request.put(`/api/admin/servers/${id}`, data) as Promise<{ ok: boolean }>

export const deleteServer = (id: string) =>
  request.delete(`/api/admin/servers/${id}`) as Promise<{ ok: boolean }>

// --- Ping 节点管理（主控统一维护，存库） ---

export const getPingNodes = () =>
  request.get('/api/admin/ping-nodes') as Promise<{ nodes: PingNode[] }>

export const createPingNode = (data: { name: string; ip: string; port: number }) =>
  request.post('/api/admin/ping-nodes', data) as Promise<{ ok: boolean; id: number }>

export const updatePingNode = (id: number, data: { name: string; ip: string; port: number }) =>
  request.put(`/api/admin/ping-nodes/${id}`, data) as Promise<{ ok: boolean }>

export const deletePingNode = (id: number) =>
  request.delete(`/api/admin/ping-nodes/${id}`) as Promise<{ ok: boolean }>
