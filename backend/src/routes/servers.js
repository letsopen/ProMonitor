const express = require('express');
const router = express.Router();
const { supabase } = require('../utils/database');
const { generateSecret } = require('../utils/crypto');

// 获取所有服务器
router.get('/', async (req, res) => {
  try {
    const { data, error } = await supabase
      .from('servers')
      .select('*')
      .order('created_at', { ascending: false });

    if (error) throw error;
    res.json(data || []);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// 创建服务器
router.post('/', async (req, res) => {
  try {
    const { name, provider, billing_cycle, price } = req.body;

    if (!name) {
      return res.status(400).json({ error: 'Name is required' });
    }

    const sharedSecret = generateSecret();

    const { data, error } = await supabase
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

    if (error) throw error;
    if (!data) {
      return res.status(500).json({ error: 'Failed to create server' });
    }

    res.status(201).json(data);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// 删除服务器
router.delete('/:id', async (req, res) => {
  try {
    const { id } = req.params;

    // 先删除关联的指标数据
    const { error: metricsError } = await supabase
      .from('metrics')
      .delete()
      .eq('server_id', id);

    if (metricsError) throw metricsError;

    // 再删除服务器
    const { error } = await supabase
      .from('servers')
      .delete()
      .eq('id', id);

    if (error) throw error;

    res.json({ message: 'Deleted' });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
