package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hstore/backend/internal/db"
)

// SKPoolNodes 是下游家宽节点列表（JSON 数组）在 sync_meta 里的键。
const SKPoolNodes = "pool_nodes"

// SKPoolTunnel 是家宽 agent 接入的隧道地址（host:port）。
const SKPoolTunnel = "pool_tunnel_addr"

// PoolNode 是一个下游家宽节点。
//
// 家宽设备通过 hshop-agent 主动连到服务器的隧道端口并用 Token 认领自己，
// 因此不需要公网 IP、不需要 SSH、也不需要手工改 authorized_keys。
type PoolNode struct {
	Index int    `json:"index"` // 节点序号
	Label string `json:"label"` // 备注名，如「成都电信」
	Token string `json:"token"` // 节点凭证，agent 用它认领
	// Enabled 为 false 时该节点不从池子里派活，但保留配置。
	Enabled   bool  `json:"enabled"`
	CreatedAt int64 `json:"createdAt"`
}

// Port 返回该节点在状态面板上展示的端口号（仅作标识，实际走隧道）。
func (n PoolNode) Port() int { return 18080 + n.Index }

// ListPoolNodes 读取全部节点配置。
func ListPoolNodes() []PoolNode {
	raw := db.GetMeta(SKPoolNodes)
	if strings.TrimSpace(raw) == "" {
		return []PoolNode{}
	}
	var list []PoolNode
	if e := json.Unmarshal([]byte(raw), &list); e != nil {
		return []PoolNode{}
	}
	if list == nil {
		list = []PoolNode{}
	}
	return list
}

func savePoolNodes(list []PoolNode) error {
	if list == nil {
		list = []PoolNode{}
	}
	raw, e := json.Marshal(list)
	if e != nil {
		return e
	}
	return db.SetMeta(SKPoolNodes, string(raw))
}

// newPoolToken 生成节点凭证。
func newPoolToken() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return "hk_" + hex.EncodeToString(buf)
}

// AddPoolNode 新增节点，自动分配序号并生成 Token。
func AddPoolNode(label string) (PoolNode, error) {
	list := ListPoolNodes()
	used := map[int]bool{}
	for _, n := range list {
		used[n.Index] = true
	}
	idx := 1
	for used[idx] {
		idx++
	}
	node := PoolNode{
		Index:     idx,
		Label:     strings.TrimSpace(label),
		Token:     newPoolToken(),
		Enabled:   true,
		CreatedAt: now(),
	}
	list = append(list, node)
	if e := savePoolNodes(list); e != nil {
		return PoolNode{}, e
	}
	// 同步 token 文件，agent 才能认领
	_ = WritePoolTokens()
	return node, nil
}

// UpdatePoolNode 修改备注 / 启用状态。
func UpdatePoolNode(index int, label string, enabled bool) error {
	list := ListPoolNodes()
	found := false
	for i := range list {
		if list[i].Index == index {
			list[i].Label = strings.TrimSpace(label)
			list[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("节点 %d 不存在", index)
	}
	if e := savePoolNodes(list); e != nil {
		return e
	}
	_ = WritePoolTokens()
	return nil
}

// DeletePoolNode 删除节点配置。
func DeletePoolNode(index int) error {
	list := ListPoolNodes()
	out := make([]PoolNode, 0, len(list))
	found := false
	for _, n := range list {
		if n.Index == index {
			found = true
			continue
		}
		out = append(out, n)
	}
	if !found {
		return fmt.Errorf("节点 %d 不存在", index)
	}
	if e := savePoolNodes(out); e != nil {
		return e
	}
	_ = WritePoolTokens()
	return nil
}

// GetPoolNode 按序号取节点。
func GetPoolNode(index int) *PoolNode {
	for _, n := range ListPoolNodes() {
		if n.Index == index {
			v := n
			return &v
		}
	}
	return nil
}

// EnabledPoolNodes 返回启用中的节点。
func EnabledPoolNodes() []PoolNode {
	out := make([]PoolNode, 0, 4)
	for _, n := range ListPoolNodes() {
		if n.Enabled {
			out = append(out, n)
		}
	}
	return out
}

// PoolTokenFilePath 是 sockspool 读取的 token 映射文件。
// 位于容器挂载的 /data 下，宿主机对应 /root/api/shop/data/pool_tokens.json，
// sockspool 定期热加载，因此增删节点不需要重启进程。
const PoolTokenFilePath = "/data/pool_tokens.json"

// PoolTunnelAddr 返回家宽 agent 应该连的隧道地址。
// 优先取系统设置里的显式配置，其次用 HSTORE_PUBLIC_HOST。
func PoolTunnelAddr() string {
	if v := strings.TrimSpace(db.GetMeta(SKPoolTunnel)); v != "" {
		return v
	}
	host := strings.TrimSpace(os.Getenv("HSTORE_PUBLIC_HOST"))
	if host == "" {
		return ""
	}
	// HSTORE_PUBLIC_HOST 形如 1.2.3.4:3001，隧道端口固定 18090
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host + ":18090"
}

// SavePoolTunnelAddr 保存隧道地址。
func SavePoolTunnelAddr(addr string) { _ = db.SetMeta(SKPoolTunnel, strings.TrimSpace(addr)) }

// WritePoolTokens 把「token -> 节点」写成 sockspool 认识的 JSON 文件。
func WritePoolTokens() error {
	m := map[string]map[string]any{}
	for _, n := range ListPoolNodes() {
		if !n.Enabled || strings.TrimSpace(n.Token) == "" {
			continue
		}
		m[n.Token] = map[string]any{"index": n.Index, "label": n.Label}
	}
	raw, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return e
	}
	if err := os.MkdirAll(filepath.Dir(PoolTokenFilePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(PoolTokenFilePath, raw, 0o600)
}
