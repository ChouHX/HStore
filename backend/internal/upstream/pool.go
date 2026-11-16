package upstream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"hstore/backend/internal/store"
)

// PoolNode 是聚合器上报的一个下游家宽节点运行状态。
type PoolNode struct {
	Index     int    `json:"index"`
	Label     string `json:"label"`
	Idle      int    `json:"idle"` // 待命连接数
	Healthy   bool   `json:"healthy"`
	LatencyMs int64  `json:"latencyMs"`
	LastSeen  string `json:"lastSeen,omitempty"`
}

// PoolStatus 是聚合器整体状态。
type PoolStatus struct {
	StatsAddress string     `json:"statsAddress"`
	Listen       string     `json:"listen"`
	Tunnel       string     `json:"tunnel"`
	Reached      bool       `json:"reached"`
	Error        string     `json:"error,omitempty"`
	Nodes        []PoolNode `json:"nodes"`
	Healthy      int        `json:"healthy"`
	TotalNodes   int        `json:"totalNodes"`
}

// poolStatsAddr 返回聚合器状态接口地址。
func poolStatsAddr() string {
	if v := strings.TrimSpace(os.Getenv("SOCKS_POOL_STATS")); v != "" {
		return v
	}
	return "http://172.20.0.1:18089"
}

// CheckPool 读取 sockspool 的节点状态。
func CheckPool() PoolStatus {
	addr := poolStatsAddr()
	// Nodes 预置空切片：nil 会被序列化成 null，前端 .length 会崩
	st := PoolStatus{StatsAddress: addr, Nodes: []PoolNode{}}

	client := &http.Client{Timeout: 4 * time.Second}
	// 聚合器在宿主机上，不能被环境变量里的代理绕出去
	client.Transport = &http.Transport{Proxy: nil}

	resp, err := client.Get(addr + "/stats")
	if err != nil {
		st.Error = "无法连接聚合器（sockspool 未运行？）：" + err.Error()
		return st
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		st.Error = fmt.Sprintf("聚合器返回 %d", resp.StatusCode)
		return st
	}
	var raw struct {
		Listen string `json:"listen"`
		Tunnel string `json:"tunnel"`
		Nodes  []struct {
			Index   int    `json:"index"`
			Label   string `json:"label"`
			Idle    int    `json:"idle"`
			Healthy bool   `json:"healthy"`
			Latency int64  `json:"latencyMs"`
			Seen    string `json:"lastSeen"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		st.Error = "解析聚合器状态失败：" + err.Error()
		return st
	}
	st.Reached = true
	st.Listen = raw.Listen
	st.Tunnel = raw.Tunnel
	for _, n := range raw.Nodes {
		node := PoolNode{
			Index: n.Index, Label: n.Label, Idle: n.Idle,
			Healthy: n.Healthy, LatencyMs: n.Latency, LastSeen: n.Seen,
		}
		if node.Healthy {
			st.Healthy++
		}
		st.Nodes = append(st.Nodes, node)
	}
	st.TotalNodes = len(st.Nodes)
	sort.Slice(st.Nodes, func(i, j int) bool { return st.Nodes[i].Index < st.Nodes[j].Index })
	return st
}

// BuildOnboardScript 生成家宽接入脚本（方案 B：agent 直连，不用 SSH）。
//
// 脚本做的事：
//  1. 下载 hshop-agent 二进制（从本服务 /api/pool/agent 下载，与镜像同版本）
//  2. 用内联的 token 写一个 systemd 服务
//  3. 启动，此后由 agent 自己维持长连接到服务器隧道端口
//
// 不需要 SSH、不需要改 authorized_keys、不需要公网 IP。
func BuildOnboardScript(serverHost string, node store.PoolNode) string {
	tunnel := store.PoolTunnelAddr()
	if tunnel == "" {
		tunnel = serverHost
		if i := strings.LastIndex(tunnel, ":"); i > 0 {
			tunnel = tunnel[:i]
		}
		tunnel += ":18090"
	}
	label := node.Label
	if label == "" {
		label = "node-" + fmt.Sprint(node.Index)
	}

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# HShop 家宽出口节点接入脚本\n")
	b.WriteString("# 节点 " + fmt.Sprint(node.Index) + "（" + label + "）\n")
	b.WriteString("# 原理：agent 主动连出到服务器隧道端口，不需要公网 IP / SSH / 加白\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("NODE_INDEX=" + fmt.Sprint(node.Index) + "\n")
	b.WriteString("NODE_LABEL=" + shellQuote(label) + "\n")
	b.WriteString("TUNNEL_ADDR=" + shellQuote(tunnel) + "\n")
	b.WriteString("AGENT_TOKEN=" + shellQuote(node.Token) + "\n")
	base := serverHost
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	b.WriteString("DOWNLOAD_URL=" + shellQuote(base+"/api/pool/agent") + "\n\n")

	b.WriteString(`echo "[1/4] 下载 hshop-agent"
mkdir -p /usr/local/bin
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$DOWNLOAD_URL" -o /usr/local/bin/hshop-agent
elif command -v wget >/dev/null 2>&1; then
  wget -qO /usr/local/bin/hshop-agent "$DOWNLOAD_URL"
else
  echo "需要 curl 或 wget，请先安装" >&2
  exit 1
fi
chmod +x /usr/local/bin/hshop-agent

echo "[2/4] 写入 systemd 服务"
cat > /etc/systemd/system/hshop-agent.service <<SVCEOF
[Unit]
Description=HShop home-broadband exit agent (node ${NODE_INDEX})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/hshop-agent -server ${TUNNEL_ADDR} -token ${AGENT_TOKEN} -pool 16
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SVCEOF

echo "[3/4] 启动"
systemctl daemon-reload
systemctl enable --now hshop-agent.service >/dev/null 2>&1 || true
sleep 3

echo "[4/4] 完成"
systemctl is-active hshop-agent.service || true
if systemctl is-active --quiet hshop-agent.service; then
  echo "节点 ${NODE_INDEX}（${NODE_LABEL}）已接入：${TUNNEL_ADDR}"
  echo "回到后台「系统设置 → 家宽出口节点」可看到该节点在线。"
else
  echo "启动失败，请执行 journalctl -u hshop-agent -n 30 查看原因" >&2
  exit 1
fi
`)
	return b.String()
}

// shellQuote 用单引号包裹并转义，避免注入。
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// BuildOnboardCommand 生成后台可直接复制的单行命令。
func BuildOnboardCommand(serverHost string, node store.PoolNode) string {
	base := serverHost
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return fmt.Sprintf(`bash -c "$(curl -fsSL '%s/api/pool/install.sh?node=%d')"`, base, node.Index)
}
