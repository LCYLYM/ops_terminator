package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/* static/*
var assets embed.FS

type ActionView struct {
	ID     string
	Href   string
	Label  string
	Icon   string
	IsLink bool
}

type NavItem struct {
	Href   string
	Label  string
	Icon   string
	Active bool
}

type PageView struct {
	PageKey       string
	BodyClass     string
	Title         string
	Subtitle      string
	NavItems      []NavItem
	PrimaryAction *ActionView
	SidebarAction *ActionView
}

type pageRenderer struct {
	view PageView
	tpl  *template.Template
}

type server struct {
	pages map[string]pageRenderer
}

func Register(mux *http.ServeMux) error {
	templateFS, err := fs.Sub(assets, "templates")
	if err != nil {
		return err
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return err
	}
	pages, err := buildPageRenderers(templateFS)
	if err != nil {
		return err
	}
	s := &server{pages: pages}

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/", s.handlePage)
	return nil
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	page, ok := s.pages[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}

	view := page.view
	view.NavItems = buildNavItems(view.PageKey)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.tpl.ExecuteTemplate(w, "layout", view); err != nil {
		http.Error(w, fmt.Sprintf("render page: %v", err), http.StatusInternalServerError)
	}
}

func buildPageRenderers(templateFS fs.FS) (map[string]pageRenderer, error) {
	base, err := template.ParseFS(templateFS, "layout.html", "shared.html")
	if err != nil {
		return nil, fmt.Errorf("parse base templates: %w", err)
	}

	definitions := map[string]struct {
		file string
		view PageView
	}{
		"/": {
			file: "chat.html",
			view: PageView{
				PageKey:   "chat",
				BodyClass: "app-page--chat",
				Title:     "对话控制面",
				Subtitle:  "真实会话、执行、审批与事件统一视图。",
				SidebarAction: &ActionView{
					ID:    "new-session-button",
					Label: "新建会话",
					Icon:  "add",
				},
			},
		},
		"/assets": {
			file: "assets.html",
			view: PageView{
				PageKey:   "assets",
				BodyClass: "app-page--assets",
				Title:     "资产管理",
				Subtitle:  "查看真实主机资产，并直接完成接入。",
				PrimaryAction: &ActionView{
					ID:    "assets-open-form",
					Label: "新增主机",
					Icon:  "add",
				},
			},
		},
		"/automation": {
			file: "automation.html",
			view: PageView{
				PageKey:   "automation",
				BodyClass: "app-page--automation",
				Title:     "自动化执行记录",
				Subtitle:  "基于真实 runs 的状态统计与执行回放。",
				PrimaryAction: &ActionView{
					Href:   "/",
					Label:  "新建执行",
					Icon:   "play_arrow",
					IsLink: true,
				},
			},
		},
		"/settings": {
			file: "settings.html",
			view: PageView{
				PageKey:   "settings",
				BodyClass: "app-page--settings",
				Title:     "系统设置",
				Subtitle:  "管理网关预设、能力边界与实时运行配置。",
			},
		},
	}

	pages := make(map[string]pageRenderer, len(definitions))
	for route, definition := range definitions {
		cloned, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone base templates for %s: %w", route, err)
		}
		pageTpl, err := cloned.ParseFS(templateFS, definition.file)
		if err != nil {
			return nil, fmt.Errorf("parse page template %s: %w", definition.file, err)
		}
		pages[route] = pageRenderer{
			view: definition.view,
			tpl:  pageTpl,
		}
	}
	return pages, nil
}

func buildNavItems(active string) []NavItem {
	items := []NavItem{
		{Href: "/", Label: "对话历史", Icon: "chat"},
		{Href: "/assets", Label: "资产管理", Icon: "dns"},
		{Href: "/automation", Label: "自动化中心", Icon: "bolt"},
		{Href: "/settings", Label: "系统设置", Icon: "settings"},
	}
	for i := range items {
		switch items[i].Href {
		case "/":
			items[i].Active = active == "chat"
		case "/assets":
			items[i].Active = active == "assets"
		case "/automation":
			items[i].Active = active == "automation"
		case "/settings":
			items[i].Active = active == "settings"
		}
	}
	return items
}
