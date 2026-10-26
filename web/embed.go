// Package web 保存管理后台静态资源。
package web

import "embed"

// Files 包含构建产物；placeholder.html 保证未构建前端时也能编译 Go。
//
//go:embed all:dist
var Files embed.FS
