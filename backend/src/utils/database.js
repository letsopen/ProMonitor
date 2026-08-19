require('dotenv').config();
const { Pool } = require('pg');

// PostgreSQL 连接串（本地部署通过环境变量 DATABASE_URL 注入）
const DATABASE_URL =
  process.env.DATABASE_URL ||
  'postgres://postgres:postgres@localhost:5432/servermonitor';

const pool = new Pool({
  connectionString: DATABASE_URL,
  max: 10,
  idleTimeoutMillis: 30000,
  connectionTimeoutMillis: 5000,
});

pool.on('error', (err) => {
  // 连接池中的空闲连接异常（如 DB 重启），仅记录，不崩溃进程
  console.error('Unexpected PostgreSQL pool error:', err.message);
});

// 数据库 schema：不使用默认 public，单独隔离本项目数据
const SCHEMA_NAME = process.env.DB_SCHEMA || 'pro_monitor';

// 带 schema 限定的表引用，例如 "pro_monitor"."servers"
function tableRef(table) {
  return `${quoteIdent(SCHEMA_NAME)}.${quoteIdent(table)}`;
}

// 表结构（用于 WHERE/SET 的显式类型转换，避免 text 与 integer/timestamptz 比较报错）
const SCHEMA = {
  servers: {
    id: 'integer',
    name: 'text',
    provider: 'text',
    billing_cycle: 'text',
    price: 'numeric',
    shared_secret: 'text',
    status: 'text',
    last_seen: 'timestamptz',
    created_at: 'timestamptz',
    updated_at: 'timestamptz',
  },
  metrics: {
    id: 'integer',
    server_id: 'integer',
    timestamp: 'timestamptz',
    cpu_cores: 'integer',
    cpu_usage: 'real',
    memory_total: 'bigint',
    memory_usage: 'real',
    disk_total: 'bigint',
    disk_used_percent: 'real',
    network_in: 'real',
    network_out: 'real',
    ping_beijing_telecom: 'real',
    ping_beijing_unicom: 'real',
    ping_beijing_mobile: 'real',
    ping_shanghai_telecom: 'real',
    ping_shanghai_unicom: 'real',
    ping_shanghai_mobile: 'real',
    ping_guangzhou_telecom: 'real',
    ping_guangzhou_unicom: 'real',
    ping_guangzhou_mobile: 'real',
  },
};

const IDENT_RE = /^[a-zA-Z_][a-zA-Z0-9_]*$/;

function quoteIdent(name) {
  if (!IDENT_RE.test(name)) {
    throw new Error(`Invalid identifier: ${name}`);
  }
  return `"${name}"`;
}

// 为某列生成带类型转换的占位符（解决 text vs integer/timestamptz 比较问题）
function castedPlaceholder(table, col, idx, value) {
  const type = SCHEMA[table] && SCHEMA[table][col];
  if (type && value !== null && value !== undefined) {
    return `$${idx}::${type}`;
  }
  return `$${idx}`;
}

class QueryBuilder {
  constructor(table) {
    this.table = table;
    this.op = 'SELECT';
    this.columns = '*';
    this.values = null;
    this.filters = []; // {col, op:'=|^=|<=', value}
    this.orderCol = null;
    this.orderAsc = true;
    this.limitVal = null;
    this.returnSingle = false;
    this.params = [];
  }

  select(columns = '*') {
    // 注意：不要在此处重置 this.op。
    // 在 INSERT/UPDATE/DELETE 之后调用 .select() 表示 RETURNING *，不应切回 SELECT。
    this.columns = columns;
    return this;
  }

  insert(data) {
    this.op = 'INSERT';
    this.values = Array.isArray(data) ? data : [data];
    return this;
  }

  update(data) {
    this.op = 'UPDATE';
    this.values = data;
    return this;
  }

  delete() {
    this.op = 'DELETE';
    return this;
  }

  _addFilter(col, operator, value) {
    if (!IDENT_RE.test(col)) throw new Error(`Invalid identifier: ${col}`);
    this.filters.push({ col, operator, value });
    return this;
  }

  eq(col, value) {
    return this._addFilter(col, '=', value);
  }
  gte(col, value) {
    return this._addFilter(col, '>=', value);
  }
  lte(col, value) {
    return this._addFilter(col, '<=', value);
  }

