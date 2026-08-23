// ProMonitor 后端冒烟测试（需先启动 server，环境变量见 README）
// 用法: node scripts/smoke.mjs
import crypto from 'crypto'
const BASE = process.env.SMOKE_URL || 'http://127.0.0.1:9000'
const SECRET = process.env.HMAC_SECRET || 'testsecret'
const ADMIN = process.env.ADMIN_USER || 'admin'
const PASS = process.env.ADMIN_PASS || 'admin123'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function hmac(body) {
  return crypto.createHmac('sha256', SECRET).update(body).digest('hex')
}
const j = (o) => JSON.stringify(o)
const ok = (cond, msg) => { console.log((cond ? 'PASS ' : 'FAIL ') + msg); if (!cond) process.exitCode = 1 }

async function main() {
  // 1) 被控上报（HMAC 验签）
  const sample = {
    server_id: 'srv-001', name: 'web-01', ip: '10.0.0.5',
    cpu: 33.2, mem: 55.1, disk: 70.0, net_in: 12.3, net_out: 4.5,
    ping_nodes: [23.1, -1, 999.0, 45.6], ts: Math.floor(Date.now() / 1000),
  }
  const body = j(sample)
  const sig = hmac(body)
  let r = await fetch(`${BASE}/api/ingest`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Signature': sig }, body,
  })
  ok(r.status === 202, `ingest valid signature -> ${r.status}`)

  // 验签失败应 401
  r = await fetch(`${BASE}/api/ingest`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Signature': 'deadbeef' }, body,
  })
  ok(r.status === 401, `ingest bad signature -> ${r.status}`)

  // 2) 首次上报即注册：等待异步 upsert 后，匿名列表应含 srv-001
  await sleep(900)
  r = await fetch(`${BASE}/api/servers`)
  ok(r.status === 200, `anonymous GET /api/servers -> ${r.status}`)
  let data = await r.json()
  ok(Array.isArray(data.servers) && data.servers.some((s) => s.id === 'srv-001'), 'anonymous list contains srv-001 (auto-registered)')

  // 2b) 公开端点 /api/ping-config 返回探测配置（被控拉取用）
  r = await fetch(`${BASE}/api/ping-config`)
  ok(r.status === 200, `anonymous GET /api/ping-config -> ${r.status}`)
  data = await r.json()
  ok(['icmp', 'tcp'].includes(data.method) && Array.isArray(data.nodes), 'ping-config shape valid (method/nodes)')

  // 3) 未登录访问管理写操作应 401（创建被控在 /api/admin/servers 鉴权组下）
  r = await fetch(`${BASE}/api/admin/servers`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: j({ id: 'x', name: 'x', ip: '1.1.1.1' }),
  })
  ok(r.status === 401, `unauth create server -> ${r.status}`)

  // 4) 登录
  r = await fetch(`${BASE}/api/admin/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: j({ username: ADMIN, password: PASS }),
  })
  ok(r.status === 200, `admin login -> ${r.status}`)
  const cookie = r.headers.get('set-cookie')
  ok(!!cookie, 'login returned session cookie')
  const jar = { 'Cookie': cookie }

  // 5) 登录后创建被控
  r = await fetch(`${BASE}/api/admin/servers`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...jar },
    body: j({ id: 'srv-002', name: 'db-01', ip: '10.0.0.9' }),
  })
  ok(r.status === 200, `authed create server -> ${r.status}`)

  // 6) 登录后列表应含两个
  r = await fetch(`${BASE}/api/servers`, { headers: jar })
  data = await r.json()
  ok(data.servers.filter((s) => ['srv-001', 'srv-002'].includes(s.id)).length === 2, 'authed list has 2 servers')

  // 7) 改密
  r = await fetch(`${BASE}/api/admin/change-password`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...jar },
    body: j({ old: PASS, new: 'newpass123' }),
  })
  ok(r.status === 200, `change password -> ${r.status}`)

  // 8) 旧密码失效，新密码可登
  r = await fetch(`${BASE}/api/admin/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: j({ username: ADMIN, password: PASS }),
  })
  ok(r.status === 401, `old password rejected after change -> ${r.status}`)
  r = await fetch(`${BASE}/api/admin/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: j({ username: ADMIN, password: 'newpass123' }),
  })
  ok(r.status === 200, `new password accepted -> ${r.status}`)
  const cookie2 = r.headers.get('set-cookie')
  const jar2 = { 'Cookie': cookie2 }

  // 9) 登出
  r = await fetch(`${BASE}/api/admin/logout`, { method: 'POST', headers: jar2 })
  ok(r.status === 200, `logout -> ${r.status}`)

  // 10) 历史接口（聚合 10 分钟才落库，初次应为空数组但不报错）
  r = await fetch(`${BASE}/api/servers/srv-001/metrics?range=24h`)
  ok(r.status === 200, `GET metrics -> ${r.status}`)
  data = await r.json()
  ok(Array.isArray(data.metrics), 'metrics is an array (empty ok)')

  console.log('\nSMOKE_DONE exitCode=' + (process.exitCode || 0))
}
main().catch((e) => { console.error('SMOKE_ERROR', e); process.exit(2) })
