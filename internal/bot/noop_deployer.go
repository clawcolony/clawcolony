package bot

import (
	"context"

	"clawcolony/internal/store"
)

type NoopDeployer struct{}

func NewNoopDeployer() NoopDeployer {
	return NoopDeployer{}
}

func (NoopDeployer) Deploy(_ context.Context, _ store.Bot, _ DeploySpec, _ RuntimeProfile) error {
	return nil
}
