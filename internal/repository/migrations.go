package repository

import (
	"context"
	"log"
)

func CreateTables() {

	usageTable := `
	CREATE TABLE IF NOT EXISTS usage_logs (
		id SERIAL PRIMARY KEY,
		api_key_id INT,
		request_id TEXT,
		model TEXT,
		prompt_tokens INT,
		completion_tokens INT,
		total_tokens INT,
		cost FLOAT,
		created_at TIMESTAMP
	);
	`

	apiKeyTable := `
	CREATE TABLE IF NOT EXISTS api_keys (
		id SERIAL PRIMARY KEY,
		key_hash TEXT UNIQUE,
		name TEXT,
		monthly_budget FLOAT,
		provider TEXT,
		provider_api_key TEXT,
		created_at TIMESTAMP
	);
	`

	_, err := DB.Exec(context.Background(), usageTable)
	if err != nil {
		log.Fatalf("Failed creating usage_logs: %v", err)
	}

	_, err = DB.Exec(context.Background(), apiKeyTable)
	if err != nil {
		log.Fatalf("Failed creating api_keys: %v", err)
	}
}