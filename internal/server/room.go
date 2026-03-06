package server

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) activeBotIDsInNamespace(ctx context.Context) map[string]struct{} {
	if s.kubeClient == nil {
		return nil
	}
	deps, err := s.kubeClient.AppsV1().Deployments(s.cfg.BotNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=aibot",
	})
	if err != nil {
		return nil
	}
	out := make(map[string]struct{}, len(deps.Items))
	for _, d := range deps.Items {
		// Only treat deployments with desired replicas > 0 as active.
		// Historical entries often stay in cluster with replicas=0.
		if d.Spec.Replicas == nil || *d.Spec.Replicas <= 0 {
			continue
		}
		userID := resolveUserIDFromLabels(d.Labels)
		if userID == "" {
			userID = strings.TrimSpace(d.Name)
		}
		if userID != "" {
			out[userID] = struct{}{}
		}
	}
	return out
}
