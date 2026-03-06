package server

import "strings"

func resolveUserIDFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	if v := strings.TrimSpace(labels["clawcolony.user_id"]); v != "" {
		return v
	}
	return ""
}
