const express = require('express');
const cors = require('cors');
const path = require('path');
const fs = require('fs');
const { initDatabase } = require('./utils/database');
require('dotenv').config();
const serverRoutes = require('./routes/servers');
const metricRoutes = require('./routes/metrics');

const app = express();
const PORT = process.env.PORT || 8080;

// 前端构建产物目录：__dirname=backend/src → 回退两层到项目根 → dist/
const distDir = path.resolve(__dirname, '..', '..', 'dist');

// Middleware
app.use(cors());
app.use(express.json());

// API Routes
app.use('/api/servers', serverRoutes);
app.use('/api/metrics', metricRoutes);

// Health check
app.get('/health', (req, res) => {
  res.json({ status: 'ok' });
});

// 前端静态托管 + SPA 路由回退（手写 sendFile，避免 serve-static 在本环境对目录索引失效）
app.use((req, res, next) => {
  if (req.method !== 'GET' || req.path.startsWith('/api')) {
    return next();
  }
  let rel = decodeURIComponent(req.path);
  if (rel.endsWith('/')) rel += 'index.html';
  const filePath = path.normalize(path.join(distDir, rel));
  // 目录穿越防护
  if (filePath !== distDir && !filePath.startsWith(distDir + path.sep)) {
    return next();
  }
  fs.stat(filePath, (err, stat) => {
    if (!err && stat.isFile()) {
      return res.sendFile(filePath, (se) => { if (se) next(); });
    }
    // 找不到具体文件 → SPA 回退到 index.html
    const indexFile = path.join(distDir, 'index.html');
    fs.stat(indexFile, (e2) => {
      if (e2) return next();
      res.sendFile(indexFile, (se) => { if (se) next(); });
    });
  });
});

// Initialize and start
async function start() {
  try {
    await initDatabase();
    app.listen(PORT, () => {
      console.log(`Server running on port ${PORT}`);
    });
  } catch (err) {
    console.error('Failed to start server:', err);
    process.exit(1);
  }
}

start();
