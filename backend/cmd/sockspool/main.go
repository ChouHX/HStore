// Command sockspool 是带故障转移的 SOCKS5 汇聚器 + 家宽隧道服务端。
//
// 两条职责
//
//  1. 对外收口：容器只用 18080 这一个 SOCKS5 端口，内部在多个家宽节点之间
//     做健康排序与故障转移。
//  2. 隧道接入：家宽设备（hshop-agent）主动连到 18090 报 token，服务端把它
//     登记成一个可用节点。设备在 NAT 后面也能接入，不需要公网 IP、不需要
//     SSH、不需要改 authorized_keys。
//
// 为什么要两层端口
//
//	sshd 不允许两个客户端绑定同一个 -R 端口（实测 remote port forwarding
//	failed），所以「一个端口收多个家宽」必须由本进程收口。原来是靠 SSH
//	反向隧道把每个家宽暴露成 127.0.0.1:18080+N，现在改成 agent 直连，
//	少一层依赖，也不用管密钥授权。
//
// 用法
//
//	sockspool -listen 0.0.0.0:18080 -tunnel 0.0.0.0:18090 \
//	          -stats 127.0.0.1:18089
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	agentPrefix = "hshop-agent"
	dialTimeout = 10 * time.Second
	// 空闲连接到多久没被取用就回收（agent 会补新的）
	idleTTL = 90 * time.Second
)

// tokenStore 保存「token -> 节点信息」，由主程序从文件加载并在变更时刷新。
type tokenStore struct {
	mu    sync.RWMutex
	nodes map[string]nodeMeta // token -> meta
}

type nodeMeta struct {
	Index int    `json:"index"`
	Label string `json:"label"`
}

func newTokenStore() *tokenStore {
	return &tokenStore{nodes: map[string]nodeMeta{}}
}

func (t *tokenStore) replace(m map[string]nodeMeta) {
	t.mu.Lock()
	t.nodes = m
	t.mu.Unlock()
}

func (t *tokenStore) lookup(token string) (nodeMeta, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	v, ok := t.nodes[token]
	return v, ok
}

// tunnelConn 是一条已认证、等待被取用的空闲连接。
type tunnelConn struct {
	conn    net.Conn
	br      *bufio.Reader
	meta    nodeMeta
	created time.Time
	claimed bool
	claimMu sync.Mutex
}

// tryClaim 标记该连接被取用；并发下只有一个请求能拿到。
func (c *tunnelConn) tryClaim() bool {
	c.claimMu.Lock()
	defer c.claimMu.Unlock()
	if c.claimed {
		return false
	}
	c.claimed = true
	return true
}

// tunnelNode 是一个在线节点：持有若干空闲连接。
type tunnelNode struct {
	meta nodeMeta

	mu    sync.Mutex
	idle  []*tunnelConn
	last  time.Time // 最后一次有任何活动
	conns atomic.Int64
}

// take 取走一条空闲连接；没有则返回 nil。
func (n *tunnelNode) take() *tunnelConn {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	for len(n.idle) > 0 {
		c := n.idle[len(n.idle)-1]
		n.idle = n.idle[:len(n.idle)-1]
		// 过期的直接丢掉
		if now.Sub(c.created) > idleTTL {
			_ = c.conn.Close()
			continue
		}
		if c.tryClaim() {
			return c
		}
	}
	return nil
}

func (n *tunnelNode) add(c *tunnelConn) {
	n.mu.Lock()
	n.idle = append(n.idle, c)
	n.last = time.Now()
	n.mu.Unlock()
	n.conns.Add(1)
}

func (n *tunnelNode) drop(c *tunnelConn) {
	n.mu.Lock()
	for i, v := range n.idle {
		if v == c {
			n.idle = append(n.idle[:i], n.idle[i+1:]...)
			break
		}
	}
	n.mu.Unlock()
}

func (n *tunnelNode) idleCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.idle)
}

// registry 管理所有在线节点。
type registry struct {
	mu    sync.RWMutex
	nodes map[int]*tunnelNode // index -> node
	toks  *tokenStore
}

