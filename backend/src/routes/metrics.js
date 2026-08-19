const express = require('express');
const router = express.Router();
const { supabase } = require('../utils/database');
const { verifySignature } = require('../utils/crypto');

// 接收指标数据（公开接口，需HMAC验签）
router.post('/', async (req, res) => {
  try {
    const serverId = req.query.server_id;
    const signature = req.headers['x-signature'];

    if (!serverId || !signature) {
      return res.status(400).json({ error: 'server_id and X-Signature header are required' });
    }

    // 查找服务器获取密钥
    const { data: servers, error: serverError } = await supabase
      .from('servers')
      .select('*')
      .eq('id', serverId)
      .single();

    if (serverError || !servers) {
      return res.status(404).json({ error: 'Server not found' });
    }

    const server = servers;
    const payload = JSON.stringify(req.body);

    // 验签
    if (!verifySignature(payload, signature, server.shared_secret)) {
      return res.status(401).json({ error: 'Invalid signature' });
    }

    const data = req.body;

    // 保存指标
    const { error: insertError } = await supabase
      .from('metrics')
      .insert({
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

    if (insertError) throw insertError;

    // 更新服务器状态
    const { error: updateError } = await supabase
      .from('servers')
      .update({
        status: 'online',
        last_seen: new Date().toISOString()
      })
      .eq('id', serverId);

    if (updateError) throw updateError;

    res.json({ message: 'Received' });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// 查询指标历史
router.get('/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const { start_time, end_time } = req.query;

    const startTime = start_time || new Date(Date.now() - 24 * 3600000).toISOString();
    const endTime = end_time || new Date().toISOString();

    const { data, error } = await supabase
      .from('metrics')
      .select('*')
      .eq('server_id', id)
      .gte('timestamp', startTime)
      .lte('timestamp', endTime)
      .order('timestamp', { ascending: true });

    if (error) throw error;
    res.json(data || []);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// 获取最新指标
router.get('/:id/latest', async (req, res) => {
  try {
    const { id } = req.params;

    const { data, error } = await supabase
      .from('metrics')
      .select('*')
      .eq('server_id', id)
      .order('timestamp', { ascending: false })
      .limit(1)
      .single();

    if (error) {
      if (error.code === 'PGRST116') {
        return res.status(404).json({ error: 'No data found' });
      }
      throw error;
    }

    res.json(data);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
