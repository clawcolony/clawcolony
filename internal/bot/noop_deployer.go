package bot

import (
	"context"

	"clawcolony/internal/store"
)

type NoopProvisioner struct{}

func NewNoopProvisioner() NoopProvisioner {
	return NoopProvisioner{}
}

func (NoopProvisioner) Deploy(_ context.Context, _ store.Bot, _ DeploySpec, _ RuntimeProfile) error {
	return nil
}