func newRegistry(toks *tokenStore) *registry {
	return &registry{nodes: map[int]*tunnelNode{}, toks: toks}
}

func (r *registry) getOrCreate(meta nodeMeta) *tunnelNode {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.nodes[meta.Index]; ok {
		n.meta = meta
		return n
	}
	n := &tunnelNode{meta: meta, last: time.Now()}
	r.nodes[meta.Index] = n
	return n
}

// ordered 返回按健康度排序的节点：有空闲连接的优先。
func (r *registry) ordered() []*tunnelNode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*tunnelNode, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, n)
	}
	// 空闲连接多的排前面；相同则轮换，避免总压一个家宽
	sortNodes(out)
	return out
}

// snapshot 生成状态列表。
func (r *registry) snapshot() []nodeStat {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]nodeStat, 0, len(r.nodes))
	for _, n := range r.nodes {
		st := nodeStat{
			Index:    n.meta.Index,
			Label:    n.meta.Label,
			Idle:     n.idleCount(),
			Healthy:  false,
			LastSeen: n.last.Format(time.RFC3339),
		}
		st.Healthy = st.Idle > 0
		out = append(out, st)
	}
	sortStat(out)
	return out
}

// handleTunnel 处理一条来自 agent 的连接。
func (r *registry) handleTunnel(conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) != 2 || !strings.HasPrefix(fields[0], agentPrefix) {
		_, _ = conn.Write([]byte("ERR bad handshake\n"))
		return
	}
	meta, ok := r.toks.lookup(fields[1])
	if !ok {
		_, _ = conn.Write([]byte("ERR invalid token\n"))
		return
	}
	if _, err := conn.Write([]byte("OK\n")); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})

	node := r.getOrCreate(meta)
	tc := &tunnelConn{conn: conn, br: br, meta: meta, created: time.Now()}
	node.add(tc)

	// 等待被取用：被取用后 conn 交给请求处理方，这里不再关闭
	// 通过读取探测判断连接是否还活着：agent 不会主动发数据，
	// 所以用 SetReadDeadline 触发一次可写检查即可。
	for {
		time.Sleep(2 * time.Second)
		tc.claimMu.Lock()
		claimed := tc.claimed
		tc.claimMu.Unlock()
		if claimed {
			// 交给使用者，禁止本函数关闭连接
			return
		}
		// 连接被 agent 关闭时，这里能读到 EOF
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		if _, err := br.Peek(1); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				_ = conn.SetReadDeadline(time.Time{})
				continue
			}
			node.drop(tc)
			return
		}
	}
}

// dialViaTunnel 通过节点建立到 target 的隧道。
func dialViaTunnel(node *tunnelNode, target string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	tc := node.take()
	if tc == nil {
		return nil, errors.New("节点无空闲连接")
	}
	log.Printf("[sockspool] 节点 %d 取到空闲连接，握手 %s", node.meta.Index, target)
	// 告诉 agent 拨号目标
	if _, err := fmt.Fprintf(tc.conn, "CONNECT %s %s\n", host, port); err != nil {
		_ = tc.conn.Close()
		return nil, err
	}
	_ = tc.conn.SetReadDeadline(time.Now().Add(dialTimeout))
	line, err := tc.br.ReadString('\n')
	if err != nil {
		_ = tc.conn.Close()
		return nil, err
	}
	_ = tc.conn.SetReadDeadline(time.Time{})
	if strings.TrimSpace(line) != "OK" {
		_ = tc.conn.Close()
		return nil, fmt.Errorf("agent 拨号失败: %s", strings.TrimSpace(line))
	}
	// 把已缓冲的数据也算进来：包一层 ReadCloser 用 br 读
	return &bufferedConn{Conn: tc.conn, br: tc.br}, nil
}

// bufferedConn 让后续读取先消费 bufio 中已缓冲的字节。
type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.br.Read(p) }

// ===== 对外 SOCKS5 收口 =====

