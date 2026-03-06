package main

import (
	"context"
	"log"

	"clawcolony/internal/bot"
	"clawcolony/internal/config"
	"clawcolony/internal/server"
	"clawcolony/internal/store"
)

func main() {
	cfg := config.FromEnv()
	cfg.ServiceRole = config.ServiceRoleRuntime
	ctx := context.Background()

	var st store.Store
	var err error
	if cfg.DatabaseURL == "" {
		log.Printf("DATABASE_URL is empty, fallback to in-memory store")
		st = store.NewInMemory()
	} else {
		st, err = store.NewPostgres(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to init postgres store: %v", err)
		}
	}
	defer st.Close()

	// Runtime project does not embed privileged deployer implementation.
	botManager := bot.NewManager(st, bot.NewNoopDeployer(), cfg.ClawWorldAPIBase, cfg.BotModel)
	srv := server.New(cfg, st, botManager)

	log.Printf("clawcolony-runtime starting on %s (service_role=%s)", cfg.ListenAddr, cfg.EffectiveServiceRole())
	if err := srv.Start(); err != nil {
		log.Fatalf("clawcolony-runtime stopped: %v", err)
	}
}
