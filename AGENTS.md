# 项目技术上下文

## Dependencies
- **element-plus**: Vue3 UI组件库，用于管理后台界面
- **echarts**: 数据可视化图表库
- **axios**: HTTP客户端，用于API请求
- **pinia**: Vue状态管理
- **vue-router**: Vue路由管理
- **express**: Node.js Web框架（后端）
- **@supabase/supabase-js**: PostgreSQL数据库客户端（后端）
- **cors**: 跨域支持（后端）

## Architecture
- **前端**: Vue3 + Vite + Element Plus，单页应用
- **后端**: Node.js + Express，RESTful API
- **数据库**: PostgreSQL (Meoo Cloud)，通过REST API连接
- **数据采集**: Bash脚本，每5分钟通过crontab执行

## Data Flow
```
被控Shell脚本 --HTTP POST--> Node.js后端 --存储--> PostgreSQL
                                          |
Vue前端 <--HTTP GET-- Node.js后端 <--读取-- PostgreSQL
```

## Security
- HMAC-SHA256签名验签保障数据采集安全
- 预共享密钥在创建服务器时生成，需妥善保存