  order(col, { ascending = true } = {}) {
    if (!IDENT_RE.test(col)) throw new Error(`Invalid identifier: ${col}`);
    this.orderCol = col;
    this.orderAsc = ascending;
    return this;
  }

  limit(n) {
    this.limitVal = parseInt(n, 10);
    return this;
  }

  // 终端：返回单行
  single() {
    this.returnSingle = true;
    return this.execute(true);
  }

  // 使 builder 可 await（非 single 场景）
  then(onFulfilled, onRejected) {
    return this.execute(false).then(onFulfilled, onRejected);
  }

  _whereClause() {
    if (this.filters.length === 0) return '';
    const conds = this.filters.map((f) => {
      this.params.push(f.value);
      const idx = this.params.length;
      return `${quoteIdent(f.col)} ${f.operator} ${castedPlaceholder(this.table, f.col, idx, f.value)}`;
    });
    return ` WHERE ${conds.join(' AND ')}`;
  }

  async execute(isSingle) {
    this.params = [];
    let sql = '';
    try {
      if (this.op === 'SELECT') {
        const cols =
          this.columns === '*' ? '*' : this.columns.split(',').map((c) => quoteIdent(c.trim())).join(', ');
        sql = `SELECT ${cols} FROM ${tableRef(this.table)}${this._whereClause()}`;
        if (this.orderCol) {
          sql += ` ORDER BY ${quoteIdent(this.orderCol)} ${this.orderAsc ? 'ASC' : 'DESC'}`;
        }
        if (isSingle || this.limitVal != null) {
          sql += ` LIMIT ${isSingle ? 1 : this.limitVal}`;
        }
      } else if (this.op === 'INSERT') {
        const rows = this.values;
        const cols = Object.keys(rows[0]);
        const placeholders = [];
        rows.forEach((row) => {
          const rowPh = cols.map((c) => {
            this.params.push(row[c]);
            const idx = this.params.length;
            return castedPlaceholder(this.table, c, idx, row[c]);
          });
          placeholders.push(`(${rowPh.join(', ')})`);
        });
        sql = `INSERT INTO ${tableRef(this.table)} (${cols.map(quoteIdent).join(', ')}) VALUES ${placeholders.join(', ')} RETURNING *`;
      } else if (this.op === 'UPDATE') {
        const sets = Object.keys(this.values).map((c) => {
          this.params.push(this.values[c]);
          const idx = this.params.length;
          return `${quoteIdent(c)} = ${castedPlaceholder(this.table, c, idx, this.values[c])}`;
        });
        sql = `UPDATE ${tableRef(this.table)} SET ${sets.join(', ')}${this._whereClause()} RETURNING *`;
      } else if (this.op === 'DELETE') {
        sql = `DELETE FROM ${tableRef(this.table)}${this._whereClause()} RETURNING *`;
      }

      const result = await pool.query(sql, this.params);
      // .single() 对所有操作统一返回单行对象（或空行错误）；否则返回完整 rows 数组
      if (isSingle) {
        if (result.rows.length === 0) {
          return { data: null, error: { code: 'PGRST116', message: 'No rows found' } };
        }
        return { data: result.rows[0], error: null };
      }
      return { data: result.rows, error: null };
    } catch (err) {
      return {
        data: null,
        error: { code: err.code || 'DB_ERROR', message: err.message, detail: err.detail },
      };
    }
  }
}

// Supabase 风格的 API（本地 PostgreSQL 实现）
const supabase = {
  from: (table) => new QueryBuilder(table),
};

// 初始化 / 连通性检查（非致命：连不上仅告警，仍启动服务，便于本地调试）
async function initDatabase() {
  try {
    await pool.query('SELECT 1');
    console.log('PostgreSQL connected successfully');
    return true;
  } catch (err) {
    console.warn(
      `WARNING: cannot connect to PostgreSQL (${err.code || err.message || 'unknown error'}). ` +
        'Set DATABASE_URL in backend/.env. API routes will return 500 until the database is reachable.'
    );
    return false;
  }
}

module.exports = { supabase, initDatabase, pool, query: (text, params) => pool.query(text, params) };
