package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/* static/*
var assets embed.FS

func Register(mux *http.ServeMux) error {
	templateFS, err := fs.Sub(assets, "templates")
	if err != nil {
		return err
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return err
	}
	tpl, err := template.ParseFS(templateFS, "*.html")
	if err != nil {
		return err
	}

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_ = tpl.ExecuteTemplate(w, "chat.html", map[string]any{"title": "运维控制台 - 对话"})
		case "/assets":
			_ = tpl.ExecuteTemplate(w, "assets.html", map[string]any{"title": "资产管理 - 运维控制台"})
		case "/automation":
			_ = tpl.ExecuteTemplate(w, "automation.html", map[string]any{"title": "自动化工作流 - 运维控制台"})
		case "/settings":
			_ = tpl.ExecuteTemplate(w, "settings.html", map[string]any{"title": "系统设置 - 运维控制台"})
		default:
			http.NotFound(w, r)
		}
	})
	return nil
}
