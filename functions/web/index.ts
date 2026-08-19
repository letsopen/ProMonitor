// Edge Function to serve static web assets
const functionName = 'web';

// HTML content embedded directly
const htmlContent = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>服务器性能监控系统</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f7fa; }
    #app { min-height: 100vh; }
    .loading { display: flex; justify-content: center; align-items: center; height: 100vh; color: #409eff; }
  </style>
</head>
<body>
  <div id="app">
    <div class="loading">加载中...</div>
  </div>
  <script type="module">
    // 动态加载 Vue 应用
    const API_BASE = window.location.origin + '/sb-api/functions/v1/api';

    // 简单的服务器监控应用
    const app = {
      servers: [],
      async init() {
        document.getElementById('app').innerHTML = \`
          <div style="padding: 20px; max-width: 1200px; margin: 0 auto;">
            <h1 style="margin-bottom: 20px; color: #333;">服务器性能监控</h1>
            <button id="addBtn" style="padding: 10px 20px; background: #409eff; color: white; border: none; border-radius: 4px; cursor: pointer; margin-bottom: 20px;">添加被控</button>
            <div id="serverList"></div>
          </div>
        \`;

        document.getElementById('addBtn').onclick = () => this.showAddDialog();
        await this.loadServers();
      },

      async loadServers() {
        try {
          const res = await fetch(API_BASE + '/servers');
          this.servers = await res.json();
          this.renderServers();
        } catch (e) {
          console.error('加载失败:', e);
        }
      },

      renderServers() {
        const list = document.getElementById('serverList');
        if (!this.servers.length) {
          list.innerHTML = '<p style="color: #999;">暂无服务器，请点击上方按钮添加</p>';
          return;
        }

        list.innerHTML = this.servers.map(s => \`
          <div style="background: white; padding: 15px; margin-bottom: 10px; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1);">
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <div>
                <h3 style="margin: 0 0 5px 0;">\${s.name}</h3>
                <p style="margin: 0; color: #666; font-size: 14px;">\${s.provider || '未知服务商'} | 状态: <span style="color: \${s.status === 'online' ? '#67c23a' : '#f56c6c'}">\${s.status === 'online' ? '在线' : '离线'}</span></p>
              </div>
              <button onclick="app.deleteServer(\${s.id})" style="padding: 5px 15px; background: #f56c6c; color: white; border: none; border-radius: 4px; cursor: pointer;">删除</button>
            </div>
          </div>
        \`).join('');
      },

      showAddDialog() {
        const name = prompt('请输入服务器名称:');
        if (!name) return;
        const provider = prompt('请输入服务商:');

        fetch(API_BASE + '/servers', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name, provider, billing_cycle: 'monthly', price: 0 })
        })
        .then(r => r.json())
        .then(data => {
          alert('添加成功！预共享密钥: ' + data.shared_secret + '\\n请妥善保存，只显示一次！');
          this.loadServers();
        })
        .catch(e => alert('添加失败: ' + e.message));
      },

      async deleteServer(id) {
        if (!confirm('确定删除此服务器?')) return;
        await fetch(API_BASE + '/servers/' + id, { method: 'DELETE' });
        this.loadServers();
      }
    };

    app.init();
    window.app = app;
  </script>
</body>
</html>`;

Deno.serve(async (req) => {
  const requestId = crypto.randomUUID().slice(0, 8);
  const url = new URL(req.url);

  console.info(`[${functionName}] request ${requestId} path=${url.pathname}`);

  const corsHeaders = {
    'Content-Type': 'text/html; charset=utf-8',
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET, OPTIONS',
    'Access-Control-Allow-Headers': 'authorization, x-client-info, apikey, content-type',
  };

  if (req.method === 'OPTIONS') {
    return new Response('ok', { headers: corsHeaders });
  }

  return new Response(htmlContent, { headers: corsHeaders });
});
