package serve

import (
	"context"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	devserver "den-services/devserver-broker"
)

type SessionLister interface {
	List(context.Context) ([]devserver.SessionState, error)
}

type StatusPage struct {
	sessions SessionLister
	template *template.Template
	clock    func() time.Time
}

type StatusPageProject struct {
	Project string
	Port    int
	URL     string
}

type StatusPageData struct {
	Projects  []StatusPageProject
	Refreshed string
}

func NewStatusPage(sessions SessionLister) (*StatusPage, error) {
	if sessions == nil {
		return nil, fmt.Errorf("status page session lister is required")
	}
	pageTemplate, err := template.New("status-page").Parse(statusPageHTML)
	if err != nil {
		return nil, fmt.Errorf("parsing status page template: %w", err)
	}
	return &StatusPage{
		sessions: sessions,
		template: pageTemplate,
		clock:    time.Now,
	}, nil
}

func (p *StatusPage) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessions, err := p.sessions.List(request.Context())
	if err != nil {
		http.Error(response, "could not read den-serve sessions", http.StatusInternalServerError)
		return
	}
	projects := reachableProjects(sessions, request.Host)
	data := StatusPageData{
		Projects:  projects,
		Refreshed: p.clock().UTC().Format(time.RFC3339),
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if err := p.template.Execute(response, data); err != nil {
		return
	}
}

func reachableProjects(sessions []devserver.SessionState, requestHost string) []StatusPageProject {
	projects := make([]StatusPageProject, 0, len(sessions))
	for _, session := range sessions {
		if !session.Health.Matched {
			continue
		}
		projects = append(projects, StatusPageProject{
			Project: session.Project,
			Port:    session.Port,
			URL:     statusProjectURL(session, requestHost),
		})
	}
	sort.Slice(projects, func(left int, right int) bool {
		if projects[left].Project == projects[right].Project {
			return projects[left].Port < projects[right].Port
		}
		return projects[left].Project < projects[right].Project
	})
	return projects
}

func statusProjectURL(session devserver.SessionState, requestHost string) string {
	if isUsableLANURL(session.LANURL) {
		return session.LANURL
	}
	host := hostname(requestHost)
	if host == "" {
		host = strings.TrimSpace(session.PublicHost)
	}
	if host == "" || session.Port < 1 || session.Port > 65535 {
		return session.LocalURL
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, strconv.Itoa(session.Port)),
		Path:   "/",
	}).String()
}

func isUsableLANURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsUnspecified() || ip.IsLoopback()) {
		return false
	}
	return true
}

func hostname(hostPort string) string {
	hostPort = strings.TrimSpace(hostPort)
	if hostPort == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(hostPort)
	if err == nil {
		return host
	}
	return strings.Trim(hostPort, "[]")
}

const statusPageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>den-serve projects</title>
  <style>
    :root { color-scheme: light dark; font-family: ui-sans-serif, system-ui, sans-serif; }
    body { max-width: 52rem; margin: 3rem auto; padding: 0 1rem; }
    header { display: flex; align-items: baseline; justify-content: space-between; gap: 1rem; }
    table { width: 100%; border-collapse: collapse; margin-top: 1.5rem; }
    th, td { padding: .75rem; border-bottom: 1px solid color-mix(in srgb, currentColor 20%, transparent); text-align: left; }
    th:last-child, td:last-child { text-align: right; }
    a { color: LinkText; }
    .empty { padding: 2rem .75rem; text-align: center !important; opacity: .7; }
    footer { margin-top: 1.5rem; opacity: .65; font-size: .85rem; }
  </style>
</head>
<body>
  <header><h1>Running den-serve projects</h1><a href="/">Refresh</a></header>
  <table>
    <thead><tr><th>Project</th><th>Port</th></tr></thead>
    <tbody>
      {{range .Projects}}<tr><td><a href="{{.URL}}">{{.Project}}</a></td><td>{{.Port}}</td></tr>
      {{else}}<tr><td class="empty" colspan="2">No running projects</td></tr>{{end}}
    </tbody>
  </table>
  <footer>Refreshed {{.Refreshed}}</footer>
</body>
</html>`
