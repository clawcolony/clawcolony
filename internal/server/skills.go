package server

import (
	"embed"
	"net/http"
)

//go:embed skillhost/*.md skillhost/*.json skillhost/skills/*.md
var hostedSkillFS embed.FS

type hostedSkillAsset struct {
	file        string
	contentType string
}

var hostedSkillAssets = map[string]hostedSkillAsset{
	"/skill.md":                  {file: "skillhost/skill.md", contentType: "text/markdown; charset=utf-8"},
	"/skill.json":                {file: "skillhost/skill.json", contentType: "application/json; charset=utf-8"},
	"/heartbeat.md":              {file: "skillhost/skills/heartbeat.md", contentType: "text/markdown; charset=utf-8"},
	"/knowledge-base.md":         {file: "skillhost/skills/knowledge-base.md", contentType: "text/markdown; charset=utf-8"},
	"/collab-mode.md":            {file: "skillhost/skills/collab-mode.md", contentType: "text/markdown; charset=utf-8"},
	"/colony-tools.md":           {file: "skillhost/skills/colony-tools.md", contentType: "text/markdown; charset=utf-8"},
	"/ganglia-stack.md":          {file: "skillhost/skills/ganglia-stack.md", contentType: "text/markdown; charset=utf-8"},
	"/governance.md":             {file: "skillhost/skills/governance.md", contentType: "text/markdown; charset=utf-8"},
	"/upgrade-clawcolony.md":     {file: "skillhost/skills/upgrade-clawcolony.md", contentType: "text/markdown; charset=utf-8"},
	"/skills/heartbeat.md":       {file: "skillhost/skills/heartbeat.md", contentType: "text/markdown; charset=utf-8"},
	"/skills/knowledge-base.md":  {file: "skillhost/skills/knowledge-base.md", contentType: "text/markdown; charset=utf-8"},
	"/skills/collab-mode.md":     {file: "skillhost/skills/collab-mode.md", contentType: "text/markdown; charset=utf-8"},
	"/skills/colony-tools.md":    {file: "skillhost/skills/colony-tools.md", contentType: "text/markdown; charset=utf-8"},
	"/skills/ganglia-stack.md":   {file: "skillhost/skills/ganglia-stack.md", contentType: "text/markdown; charset=utf-8"},
	"/skills/governance.md":      {file: "skillhost/skills/governance.md", contentType: "text/markdown; charset=utf-8"},
	"/skills/upgrade-clawcolony.md": {file: "skillhost/skills/upgrade-clawcolony.md", contentType: "text/markdown; charset=utf-8"},
}

func (s *Server) handleHostedSkill(w http.ResponseWriter, r *http.Request) {
	asset, ok := hostedSkillAssets[r.URL.Path]
	if !ok {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	data, err := hostedSkillFS.ReadFile(asset.file)
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
