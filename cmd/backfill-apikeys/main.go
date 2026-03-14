// backfill-apikeys generates api_keys for existing agent_registrations that
// lack one and prints the plaintext keys to stdout (one per line).
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/backfill-apikeys
//	DATABASE_URL=postgres://... go run ./cmd/backfill-apikeys --dry-run
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"clawcolony/internal/store"
)

const apiKeyPrefix = "clawcolony-"

func main() {
	dryRun := false
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" || arg == "-n" {
			dryRun = true
		}
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	st, err := store.NewPostgres(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer st.Close()

	regs, err := st.ListAgentRegistrationsWithoutAPIKey(ctx)
	if err != nil {
		log.Fatalf("failed to list registrations without api_key: %v", err)
	}
	if len(regs) == 0 {
		log.Println("all registrations already have an api_key, nothing to do")
		return
	}
	log.Printf("found %d registration(s) without api_key", len(regs))

	type result struct {
		UserID string `json:"user_id"`
		APIKey string `json:"api_key"`
	}
	results := make([]result, 0, len(regs))

	for _, reg := range regs {
		apiKey, err := randomPrefixedSecret(apiKeyPrefix, 12)
		if err != nil {
			log.Fatalf("failed to generate api_key for %s: %v", reg.UserID, err)
		}
		if dryRun {
			log.Printf("[dry-run] would set api_key for user_id=%s", reg.UserID)
			results = append(results, result{UserID: reg.UserID, APIKey: apiKey})
			continue
		}
		_, err = st.UpdateAgentRegistrationAPIKeyHash(ctx, reg.UserID, hashSecret(apiKey))
		if err != nil {
			log.Fatalf("failed to update api_key_hash for %s: %v", reg.UserID, err)
		}
		log.Printf("set api_key for user_id=%s", reg.UserID)
		results = append(results, result{UserID: reg.UserID, APIKey: apiKey})
	}

	fmt.Fprintln(os.Stderr, "--- generated api_keys (save these, they will not be shown again) ---")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(results)
}

func randomPrefixedSecret(prefix string, byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}

func hashSecret(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}
