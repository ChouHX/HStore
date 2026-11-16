// Command sockspool 是一个带故障转移的 SOCKS5 汇聚器。
//
// 背景
//
//	上游天启按出口 IP 拦海外机房，所以容器的出网必须走家宽。家宽通过 SSH
//	反向隧道把各自的 SOCKS5 暴露到服务器的 127.0.0.1:<port>。单节点的问题很
//	直接：某个家宽掉线（关机 / 断网 / 换 IP），整条链路就断了。
//
// 做法
//
//	对外只暴露一个 SOCKS5 端口（默认 18080），内部持有一组上游节点
//	（127.0.0.1:18081、18082…）。每次有连接进来时按「健康 + 延迟」顺序挑一个
//	节点；连接失败立刻换下一个，直到成功或全部失败。容器侧不需要任何改动。
//
//	健康探测定期跑，结果用于排序；刚失败的节点会被短暂降权（冷却），避免每
//	个新连接都先撞一次坏节点。
//
// 用法
//
//	sockspool -listen 0.0.0.0:18080 \
//	          -upstream 127.0.0.1:18081 \
//	          -upstream 127.0.0.1:18082 \
//	          -upstream 127.0.0.1:18083
package main

import (
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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// 探测周期：多久检查一次各节点是否还能握手。
	probeInterval = 30 * time.Second
	// 单次探测超时。
	probeTimeout = 3 * time.Second
	// 失败后的冷却期：这段时间内该节点排在最后。
	failCooldown = 60 * time.Second
	// 建连上游节点的超时。
	dialTimeout = 8 * time.Second
)

// node 是一个上游 SOCKS5 节点。
type node struct {
	addr string

	mu        sync.Mutex
	healthy   bool
	latency   time.Duration
	failedAt  time.Time
	lastProbe time.Time
	lastErr   string

	// 统计用
	total   atomic.Int64
	success atomic.Int64
	failed  atomic.Int64
}

func (n *node) snapshot() (healthy bool, latency time.Duration, failedAt time.Time, errMsg string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.healthy, n.latency, n.failedAt, n.lastErr
}

func (n *node) markOK(latency time.Duration) {
	n.mu.Lock()
	n.healthy = true
	n.latency = latency
	n.lastProbe = time.Now()
	n.lastErr = ""
	n.mu.Unlock()
}

func (n *node) markFail(err error) {
	n.mu.Lock()
	n.healthy = false
	n.failedAt = time.Now()
	n.lastProbe = time.Now()
	if err != nil {
		n.lastErr = err.Error()
	}
	n.mu.Unlock()
}

// pool 管理所有节点。
type pool struct {
	nodes []*node
	rr    atomic.Uint64
}

// ordered 返回本轮尝试顺序：健康的、延迟低的在前；刚失败过的排到最后。
func (p *pool) ordered() []*node {
	type scored struct {
		n       *node
		penalty int // 0 = 健康，1 = 冷却中，2 = 不健康
		latency time.Duration
	}
	now := time.Now()
	list := make([]scored, 0, len(p.nodes))
	for _, n := range p.nodes {
		healthy, latency, failedAt, _ := n.snapshot()
		switch {
		case healthy:
			list = append(list, scored{n: n, penalty: 0, latency: latency})
		case !failedAt.IsZero() && now.Sub(failedAt) < failCooldown:
			// 冷却期内：仍然可用（也许已经恢复），但排在最后
			list = append(list, scored{n: n, penalty: 1, latency: 99 * time.Second})
		default:
			list = append(list, scored{n: n, penalty: 2, latency: 99 * time.Second})
		}
	}
	// 同分时轮换起点，避免总是压同一个节点
	start := int(p.rr.Add(1)) % len(list)
	if len(list) > 0 && start > 0 {
		list = append(list[start:], list[:start]...)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].penalty != list[j].penalty {
			return list[i].penalty < list[j].penalty
		}
		return list[i].latency < list[j].latency
	})
	out := make([]*node, 0, len(list))
	for _, s := range list {
		out = append(out, s.n)
	}
	return out
}

// probeLoop 定期探测每个节点。
func (p *pool) probeLoop(ctx context.Context) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	p.probeAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probeAll(ctx)
		}
	}
}

func (p *pool) probeAll(ctx context.Context) {
	for _, n := range p.nodes {
		go func(n *node) {
			start := time.Now()
			conn, err := net.DialTimeout("tcp", n.addr, probeTimeout)
			if err != nil {
				n.markFail(err)
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(probeTimeout))
			// SOCKS5 无认证握手
			if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
				n.markFail(err)
				return
			}
			reply := make([]byte, 2)
			if _, err := io.ReadFull(conn, reply); err != nil {
				n.markFail(err)
				return
			}
			if reply[0] != 0x05 || reply[1] == 0xFF {
				n.markFail(errors.New("SOCKS5 握手被拒绝"))
				return
			}
			n.markOK(time.Since(start))
		}(n)
	}
}

