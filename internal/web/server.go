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
		_ = tpl.ExecuteTemplate(w, "index.html", map[string]any{"title": "OSAgent Product MVP"})
	})
	return nil
}
