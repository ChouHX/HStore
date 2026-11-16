// Command poolctl 是家宽节点配置的运维小工具。
//
// 后台也能增删节点，但服务器上没有管理员会话时用它可以快速建一个：
//
//	poolctl add "成都电信"   # 新增节点并打印一键接入命令
//	poolctl list             # 列出节点与 token 摘要
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

func main() {
	if _, e := db.Open(); e != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", e)
		os.Exit(1)
	}
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("用法: poolctl add <备注> | list | rm <序号>")
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		label := ""
		if len(args) > 1 {
			label = strings.Join(args[1:], " ")
		}
		node, e := store.AddPoolNode(label)
		if e != nil {
			fmt.Fprintf(os.Stderr, "新增失败: %v\n", e)
			os.Exit(1)
		}
		host := os.Getenv("HSTORE_PUBLIC_HOST")
		if host == "" {
			host = "43.166.8.180:3001"
		}
		fmt.Printf("节点 %d（%s）已创建\n", node.Index, node.Label)
		fmt.Printf("接入命令：\n%s\n", upstream.BuildOnboardCommand(host, node))
	case "list":
		list := store.ListPoolNodes()
		if len(list) == 0 {
			fmt.Println("（无节点）")
			return
		}
		for _, n := range list {
			fmt.Printf("节点 %d  label=%q enabled=%v token=%s...\n",
				n.Index, n.Label, n.Enabled, n.Token[:12])
		}
	case "rm":
		if len(args) < 2 {
			fmt.Println("用法: poolctl rm <序号>")
			os.Exit(2)
		}
		var idx int
		fmt.Sscanf(args[1], "%d", &idx)
		if e := store.DeletePoolNode(idx); e != nil {
			fmt.Fprintf(os.Stderr, "删除失败: %v\n", e)
			os.Exit(1)
		}
		fmt.Println("已删除")
	default:
		flag.Usage()
		os.Exit(2)
	}
}
