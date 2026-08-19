import axios from 'axios'

// 本地部署：前端与后端同源。
// - 开发环境：Vite 已配置 /api 代理到 http://localhost:8080
// - 生产环境：由后端 Express 托管 dist/，同样同源
// 如需指向独立后端地址，可设置 VITE_API_BASE（如 http://192.168.1.10:8080）
const baseURL = import.meta.env.VITE_API_BASE || ''

const request = axios.create({
  baseURL,
  timeout: 10000,
})

request.interceptors.response.use(
  (response: any) => response.data,
  (error: any) => Promise.reject(error)
)

export default request
