package server

import (
	"embed"
	"net/http"
	"path"
	"strings"
)

//go:embed web/*.html
var dashboardFS embed.FS

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	cleanPath := strings.Trim(path.Clean(r.URL.Path), "/")
	page := "dashboard_home.html"

	switch cleanPath {
	case "dashboard":
		page = "dashboard_home.html"
	case "dashboard/mail":
		page = "dashboard_mail.html"
	case "dashboard/system-logs":
		page = "dashboard_system_logs.html"
	case "dashboard/collab":
		page = "dashboard_collab.html"
	case "dashboard/kb":
		page = "dashboard_kb.html"
	case "dashboard/world-tick":
		page = "dashboard_world_tick.html"
	case "dashboard/world-replay":
		page = "dashboard_world_replay.html"
	case "dashboard/ops":
		page = "dashboard_ops.html"
	case "dashboard/monitor":
		page = "dashboard_monitor.html"
	case "dashboard/governance":
		page = "dashboard_governance.html"
	case "dashboard/ganglia":
		page = "dashboard_ganglia.html"
	case "dashboard/bounty":
		page = "dashboard_bounty.html"
	default:
		writeError(w, http.StatusNotFound, "dashboard page not found")
		return
	}

	data, err := dashboardFS.ReadFile("web/" + page)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
