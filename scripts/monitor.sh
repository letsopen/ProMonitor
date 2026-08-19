#!/bin/bash

# ==================== 配置区域 ====================
# 主控后端API地址（替换为实际部署地址）
SERVER_URL="http://localhost:8080/api/metrics"
# 服务器ID（从主控创建服务器时获取）
SERVER_ID="1"
# 预共享密钥（从主控创建服务器时获取）
SHARED_SECRET="your-secret-key-here"
# ================================================

# 采集CPU核数
get_cpu_cores() {
    nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo "1"
}

# 采集CPU使用率
get_cpu_usage() {
    local cpu_idle=$(top -bn1 2>/dev/null | grep "Cpu(s)" | awk '{print $8}' | cut -d'.' -f1)
    if [ -z "$cpu_idle" ]; then
        echo "0"
    else
        echo $((100 - ${cpu_idle:-0}))
    fi
}

# 采集内存总大小(MB)和使用率
get_memory_info() {
    local mem_info=$(free -m 2>/dev/null | grep Mem)
    if [ -n "$mem_info" ]; then
        echo "$mem_info" | awk '{printf "%d %.2f", $2, $3/$2 * 100}'
    else
        echo "0 0"
    fi
}

# 采集磁盘总空间(GB)和已占用比例
get_disk_info() {
    local disk_info=$(df -BG / 2>/dev/null | tail -1)
    if [ -n "$disk_info" ]; then
        local total=$(echo "$disk_info" | awk '{gsub(/G/,"",$2); print $2}')
        local percent=$(echo "$disk_info" | awk '{print $5}' | sed 's/%//')
        echo "${total:-0} ${percent:-0}"
    else
        echo "0 0"
    fi
}

# 采集网络流速(KB/s)
get_network_speed() {
    local iface="eth0"
    # 尝试其他常见网卡名
    if ! grep -q "$iface" /proc/net/dev 2>/dev/null; then
        iface=$(awk 'NR>2{print $1}' /proc/net/dev 2>/dev/null | head -1 | tr -d ':')
    fi

    if [ -z "$iface" ]; then
        echo "0 0"
        return
    fi

    local before=$(cat /proc/net/dev 2>/dev/null | grep "$iface" | awk '{print $2, $10}')
    sleep 1
    local after=$(cat /proc/net/dev 2>/dev/null | grep "$iface" | awk '{print $2, $10}')

    local in_before=$(echo "$before" | awk '{print $1}')
    local out_before=$(echo "$before" | awk '{print $2}')
    local in_after=$(echo "$after" | awk '{print $1}')
    local out_after=$(echo "$after" | awk '{print $2}')

    local in_speed=$(( (${in_after:-0} - ${in_before:-0}) / 1024 ))
    local out_speed=$(( (${out_after:-0} - ${out_before:-0}) / 1024 ))

    echo "${in_speed:-0} ${out_speed:-0}"
}

# 测试网络延迟
test_ping() {
    local target=$1
    local count=${2:-3}
    local result=$(ping -c $count -W 2 "$target" 2>/dev/null | grep 'rtt' | awk -F'/' '{print $5}')
    if [ -z "$result" ]; then
        echo "-1"
    else
        echo "$result"
    fi
}

# 生成HMAC-SHA256签名
generate_signature() {
    echo -n "$1" | openssl dgst -sha256 -hmac "$2" 2>/dev/null | awk '{print $NF}'
}

# 主函数
main() {
    CPU_CORES=$(get_cpu_cores)
    CPU_USAGE=$(get_cpu_usage)

    MEM_INFO=$(get_memory_info)
    MEMORY_TOTAL=$(echo "$MEM_INFO" | awk '{print $1}')
    MEMORY_USAGE=$(echo "$MEM_INFO" | awk '{print $2}')

    DISK_INFO=$(get_disk_info)
    DISK_TOTAL=$(echo "$DISK_INFO" | awk '{print $1}')
    DISK_USED_PERCENT=$(echo "$DISK_INFO" | awk '{print $2}')

    NET_SPEED=$(get_network_speed)
    NETWORK_IN=$(echo "$NET_SPEED" | awk '{print $1}')
    NETWORK_OUT=$(echo "$NET_SPEED" | awk '{print $2}')

    # 网络延迟测试（9个节点，使用公共DNS作为示例）
    # 注意：实际使用时应替换为真实的测试节点IP或域名
    PING_BJ_TEL=$(test_ping "114.114.114.114")      # 电信DNS示例
    PING_BJ_UNI=$(test_ping "223.5.5.5")             # 联通DNS示例
    PING_BJ_MOB=$(test_ping "120.196.165.24")        # 移动DNS示例
    PING_SH_TEL=$(test_ping "114.114.114.114")
    PING_SH_UNI=$(test_ping "223.5.5.5")
    PING_SH_MOB=$(test_ping "120.196.165.24")
    PING_GZ_TEL=$(test_ping "114.114.114.114")
    PING_GZ_UNI=$(test_ping "223.5.5.5")
    PING_GZ_MOB=$(test_ping "120.196.165.24")

    PAYLOAD=$(cat <<EOF
{
  "cpu_cores": ${CPU_CORES},
  "cpu_usage": ${CPU_USAGE},
  "memory_total": ${MEMORY_TOTAL},
  "memory_usage": ${MEMORY_USAGE},
  "disk_total": ${DISK_TOTAL},
  "disk_used_percent": ${DISK_USED_PERCENT},
  "network_in": ${NETWORK_IN:-0},
  "network_out": ${NETWORK_OUT:-0},
  "ping_beijing_telecom": ${PING_BJ_TEL:-0},
  "ping_beijing_unicom": ${PING_BJ_UNI:-0},
  "ping_beijing_mobile": ${PING_BJ_MOB:-0},
  "ping_shanghai_telecom": ${PING_SH_TEL:-0},
  "ping_shanghai_unicom": ${PING_SH_UNI:-0},
  "ping_shanghai_mobile": ${PING_SH_MOB:-0},
  "ping_guangzhou_telecom": ${PING_GZ_TEL:-0},
  "ping_guangzhou_unicom": ${PING_GZ_UNI:-0},
  "ping_guangzhou_mobile": ${PING_GZ_MOB:-0}
}
EOF
)

    SIGNATURE=$(generate_signature "$PAYLOAD" "$SHARED_SECRET")

    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${SERVER_URL}?server_id=${SERVER_ID}" \
        -H "Content-Type: application/json" \
        -H "X-Signature: ${SIGNATURE}" \
        -d "$PAYLOAD" 2>/dev/null)

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)

    if [ "$HTTP_CODE" = "200" ]; then
        echo "[$(date)] OK"
    else
        echo "[$(date)] Failed (HTTP $HTTP_CODE)" >&2
    fi
}

main