func (r *registry) handleClient(client net.Conn) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))

	head := make([]byte, 2)
	if _, err := io.ReadFull(client, head); err != nil {
		return
	}
	if head[0] != 0x05 {
		return
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	reqHead := make([]byte, 4)
	if _, err := io.ReadFull(client, reqHead); err != nil {
		return
	}
	if reqHead[0] != 0x05 || reqHead[1] != 0x01 {
		_, _ = client.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	var host string
	switch reqHead[3] {
	case 0x01:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(client, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(client, lb); err != nil {
			return
		}
		buf := make([]byte, int(lb[0]))
		if _, err := io.ReadFull(client, buf); err != nil {
			return
		}
		host = string(buf)
	case 0x04:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(client, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	default:
		return
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(client, portBuf); err != nil {
		return
	}
	target := net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(portBuf)))

	var upstream net.Conn
	for _, n := range r.ordered() {
		conn, err := dialViaTunnel(n, target)
		if err != nil {
			log.Printf("[sockspool] 节点 %d 拨号 %s 失败: %v", n.meta.Index, target, err)
			continue
		}
		upstream = conn
		break
	}
	if upstream == nil {
		_, _ = client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
	<-done
}

// ===== 状态接口 =====

type nodeStat struct {
	Index     int    `json:"index"`
	Label     string `json:"label"`
	Idle      int    `json:"idle"`
	Healthy   bool   `json:"healthy"`
	LatencyMs int64  `json:"latencyMs"`
	LastSeen  string `json:"lastSeen,omitempty"`
}

func main() {
	var (
		listen    = flag.String("listen", "0.0.0.0:18080", "对外 SOCKS5 监听地址（容器连这个）")
		tunnel    = flag.String("tunnel", "0.0.0.0:18090", "家宽 agent 接入端口")
		statsAddr = flag.String("stats", "127.0.0.1:18089", "状态接口地址")
		tokenFile = flag.String("tokens", "", "token 映射文件（JSON: {\"token\":{\"index\":1,\"label\":\"成都\"}}）")
	)
	flag.Parse()

	toks := newTokenStore()
	if *tokenFile != "" {
		if err := reloadTokens(*tokenFile, toks); err != nil {
			log.Printf("[sockspool] 读取 token 文件失败: %v", err)
		}
		// 定期热加载，后台增删节点后无需重启
		go func() {
			for range time.Tick(10 * time.Second) {
				if err := reloadTokens(*tokenFile, toks); err != nil {
					log.Printf("[sockspool] 热加载 token 失败: %v", err)
				}
			}
		}()
	}

	reg := newRegistry(toks)
	log.Printf("[sockspool] SOCKS5 %s，隧道 %s，状态 %s", *listen, *tunnel, *statsAddr)

	// 状态接口
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/stats", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			nodes := reg.snapshot()
			healthy := 0
			for _, n := range nodes {
				if n.Healthy {
					healthy++
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"listen":  *listen,
				"tunnel":  *tunnel,
				"nodes":   nodes,
				"healthy": healthy,
				"total":   len(nodes),
			})
		})
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
			for _, n := range reg.snapshot() {
				if n.Healthy {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("ok"))
					return
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("no healthy node"))
		})
		srv := &http.Server{Addr: *statsAddr, Handler: mux}
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("[sockspool] 状态接口退出: %v", err)
		}
	}()

	// 隧道接入
	tn, err := net.Listen("tcp", *tunnel)
	if err != nil {
		log.Fatalf("[sockspool] 隧道端口监听失败: %v", err)
	}
	go func() {
		for {
			c, err := tn.Accept()
			if err != nil {
				log.Printf("[sockspool] 隧道 accept 出错: %v", err)
				continue
			}
			go reg.handleTunnel(c)
		}
	}()

	// SOCKS5 收口
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("[sockspool] SOCKS5 监听失败: %v", err)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("[sockspool] accept 出错: %v", err)
			continue
		}
		go reg.handleClient(c)
	}
}

// reloadTokens 从文件加载 token 映射。
func reloadTokens(path string, toks *tokenStore) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]nodeMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if m == nil {
		m = map[string]nodeMeta{}
	}
	toks.replace(m)
	return nil
}

func sortNodes(list []*tunnelNode) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].idleCount() > list[j-1].idleCount(); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func sortStat(list []nodeStat) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Index < list[j-1].Index; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

var _ = context.Background
