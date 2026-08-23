import axios from 'axios'

// 自托管：前后端同源（Go 同时托管 API 与前端），baseURL 留空即可。
// 如需指向独立后端地址，可设置 VITE_API_BASE（如 http://192.168.1.10:9000）
const baseURL = import.meta.env.VITE_API_BASE || ''

const request = axios.create({
  baseURL,
  timeout: 10000,
  withCredentials: true, // 携带 httpOnly 会话 Cookie
})

request.interceptors.response.use(
  (response: any) => response.data,
  (error: any) => Promise.reject(error)
)

export default request
