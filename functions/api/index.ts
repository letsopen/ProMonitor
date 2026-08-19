import { createClient } from 'https://esm.sh/@supabase/supabase-js@2';

const functionName = 'api';

// CORS headers
const corsHeaders = {
  'Content-Type': 'application/json',
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, POST, DELETE, OPTIONS',
  'Access-Control-Allow-Headers': 'authorization, x-client-info, apikey, content-type, x-signature',
};

// HMAC-SHA256 signature verification
async function verifySignature(payload: string, signature: string, secret: string): Promise<boolean> {
  const encoder = new TextEncoder();
  const key = await crypto.subtle.importKey(
    'raw',
    encoder.encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign']
  );
  const signed = await crypto.subtle.sign('HMAC', key, encoder.encode(payload));
  const expectedSig = Array.from(new Uint8Array(signed))
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
  return signature === expectedSig;
}

// Generate random secret
function generateSecret(): string {
  const array = new Uint8Array(32);
  crypto.getRandomValues(array);
  return Array.from(array)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

// Route handlers
async function handleServers(req: Request, supabaseAdmin: any): Promise<Response> {
  const url = new URL(req.url);
  const path = url.pathname.replace('/functions/v1/api', '').replace('/api', '');

  // GET /api/servers - List all servers
  if (req.method === 'GET' && (path === '/servers' || path === '/servers/')) {
    const { data, error } = await supabaseAdmin
      .from('servers')
      .select('*')
      .order('created_at', { ascending: false });

    if (error) {
      return new Response(JSON.stringify({ error: error.message }), { status: 500, headers: corsHeaders });
    }
    return new Response(JSON.stringify(data || []), { headers: corsHeaders });
  }

  // POST /api/servers - Create server
  if (req.method === 'POST' && (path === '/servers' || path === '/servers/')) {
    const body = await req.json();
    const { name, provider, billing_cycle, price } = body;

    if (!name) {
      return new Response(JSON.stringify({ error: 'Name is required' }), { status: 400, headers: corsHeaders });
    }

    const sharedSecret = generateSecret();

    const { data, error } = await supabaseAdmin
      .from('servers')
      .insert({
        name,
        provider: provider || '',
        billing_cycle: billing_cycle || 'monthly',
        price: price || 0,
        shared_secret: sharedSecret
      })
      .select()
      .single();

    if (error) {
      return new Response(JSON.stringify({ error: error.message }), { status: 500, headers: corsHeaders });
    }
    return new Response(JSON.stringify(data), { status: 201, headers: corsHeaders });
  }

  // DELETE /api/servers/:id - Delete server
  if (req.method === 'DELETE' && path.match(/^\/servers\/\d+$/)) {
    const id = path.split('/')[2];

    // Delete related metrics first
    await supabaseAdmin.from('metrics').delete().eq('server_id', id);

    const { error } = await supabaseAdmin.from('servers').delete().eq('id', id);

    if (error) {
      return new Response(JSON.stringify({ error: error.message }), { status: 500, headers: corsHeaders });
    }
    return new Response(JSON.stringify({ message: 'Deleted' }), { headers: corsHeaders });
  }

  return new Response(JSON.stringify({ error: 'Not found' }), { status: 404, headers: corsHeaders });
}

async function handleMetrics(req: Request, supabaseAdmin: any): Promise<Response> {
  const url = new URL(req.url);
  const path = url.pathname.replace('/functions/v1/api', '').replace('/api', '');

  // POST /api/metrics?server_id=X - Submit metrics (with HMAC verification)
  if (req.method === 'POST' && (path === '/metrics' || path === '/metrics/')) {
    const serverId = url.searchParams.get('server_id');
    const signature = req.headers.get('x-signature');

    if (!serverId || !signature) {
      return new Response(JSON.stringify({ error: 'server_id and X-Signature header are required' }), { status: 400, headers: corsHeaders });
    }

    // Get server to verify signature
    const { data: server, error: serverError } = await supabaseAdmin
      .from('servers')
      .select('*')
      .eq('id', serverId)
      .single();

    if (serverError || !server) {
      return new Response(JSON.stringify({ error: 'Server not found' }), { status: 404, headers: corsHeaders });
    }

    const bodyText = await req.text();

    // Verify signature
    const isValid = await verifySignature(bodyText, signature, server.shared_secret);
    if (!isValid) {
      return new Response(JSON.stringify({ error: 'Invalid signature' }), { status: 401, headers: corsHeaders });
    }

    const data = JSON.parse(bodyText);

    // Insert metrics
    const { error: insertError } = await supabaseAdmin.from('metrics').insert({
      server_id: parseInt(serverId),
      timestamp: new Date().toISOString(),
      cpu_cores: data.cpu_cores || 0,
      cpu_usage: data.cpu_usage || 0,
      memory_total: data.memory_total || 0,
      memory_usage: data.memory_usage || 0,
      disk_total: data.disk_total || 0,
      disk_used_percent: data.disk_used_percent || 0,
      network_in: data.network_in || 0,
      network_out: data.network_out || 0,
      ping_beijing_telecom: data.ping_beijing_telecom || 0,
      ping_beijing_unicom: data.ping_beijing_unicom || 0,
      ping_beijing_mobile: data.ping_beijing_mobile || 0,
      ping_shanghai_telecom: data.ping_shanghai_telecom || 0,
      ping_shanghai_unicom: data.ping_shanghai_unicom || 0,
      ping_shanghai_mobile: data.ping_shanghai_mobile || 0,
      ping_guangzhou_telecom: data.ping_guangzhou_telecom || 0,
      ping_guangzhou_unicom: data.ping_guangzhou_unicom || 0,
      ping_guangzhou_mobile: data.ping_guangzhou_mobile || 0
    });

    if (insertError) {
      return new Response(JSON.stringify({ error: insertError.message }), { status: 500, headers: corsHeaders });
    }

    // Update server status
    await supabaseAdmin.from('servers').update({
      status: 'online',
      last_seen: new Date().toISOString()
    }).eq('id', serverId);

    return new Response(JSON.stringify({ message: 'Received' }), { headers: corsHeaders });
  }

  // GET /api/metrics/:id - Get metrics history
  if (req.method === 'GET' && path.match(/^\/metrics\/\d+$/)) {
    const id = path.split('/')[2];
    const startTime = url.searchParams.get('start_time') || new Date(Date.now() - 24 * 3600000).toISOString();
    const endTime = url.searchParams.get('end_time') || new Date().toISOString();

    const { data, error } = await supabaseAdmin
      .from('metrics')
      .select('*')
      .eq('server_id', id)
      .gte('timestamp', startTime)
      .lte('timestamp', endTime)
      .order('timestamp', { ascending: true });

    if (error) {
      return new Response(JSON.stringify({ error: error.message }), { status: 500, headers: corsHeaders });
    }
    return new Response(JSON.stringify(data || []), { headers: corsHeaders });
  }

  // GET /api/metrics/:id/latest - Get latest metrics
  if (req.method === 'GET' && path.match(/^\/metrics\/\d+\/latest$/)) {
    const id = path.split('/')[2];

    const { data, error } = await supabaseAdmin
      .from('metrics')
      .select('*')
      .eq('server_id', id)
      .order('timestamp', { ascending: false })
      .limit(1)
      .single();

    if (error) {
      if (error.code === 'PGRST116') {
        return new Response(JSON.stringify({ error: 'No data found' }), { status: 404, headers: corsHeaders });
      }
      return new Response(JSON.stringify({ error: error.message }), { status: 500, headers: corsHeaders });
    }
    return new Response(JSON.stringify(data), { headers: corsHeaders });
  }

  return new Response(JSON.stringify({ error: 'Not found' }), { status: 404, headers: corsHeaders });
}

// HTML content for web interface
const htmlContent = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>服务器性能监控系统</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f7fa; }
    .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
    h1 { color: #333; margin-bottom: 20px; }
    .btn { padding: 10px 20px; background: #409eff; color: white; border: none; border-radius: 4px; cursor: pointer; margin-bottom: 20px; }
    .btn:hover { background: #66b1ff; }
    .btn-danger { background: #f56c6c; }
    .btn-danger:hover { background: #f78989; }
    .server-card { background: white; padding: 15px; margin-bottom: 10px; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    .server-header { display: flex; justify-content: space-between; align-items: center; }
    .server-name { font-size: 18px; font-weight: bold; color: #333; cursor: pointer; }
    .server-name:hover { color: #409eff; }
    .server-info { color: #666; font-size: 14px; margin-top: 5px; }
    .status-online { color: #67c23a; }
    .status-offline { color: #f56c6c; }
    .empty { color: #999; text-align: center; padding: 40px; }
    .modal { display: none; position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.5); justify-content: center; align-items: center; }
    .modal.show { display: flex; }
    .modal-content { background: white; padding: 30px; border-radius: 8px; width: 400px; max-height: 90vh; overflow-y: auto; }
    .modal-content.detail { width: 900px; }
    .modal-title { font-size: 18px; margin-bottom: 20px; }
    .form-group { margin-bottom: 15px; }
    .form-label { display: block; margin-bottom: 5px; color: #333; }
    .form-input { width: 100%; padding: 8px; border: 1px solid #dcdfe6; border-radius: 4px; }
    .modal-buttons { display: flex; justify-content: flex-end; gap: 10px; margin-top: 20px; }
    .btn-secondary { background: #909399; }
    .btn-secondary:hover { background: #a6a9ad; }
    .secret-box { background: #f5f7fa; padding: 15px; border-radius: 4px; margin: 15px 0; word-break: break-all; font-family: monospace; }
    .detail-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 15px; margin-bottom: 20px; }
    .detail-item { background: #f5f7fa; padding: 15px; border-radius: 8px; text-align: center; }
    .detail-label { color: #666; font-size: 12px; margin-bottom: 5px; }
    .detail-value { color: #333; font-size: 24px; font-weight: bold; }
    .detail-unit { color: #999; font-size: 12px; }
    .chart-container { margin: 20px 0; background: white; border-radius: 8px; padding: 15px; border: 1px solid #e0e0e0; }
    .chart-container canvas { display: block; width: 100%; height: auto; }
    .tabs { display: flex; gap: 10px; margin-bottom: 20px; border-bottom: 1px solid #dcdfe6; }
    .tab { padding: 10px 20px; cursor: pointer; border-bottom: 2px solid transparent; }
    .tab.active { border-bottom-color: #409eff; color: #409eff; }
  </style>
</head>
<body>
  <div class="container">
    <h1>服务器性能监控</h1>
    <button class="btn" onclick="showAddModal()">添加被控</button>
    <div id="serverList"></div>
  </div>

  <div id="addModal" class="modal">
    <div class="modal-content">
      <div class="modal-title">添加服务器</div>
      <div class="form-group">
        <label class="form-label">服务器名称 *</label>
        <input type="text" id="serverName" class="form-input" placeholder="例如: 香港服务器">
      </div>
      <div class="form-group">
        <label class="form-label">服务商</label>
        <input type="text" id="serverProvider" class="form-input" placeholder="例如: 阿里云">
      </div>
      <div class="form-group">
        <label class="form-label">付费周期</label>
        <select id="billingCycle" class="form-input">
          <option value="monthly">月付</option>
          <option value="yearly">年付</option>
        </select>
      </div>
      <div class="form-group">
        <label class="form-label">价格</label>
        <input type="number" id="serverPrice" class="form-input" placeholder="0" value="0">
      </div>
      <div class="modal-buttons">
        <button class="btn btn-secondary" onclick="hideAddModal()">取消</button>
        <button class="btn" onclick="addServer()">确定</button>
      </div>
    </div>
  </div>

  <div id="secretModal" class="modal">
    <div class="modal-content">
      <div class="modal-title">服务器添加成功</div>
      <p>请保存以下预共享密钥，<strong>只显示一次</strong>：</p>
      <div id="secretValue" class="secret-box"></div>
      <p style="color: #666; font-size: 12px;">在被控服务器上配置 monitor.sh 时使用此密钥</p>
      <div class="modal-buttons">
        <button class="btn" onclick="hideSecretModal()">确定</button>
      </div>
    </div>
  </div>

  <div id="detailModal" class="modal">
    <div class="modal-content detail">
      <div class="modal-title" id="detailTitle">服务器详情</div>
      <div id="detailContent">
        <div class="detail-grid">
          <div class="detail-item">
            <div class="detail-label">CPU 使用率</div>
            <div class="detail-value" id="cpuValue">-</div>
            <div class="detail-unit">%</div>
          </div>
          <div class="detail-item">
            <div class="detail-label">内存使用率</div>
            <div class="detail-value" id="memoryValue">-</div>
            <div class="detail-unit">%</div>
          </div>
          <div class="detail-item">
            <div class="detail-label">磁盘使用率</div>
            <div class="detail-value" id="diskValue">-</div>
            <div class="detail-unit">%</div>
          </div>
        </div>
        <div class="detail-grid">
          <div class="detail-item">
            <div class="detail-label">入站流量</div>
            <div class="detail-value" id="networkInValue">-</div>
            <div class="detail-unit">KB/s</div>
          </div>
          <div class="detail-item">
            <div class="detail-label">出站流量</div>
            <div class="detail-value" id="networkOutValue">-</div>
            <div class="detail-unit">KB/s</div>
          </div>
          <div class="detail-item">
            <div class="detail-label">最后上报</div>
            <div class="detail-value" id="lastSeenValue" style="font-size: 14px;">-</div>
          </div>
        </div>
        <h3 style="margin: 20px 0 10px 0; color: #333;">网络延迟趋势 (最近24小时)</h3>
        <div class="chart-container">
          <canvas id="pingChart" width="800" height="250"></canvas>
        </div>
      </div>
      <div class="modal-buttons">
        <button class="btn btn-secondary" onclick="hideDetailModal()">关闭</button>
      </div>
    </div>
  </div>

  <script>
    const API_BASE = window.location.origin + '/sb-api/functions/v1/api';

    async function loadServers() {
      try {
        const res = await fetch(API_BASE + '/servers');
        const servers = await res.json();
        renderServers(servers);
      } catch (e) {
        document.getElementById('serverList').innerHTML = '<div class="empty">加载失败: ' + e.message + '</div>';
      }
    }

    function renderServers(servers) {
      const list = document.getElementById('serverList');
      if (!servers || !servers.length) {
        list.innerHTML = '<div class="empty">暂无服务器，请点击上方按钮添加</div>';
        return;
      }

      list.innerHTML = servers.map(s => \`
        <div class="server-card">
          <div class="server-header">
            <div>
              <div class="server-name" onclick="showDetail(\${s.id}, '\${s.name}')">\${s.name}</div>
              <div class="server-info">
                \${s.provider || '未知服务商'} |
                状态: <span class="\${s.status === 'online' ? 'status-online' : 'status-offline'}">\${s.status === 'online' ? '在线' : '离线'}</span>
                \${s.last_seen ? '| 最后上报: ' + new Date(s.last_seen).toLocaleString() : ''}
              </div>
            </div>
            <button class="btn btn-danger" onclick="event.stopPropagation(); deleteServer(\${s.id})">删除</button>
          </div>
        </div>
      \`).join('');
    }

    function showAddModal() {
      document.getElementById('addModal').classList.add('show');
    }

    function hideAddModal() {
      document.getElementById('addModal').classList.remove('show');
      document.getElementById('serverName').value = '';
      document.getElementById('serverProvider').value = '';
      document.getElementById('serverPrice').value = '0';
    }

    function hideSecretModal() {
      document.getElementById('secretModal').classList.remove('show');
      loadServers();
    }

    async function addServer() {
      const name = document.getElementById('serverName').value.trim();
      const provider = document.getElementById('serverProvider').value.trim();
      const billing_cycle = document.getElementById('billingCycle').value;
      const price = parseFloat(document.getElementById('serverPrice').value) || 0;

      if (!name) {
        alert('请输入服务器名称');
        return;
      }

      try {
        const res = await fetch(API_BASE + '/servers', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name, provider, billing_cycle, price })
        });

        if (!res.ok) throw new Error('添加失败');

        const data = await res.json();
        hideAddModal();
        document.getElementById('secretValue').textContent = data.shared_secret;
        document.getElementById('secretModal').classList.add('show');
      } catch (e) {
        alert('添加失败: ' + e.message);
      }
    }

    async function deleteServer(id) {
      if (!confirm('确定删除此服务器?')) return;
      try {
        await fetch(API_BASE + '/servers/' + id, { method: 'DELETE' });
        loadServers();
      } catch (e) {
        alert('删除失败: ' + e.message);
      }
    }

    // Close modal on outside click
    document.querySelectorAll('.modal').forEach(m => {
      m.onclick = (e) => { if (e.target === m) m.classList.remove('show'); };
    });

    async function showDetail(id, name) {
      document.getElementById('detailTitle').textContent = name + ' - 服务器详情';
      document.getElementById('detailModal').classList.add('show');

      // Reset values
      document.getElementById('cpuValue').textContent = '-';
      document.getElementById('memoryValue').textContent = '-';
      document.getElementById('diskValue').textContent = '-';
      document.getElementById('networkInValue').textContent = '-';
      document.getElementById('networkOutValue').textContent = '-';
      document.getElementById('lastSeenValue').textContent = '-';

      // Clear canvas
      const canvas = document.getElementById('pingChart');
      const ctx = canvas.getContext('2d');
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.fillStyle = '#999';
      ctx.font = '14px sans-serif';
      ctx.textAlign = 'center';
      ctx.fillText('加载中...', canvas.width / 2, canvas.height / 2);

      try {
        // Load latest metrics
        const latestRes = await fetch(API_BASE + '/metrics/' + id + '/latest');
        if (latestRes.ok) {
          const data = await latestRes.json();
          document.getElementById('cpuValue').textContent = data.cpu_usage?.toFixed(1) || '-';
          document.getElementById('memoryValue').textContent = data.memory_usage?.toFixed(1) || '-';
          document.getElementById('diskValue').textContent = data.disk_used_percent?.toFixed(1) || '-';
          document.getElementById('networkInValue').textContent = data.network_in?.toFixed(1) || '-';
          document.getElementById('networkOutValue').textContent = data.network_out?.toFixed(1) || '-';
          document.getElementById('lastSeenValue').textContent = data.timestamp ? new Date(data.timestamp).toLocaleString() : '-';
        }

        // Load historical metrics for chart (last 24 hours)
        const historyRes = await fetch(API_BASE + '/metrics/' + id);
        if (historyRes.ok) {
          const historyData = await historyRes.json();
          drawPingChart(historyData);
        } else {
          ctx.clearRect(0, 0, canvas.width, canvas.height);
          ctx.fillStyle = '#999';
          ctx.font = '14px sans-serif';
          ctx.textAlign = 'center';
          ctx.fillText('暂无历史数据', canvas.width / 2, canvas.height / 2);
        }
      } catch (e) {
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.fillStyle = '#f56c6c';
        ctx.font = '14px sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('加载失败', canvas.width / 2, canvas.height / 2);
      }
    }

    function drawPingChart(data) {
      const canvas = document.getElementById('pingChart');
      const ctx = canvas.getContext('2d');
      const width = canvas.width;
      const height = canvas.height;
      const padding = { top: 30, right: 120, bottom: 40, left: 50 };
      const chartWidth = width - padding.left - padding.right;
      const chartHeight = height - padding.top - padding.bottom;

      // Clear canvas
      ctx.clearRect(0, 0, width, height);

      if (!data || data.length === 0) {
        ctx.fillStyle = '#999';
        ctx.font = '14px sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('暂无数据', width / 2, height / 2);
        return;
      }

      // Sort by timestamp
      data.sort((a, b) => new Date(a.timestamp) - new Date(b.timestamp));

      // Define ping series
      const series = [
        { key: 'ping_beijing_telecom', label: '北京-电信', color: '#409eff' },
        { key: 'ping_beijing_unicom', label: '北京-联通', color: '#67c23a' },
        { key: 'ping_beijing_mobile', label: '北京-移动', color: '#e6a23c' },
        { key: 'ping_shanghai_telecom', label: '上海-电信', color: '#f56c6c' },
        { key: 'ping_shanghai_unicom', label: '上海-联通', color: '#909399' },
        { key: 'ping_shanghai_mobile', label: '上海-移动', color: '#9c27b0' },
        { key: 'ping_guangzhou_telecom', label: '广州-电信', color: '#00bcd4' },
        { key: 'ping_guangzhou_unicom', label: '广州-联通', color: '#ff9800' },
        { key: 'ping_guangzhou_mobile', label: '广州-移动', color: '#795548' },
      ];

      // Calculate min/max values
      let minVal = Infinity;
      let maxVal = 0;
      data.forEach(d => {
        series.forEach(s => {
          const val = d[s.key];
          if (val > 0) {
            minVal = Math.min(minVal, val);
            maxVal = Math.max(maxVal, val);
          }
        });
      });

      if (minVal === Infinity) {
        ctx.fillStyle = '#999';
        ctx.font = '14px sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('无有效延迟数据', width / 2, height / 2);
        return;
      }

      // Add some padding to y-axis
      minVal = Math.max(0, minVal - 10);
      maxVal = maxVal + 20;

      // Draw grid lines
      ctx.strokeStyle = '#e0e0e0';
      ctx.lineWidth = 1;
      for (let i = 0; i <= 5; i++) {
        const y = padding.top + (chartHeight / 5) * i;
        ctx.beginPath();
        ctx.moveTo(padding.left, y);
        ctx.lineTo(padding.left + chartWidth, y);
        ctx.stroke();

        // Y-axis labels
        const val = maxVal - (maxVal - minVal) * (i / 5);
        ctx.fillStyle = '#666';
        ctx.font = '11px sans-serif';
        ctx.textAlign = 'right';
        ctx.fillText(val.toFixed(0) + 'ms', padding.left - 8, y + 4);
      }

      // Draw X-axis time labels
      const timeLabels = [0, Math.floor(data.length / 2), data.length - 1];
      timeLabels.forEach(idx => {
        if (idx >= 0 && idx < data.length) {
          const x = padding.left + (idx / (data.length - 1 || 1)) * chartWidth;
          const date = new Date(data[idx].timestamp);
          const timeStr = date.getHours().toString().padStart(2, '0') + ':' + date.getMinutes().toString().padStart(2, '0');
          ctx.fillStyle = '#666';
          ctx.font = '11px sans-serif';
          ctx.textAlign = 'center';
          ctx.fillText(timeStr, x, height - 10);
        }
      });

      // Draw each series
      series.forEach(s => {
        const points = [];
        data.forEach((d, idx) => {
          const val = d[s.key];
          if (val > 0) {
            const x = padding.left + (idx / (data.length - 1 || 1)) * chartWidth;
            const y = padding.top + chartHeight - ((val - minVal) / (maxVal - minVal)) * chartHeight;
            points.push({ x, y, val });
          }
        });

        if (points.length < 2) return;

        // Draw line
        ctx.strokeStyle = s.color;
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.moveTo(points[0].x, points[0].y);
        for (let i = 1; i < points.length; i++) {
          ctx.lineTo(points[i].x, points[i].y);
        }
        ctx.stroke();

        // Draw points
        ctx.fillStyle = s.color;
        points.forEach(p => {
          ctx.beginPath();
          ctx.arc(p.x, p.y, 3, 0, Math.PI * 2);
          ctx.fill();
        });
      });

      // Draw legend
      const legendX = padding.left + chartWidth + 10;
      series.forEach((s, idx) => {
        const y = padding.top + idx * 22;

        // Color box
        ctx.fillStyle = s.color;
        ctx.fillRect(legendX, y - 6, 12, 12);

        // Label
        ctx.fillStyle = '#333';
        ctx.font = '11px sans-serif';
        ctx.textAlign = 'left';
        ctx.fillText(s.label, legendX + 18, y + 3);
      });

      // Draw axes
      ctx.strokeStyle = '#333';
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(padding.left, padding.top);
      ctx.lineTo(padding.left, padding.top + chartHeight);
      ctx.lineTo(padding.left + chartWidth, padding.top + chartHeight);
      ctx.stroke();
    }

    function hideDetailModal() {
      document.getElementById('detailModal').classList.remove('show');
    }

    loadServers();
  </script>
</body>
</html>`;

// Main handler
Deno.serve(async (req) => {
  const requestId = crypto.randomUUID().slice(0, 8);
  const url = new URL(req.url);
  const path = url.pathname.replace('/functions/v1/api', '').replace('/api', '');

  console.info(`[${functionName}] request ${requestId} method=${req.method} url=${req.url} path=${path}`);

  // CORS preflight
  if (req.method === 'OPTIONS') {
    console.info(`[${functionName}] CORS preflight ${requestId}`);
    return new Response('ok', { headers: corsHeaders });
  }

  // Serve HTML for root path
  if (path === '' || path === '/') {
    return new Response(htmlContent, {
      headers: {
        'Content-Type': 'text/html; charset=utf-8',
        'Access-Control-Allow-Origin': '*'
      }
    });
  }

  try {
    const supabaseUrl = Deno.env.get('SUPABASE_URL');
    const supabaseServiceKey = Deno.env.get('SUPABASE_SERVICE_ROLE_KEY');

    if (!supabaseUrl || !supabaseServiceKey) {
      throw new Error('Missing SUPABASE_URL or SUPABASE_SERVICE_ROLE_KEY');
    }

    const supabaseAdmin = createClient(supabaseUrl, supabaseServiceKey);

    // Route to appropriate handler
    if (path.startsWith('/servers')) {
      const response = await handleServers(req, supabaseAdmin);
      console.info(`[${functionName}] success ${requestId}`);
      return response;
    }

    if (path.startsWith('/metrics')) {
      const response = await handleMetrics(req, supabaseAdmin);
      console.info(`[${functionName}] success ${requestId}`);
      return response;
    }

    // Health check
    if (path === '/health' || path === '/health/') {
      return new Response(JSON.stringify({ status: 'ok' }), { headers: corsHeaders });
    }

    return new Response(JSON.stringify({ error: 'Not found' }), { status: 404, headers: corsHeaders });
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Unknown error';
    console.error(`[${functionName}] failed ${requestId}: ${message}`);
    return new Response(JSON.stringify({ error: message }), {
      status: 500,
      headers: corsHeaders,
    });
  }
});
