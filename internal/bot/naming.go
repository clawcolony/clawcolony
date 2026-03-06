package bot

import (
	"fmt"
	"regexp"
	"strings"
)

var invalidK8sChars = regexp.MustCompile(`[^a-z0-9-]`)

func sanitizeName(name string) string {
	out := strings.ToLower(name)
	out = invalidK8sChars.ReplaceAllString(out, "-")
	out = strings.Trim(out, "-")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	if out == "" {
		return "user"
	}
	return out
}

func WorkloadName(botID string) string {
	return sanitizeName(botID)
}

func ProfileConfigMapName(workloadName string) string {
	return sanitizeName(fmt.Sprintf("%s-profile", workloadName))
}

func StatePVCName(workloadName string) string {
	return sanitizeName(fmt.Sprintf("%s-state", workloadName))
}