// dialVia 通过指定 SOCKS5 节点连到 target。
func dialVia(ctx context.Context, nodeAddr, target string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", nodeAddr)
	if err != nil {
		return nil, err
	}
	// 握手 + CONNECT 有整体超时，避免卡在半开连接上
	deadline := time.Now().Add(dialTimeout)
	_ = conn.SetDeadline(deadline)

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		conn.Close()
		return nil, err
	}
	port, err := parsePort(portStr)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		return nil, err
	}
	if reply[0] != 0x05 || reply[1] == 0xFF {
		conn.Close()
		return nil, errors.New("SOCKS5 认证失败")
	}

	// CONNECT 请求：优先用域名（让家宽侧解析，避免本地 DNS 造成地域偏差）
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 0x01)
			req = append(req, v4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			conn.Close()
			return nil, errors.New("域名过长")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, port)
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	// 应答：VER REP RSV ATYP BND.ADDR BND.PORT
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		conn.Close()
		return nil, err
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 CONNECT 失败，REP=0x%02x", head[1])
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(conn, lb); err != nil {
			conn.Close()
			return nil, err
		}
		skip = int(lb[0])
	case 0x04:
		skip = 16
	default:
		conn.Close()
		return nil, errors.New("SOCKS5 应答地址类型未知")
	}
	if _, err := io.ReadFull(conn, make([]byte, skip+2)); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func parsePort(s string) (uint16, error) {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil || p <= 0 || p > 65535 {
		return 0, errors.New("端口不合法")
	}
	return uint16(p), nil
}

// handle 处理一个来自容器的 SOCKS5 连接：先读完客户端的 CONNECT 请求，
// 再按节点优先级依次尝试。
func (p *pool) handle(client net.Conn) {
	defer client.Close()

	_ = client.SetDeadline(time.Now().Add(30 * time.Second))

	// 客户端握手
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
	// 只支持无认证
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// CONNECT 请求
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

	// 依次尝试节点
	var upstream net.Conn
	var used *node
	for _, n := range p.ordered() {
		n.total.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
		conn, err := dialVia(ctx, n.addr, target)
		cancel()
		if err != nil {
			n.failed.Add(1)
			n.markFail(err)
			continue
		}
		n.success.Add(1)
		upstream = conn
		used = n
		break
	}
	if upstream == nil {
		_, _ = client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	if used != nil && !used.isHealthy() {
		used.markOK(0)
	}

	// 告诉客户端成功
	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})

	// 双向转发
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
	<-done
}

func (n *node) isHealthy() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.healthy
}

// statsHandler 输出各节点状态（供后台监控读取）。
type nodeStat struct {
	Address  string `json:"address"`
	Healthy  bool   `json:"healthy"`
	Latency  int64  `json:"latencyMs"`
	LastErr  string `json:"lastError,omitempty"`
	Total    int64  `json:"total"`
	Success  int64  `json:"success"`
	Failed   int64  `json:"failed"`
	LastSeen string `json:"lastProbe,omitempty"`
}

func (p *pool) stats() []nodeStat {
	out := make([]nodeStat, 0, len(p.nodes))
	for _, n := range p.nodes {
		n.mu.Lock()
		st := nodeStat{
			Address: n.addr,
			Healthy: n.healthy,
			Latency: n.latency.Milliseconds(),
			LastErr: n.lastErr,
			Total:   n.total.Load(),
			Success: n.success.Load(),
			Failed:  n.failed.Load(),
		}
		if !n.lastProbe.IsZero() {
			st.LastSeen = n.lastProbe.Format(time.RFC3339)
		}
		n.mu.Unlock()
		out = append(out, st)
	}
	return out
}

func main() {
	var (
		listen    = flag.String("listen", "0.0.0.0:18080", "对外监听地址（容器连这个端口）")
		upstreams multiFlag
		statAddr  = flag.String("stats", "127.0.0.1:18089", "状态接口地址（GET /stats 返回 JSON）")
	)
	flag.Var(&upstreams, "upstream", "上游节点，可重复指定，如 127.0.0.1:18081")
	flag.Parse()

	if len(upstreams) == 0 {
		log.Fatal("[sockspool] 至少需要一个 -upstream 节点")
	}

	p := &pool{}
	for _, addr := range upstreams {
		p.nodes = append(p.nodes, &node{addr: addr})
	}

	log.Printf("[sockspool] 监听 %s，上游节点 %d 个: %s", *listen, len(p.nodes), strings.Join(upstreams, ", "))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.probeLoop(ctx)

	// 状态接口
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"listen": *listen,
				"nodes":  p.stats(),
			})
		})
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			for _, n := range p.nodes {
				if n.isHealthy() {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("ok"))
					return
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("no healthy node"))
		})
		srv := &http.Server{Addr: *statAddr, Handler: mux}
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("[sockspool] 状态接口退出: %v", err)
		}
	}()

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("[sockspool] 监听失败: %v", err)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[sockspool] accept 出错: %v", err)
			continue
		}
		go p.handle(conn)
	}
}

// multiFlag 支持 -upstream 重复多次。
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return errors.New("节点地址不能为空")
	}
	*m = append(*m, v)
	return nil
}
