package upstream

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// PoolNode 是聚合器里的一个下游家宽节点。
//
// 家宽通过 SSH 反向隧道把自己的 SOCKS5 暴露到服务器的独立端口
// （18081、18082…），sockspool 在 18080 上统一收口并做故障转移。
type PoolNode struct {
	Index     int    `json:"index"`     // 节点序号，对应 18080+N 的端口
	Address   string `json:"address"`   // 127.0.0.1:18081
	Healthy   bool   `json:"healthy"`   // 聚合器探测结果
	LatencyMs int64  `json:"latencyMs"` // SOCKS5 握手延迟
	LastError string `json:"lastError,omitempty"`
	Total     int64  `json:"total"`   // 经过该节点的连接数
	Success   int64  `json:"success"` // 成功数
	Failed    int64  `json:"failed"`  // 失败数
	LastProbe string `json:"lastProbe,omitempty"`
}

// PoolStatus 是聚合器整体状态。
type PoolStatus struct {
	// StatsAddress 是聚合器状态接口地址（容器内可见的地址）。
	StatsAddress string `json:"statsAddress"`
	// Listen 是对外收口的 SOCKS5 地址。
	Listen  string     `json:"listen"`
	Reached bool       `json:"reached"` // 是否成功读到聚合器状态
	Error   string     `json:"error,omitempty"`
	Nodes   []PoolNode `json:"nodes"`
	// Healthy 是健康节点数。
	Healthy int `json:"healthy"`
	// TotalNodes 是配置的节点总数。
	TotalNodes int `json:"totalNodes"`
}

// poolStatsAddr 返回聚合器状态接口地址。
// 默认走 172.20.0.1（docker 网桥网关）——聚合器只监听宿主的 127.0.0.1，
// 容器内需要用网关地址访问。
func poolStatsAddr() string {
	if v := strings.TrimSpace(os.Getenv("SOCKS_POOL_STATS")); v != "" {
		return v
	}
	return "http://172.20.0.1:18089"
}

