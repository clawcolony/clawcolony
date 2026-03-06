package server

import (
	"context"
	"fmt"

	"clawcolony/internal/store"
)

// openClawAdminActionRequest is kept as a minimal local type so shared
// world-tick code can compile in runtime-only project.
type openClawAdminActionRequest struct {
	Action   string `json:"action"`
	UserID   string `json:"user_id"`
	Provider string `json:"provider"`
	Image    string `json:"image"`
}

func (s *Server) startRegisterTask(_ context.Context, _ openClawAdminActionRequest) (store.RegisterTask, error) {
	return store.RegisterTask{}, fmt.Errorf("register task is deployer-only in runtime project")
}
