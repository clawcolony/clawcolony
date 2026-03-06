package server

import (
	"context"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) activeBotIDsInNamespace(ctx context.Context) (map[string]struct{}, bool) {
	if s.kubeClient == nil {
		return nil, false
	}
	deps, err := s.kubeClient.AppsV1().Deployments(s.cfg.BotNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=aibot",
	})
	if err != nil {
		log.Printf("active_bot_ids_list_error namespace=%s err=%v", s.cfg.BotNamespace, err)
		return nil, false
	}
	out := make(map[string]struct{}, len(deps.Items))
	for _, d := range deps.Items {
		// Only treat deployments with desired replicas > 0 as active.
		// Historical entries often stay in cluster with replicas=0.
		if d.Spec.Replicas == nil || *d.Spec.Replicas <= 0 {
			continue
		}
		userID := resolveUserIDFromWorkload(d.Name, d.Labels)
		if userID != "" {
			out[userID] = struct{}{}
		}
	}
	return out, true
}
