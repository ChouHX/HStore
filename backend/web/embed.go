// Package web 内嵌前端构建产物（vite build 输出到本目录下的 dist/）。
//
// 构建：npm run build  →  backend/web/dist/{index.html,assets/*}
// 运行：Go 服务在 /api/* 之外提供静态文件，未命中的路径回退 index.html（SPA）。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回去掉 dist 前缀的静态文件系统；不可用时返回 nil。
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	return sub
}
