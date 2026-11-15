// Package webassets 通过 go:embed 内嵌前端资源。
package webassets

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js style.css
var files embed.FS

// Handler 返回前端静态资源处理器。资源随二进制发布，无需额外目录。
func Handler() http.Handler {
	sub, err := fs.Sub(files, ".")
	if err != nil {
		panic("webassets: 无法读取内嵌资源: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fileServer.ServeHTTP(w, r)
	})
}