// CheckPool 读取 sockspool 的节点状态。
// 聚合器不可达时返回 Reached=false，前端据此提示「聚合器未运行」。
func CheckPool() PoolStatus {
	addr := poolStatsAddr()
	// Nodes 预置为空切片：nil 会被序列化成 null，前端 .length 会直接崩。
	st := PoolStatus{StatsAddress: addr, Nodes: []PoolNode{}}
	client := &http.Client{Timeout: 4 * time.Second}
	// 聚合器在宿主本机，不能被环境变量里的代理绕出去
	client.Transport = &http.Transport{Proxy: nil}

	resp, err := client.Get(addr + "/stats")
	if err != nil {
		st.Error = "无法连接聚合器：" + err.Error()
		return st
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		st.Error = fmt.Sprintf("聚合器返回 %d", resp.StatusCode)
		return st
	}
	var raw struct {
		Listen string `json:"listen"`
		Nodes  []struct {
			Address   string `json:"address"`
			Healthy   bool   `json:"healthy"`
			LatencyMs int64  `json:"latencyMs"`
			LastError string `json:"lastError"`
			Total     int64  `json:"total"`
			Success   int64  `json:"success"`
			Failed    int64  `json:"failed"`
			LastProbe string `json:"lastProbe"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		st.Error = "解析聚合器状态失败：" + err.Error()
		return st
	}
	st.Reached = true
	st.Listen = raw.Listen
	for _, n := range raw.Nodes {
		idx := 0
		if _, port, e := net.SplitHostPort(n.Address); e == nil {
			var p int
			if _, e := fmt.Sscanf(port, "%d", &p); e == nil && p > 18080 {
				idx = p - 18080
			}
		}
		node := PoolNode{
			Index: idx, Address: n.Address, Healthy: n.Healthy,
			LatencyMs: n.LatencyMs, LastError: n.LastError,
			Total: n.Total, Success: n.Success, Failed: n.Failed, LastProbe: n.LastProbe,
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

// NodePort 返回节点序号对应的服务器端口。
func NodePort(index int) int { return 18080 + index }

// OnboardCommand 是给新家宽设备执行的一键接入命令。
//
// 设计目标：在全新的 Linux 设备上粘贴一条命令，就能把该设备变成
// 一个可用的下游节点（自建 SOCKS5 + systemd 常驻 + SSH 反向隧道）。
type OnboardCommand struct {
	Index int    `json:"index"`
	Port  int    `json:"port"`
	Shell string `json:"shell"` // 完整可粘贴的 shell 命令
}

// BuildOnboardScript 生成家宽接入脚本的内容（供 /api/admin/pool/onboard.sh 下发）。
//
// 参数全部内联，避免新设备还要手工改配置。
func BuildOnboardScript(serverHost, sshUser string, index int) string {
	port := NodePort(index)
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# HShop 下游家宽节点接入脚本（节点 " + fmt.Sprint(index) + "，服务器端口 " + fmt.Sprint(port) + "）\n")
	b.WriteString("# 用法：curl -fsSL <此脚本地址> | bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("NODE_INDEX=" + fmt.Sprint(index) + "\n")
	b.WriteString("SERVER_HOST=" + serverHost + "\n")
	b.WriteString("SSH_USER=" + sshUser + "\n")
	b.WriteString("NODE_PORT=" + fmt.Sprint(port) + "\n")
	b.WriteString("SOCKS_PORT=1080\n")
	b.WriteString("KEY=/root/.ssh/id_ed25519_hshop\n\n")
	b.WriteString(`echo "[1/4] 安装依赖"
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -qq && apt-get install -y -qq openssh-client python3 curl >/dev/null
elif command -v apk >/dev/null 2>&1; then
  apk add --no-cache openssh-client python3 curl >/dev/null
fi

echo "[2/4] 生成 SSH 密钥（若不存在）"
mkdir -p /root/.ssh && chmod 700 /root/.ssh
[ -f "$KEY" ] || ssh-keygen -t ed25519 -N "" -f "$KEY" -q
mkdir -p ~/.ssh
[ -f "$KEY.pub" ] && cat "$KEY.pub"

echo "[3/4] 安装本地 SOCKS5 与反向隧道服务"
cat > /usr/local/bin/hshop-socks5.py <<'PYEOF'
import socket, struct, threading, select
def pipe(c, r):
    socks=[c,r]
    try:
        while True:
            rd,_,_=select.select(socks,[],[],60)
            if not rd: continue
            for s in rd:
                d=s.recv(65536)
                if not d: return
                (r if s is c else c).sendall(d)
    except Exception: pass
    finally:
        for s in (c,r):
            try: s.close()
            except Exception: pass
def handle(c):
    try:
        c.recv(262); c.sendall(b"\x05\x00")
        ver,cmd,_,atyp=c.recv(4)
        if atyp==1: addr=socket.inet_ntoa(c.recv(4))
        elif atyp==3: addr=c.recv(c.recv(1)[0]).decode()
        else: addr=socket.inet_ntop(socket.AF_INET6,c.recv(16))
        port=struct.unpack("!H",c.recv(2))[0]
        r=socket.create_connection((addr,port),15)
        c.sendall(b"\x05\x00\x00\x01"+socket.inet_aton("0.0.0.0")+struct.pack("!H",0))
        threading.Thread(target=pipe,args=(c,r),daemon=True).start()
    except Exception:
        try: c.close()
        except Exception: pass
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
s.bind(("127.0.0.1",1080)); s.listen(128)
while True:
    conn,_=s.accept(); threading.Thread(target=handle,args=(conn,),daemon=True).start()
PYEOF
chmod +x /usr/local/bin/hshop-socks5.py

cat > /etc/systemd/system/hshop-socks5.service <<'SVCEOF'
[Unit]
Description=HShop local SOCKS5 for reverse tunnel
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
ExecStart=/usr/bin/python3 /usr/local/bin/hshop-socks5.py
Restart=always
RestartSec=3
[Install]
WantedBy=multi-user.target
SVCEOF

cat > /etc/systemd/system/hshop-tunnel.service <<SVCEOF
[Unit]
Description=HShop reverse SOCKS5 tunnel (node $NODE_INDEX -> port $NODE_PORT)
After=network-online.target hshop-socks5.service
Wants=network-online.target
Requires=hshop-socks5.service
[Service]
Type=simple
ExecStart=/usr/bin/ssh -i $KEY -o StrictHostKeyChecking=no -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 -o ServerAliveCountMax=5 -o TCPKeepAlive=yes -N -R 0.0.0.0:$NODE_PORT:127.0.0.1:$SOCKS_PORT $SSH_USER@$SERVER_HOST
Restart=always
RestartSec=5
[Install]
WantedBy=multi-user.target
SVCEOF

systemctl daemon-reload
systemctl enable --now hshop-socks5.service >/dev/null
systemctl enable --now hshop-tunnel.service >/dev/null
sleep 3

echo "[4/4] 完成"
systemctl is-active hshop-socks5.service hshop-tunnel.service
echo "节点 $NODE_INDEX 已接入：$SERVER_HOST:$NODE_PORT"
`)
	return b.String()
}

// BuildOnboardOneLiner 生成单行安装命令（后台直接复制粘贴用）。
func BuildOnboardOneLiner(serverHost, sshUser string, index int) string {
	return fmt.Sprintf(
		`bash -c "$(curl -fsSL http://%s/api/pool/install.sh?node=%d)"; echo '若提示需要授权，请把上面输出的公钥加到服务器 /root/.ssh/authorized_keys（可用一键命令自动完成）'`,
		serverHost, index)
}
