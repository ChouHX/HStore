// Command agent 是下游家宽设备的就近出口代理（hshop-agent）。
//
// 背景
//
//	上游按出口 IP 拦海外机房，所以服务器出网必须借家宽。家宽普遍在 NAT
//	后面、没有公网 IP，因此只能是「设备主动连出去」。
//
// 工作方式
//
//	agent 连到服务器的隧道端口，报上自己的 token 完成认领，然后维持一组
//	空闲连接待命。服务器收到容器发来的 SOCKS5 请求时，取走一条空闲连接，
//	下发 "CONNECT host port"，agent 在本机拨号并把字节流原样回传。
//
//	一条连接只承载一次隧道，用完即弃，agent 随即补足空闲连接数。这样
//	服务器侧不需要实现多路复用，协议只有两行文本，极难出错。
//
// 用法
//
//	agent -server tunnel.example.com:18090 -token <节点token> -pool 16
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

const agentVersion = "hshop-agent/1"

var (
	serverAddr string
	token      string
	poolSize   int
	dialTarget time.Duration
)

// 统计，便于排查
var (
	statServe  atomic.Int64 // 成功承载的隧道数
	statErr    atomic.Int64 // 出错次数
	statActive atomic.Int64 // 当前活跃隧道数
)

func main() {
	var (
		server = flag.String("server", "", "服务器隧道地址 host:port")
		tok    = flag.String("token", "", "节点 token（后台「家宽出口节点」里复制）")
		pool   = flag.Int("pool", 16, "保持的空闲连接数")
		dialTO = flag.Int("dial-timeout", 15, "连接目标站点的超时（秒）")
		debug  = flag.Bool("v", false, "输出详细日志")
	)
	flag.Parse()

	if strings.TrimSpace(*server) == "" || strings.TrimSpace(*tok) == "" {
		log.Fatal("[agent] 必须提供 -server 与 -token")
	}
	serverAddr = strings.TrimSpace(*server)
	token = strings.TrimSpace(*tok)
	poolSize = *pool
	if poolSize < 1 {
		poolSize = 1
	}
	dialTarget = time.Duration(*dialTO) * time.Second

	if *debug {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}
	log.Printf("[agent] %s 连接 %s，空闲连接数 %d", agentVersion, serverAddr, poolSize)

	// 保持 poolSize 条空闲连接：每次用完都会补一条，断线也会补。
	for i := 0; i < poolSize; i++ {
		go maintain()
	}

	// 定期打印统计
	go func() {
		for range time.Tick(2 * time.Minute) {
			log.Printf("[agent] 统计：承载 %d 次，活跃 %d，错误 %d",
				statServe.Load(), statActive.Load(), statErr.Load())
		}
	}()

	select {}
}

// maintain 维持一条空闲连接循环：连上 → 待命 → 用完 → 重连。
func maintain() {
	backoff := time.Second
	for {
		if err := runOnce(); err != nil {
			statErr.Add(1)
			log.Printf("[agent] 连接异常: %v（%s 后重试）", err, backoff)
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

// runOnce 建立一条连接并处理直到被服务器取用或断开。
func runOnce() error {
	conn, err := net.DialTimeout("tcp", serverAddr, 15*time.Second)
	if err != nil {
		return fmt.Errorf("拨号服务器失败: %w", err)
	}
	defer conn.Close()

	// 认领：报告版本与 token
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s %s\n", agentVersion, token); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("等待服务器应答失败: %w", err)
	}
	line = strings.TrimSpace(line)
	if line != "OK" {
		// token 无效等服务器拒绝的情形，重试无意义但要避免刷屏
		return fmt.Errorf("服务器拒绝: %s", line)
	}

	// 待命：等服务器下发 CONNECT
	_ = conn.SetDeadline(time.Time{})
	cmd, err := br.ReadString('\n')
	if err != nil {
		return nil // 服务器主动关闭空闲连接，属正常情况
	}
	cmd = strings.TrimSpace(cmd)
	if !strings.HasPrefix(cmd, "CONNECT ") {
		return fmt.Errorf("未知指令: %s", cmd)
	}
	parts := strings.Fields(cmd)
	if len(parts) != 3 {
		return fmt.Errorf("CONNECT 参数不对: %s", cmd)
	}
	target := net.JoinHostPort(parts[1], parts[2])

	// 本机拨号目标站点（用家宽出口）
	up, err := net.DialTimeout("tcp", target, dialTarget)
	if err != nil {
		_, _ = fmt.Fprintf(conn, "ERR %s\n", sanitize(err.Error()))
		return nil
	}
	defer up.Close()

	if _, err := fmt.Fprintf(conn, "OK\n"); err != nil {
		return err
	}
	statActive.Add(1)
	statServe.Add(1)
	defer statActive.Add(-1)

	// 双向转发。注意：bufio 里可能有已缓冲的数据，必须经 br 读。
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(up, br)
		if c, ok := up.(*net.TCPConn); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, up)
		if c, ok := conn.(*net.TCPConn); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	return nil
}

// sanitize 让错误文本能安全地放在单行协议里。
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
