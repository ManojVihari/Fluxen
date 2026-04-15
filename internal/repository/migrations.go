package repository

import (
	"context"
	"log"
)

func CreateTables() {

	rolesTable := `
	CREATE TABLE IF NOT EXISTS roles (
		id SERIAL PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		policy JSONB NOT NULL DEFAULT '{}'
	);
	`

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
		role_id INT REFERENCES roles(id) DEFAULT NULL,
		created_at TIMESTAMP
	);
	`

	_, err := DB.Exec(context.Background(), rolesTable)
	if err != nil {
		log.Fatalf("Failed creating roles: %v", err)
	}

	// Seed default roles
	seedRoles()

	_, err = DB.Exec(context.Background(), usageTable)
	if err != nil {
		log.Fatalf("Failed creating usage_logs: %v", err)
	}

	_, err = DB.Exec(context.Background(), apiKeyTable)
	if err != nil {
		log.Fatalf("Failed creating api_keys: %v", err)
	}

	// Add role_id column if missing (existing installs)
	addRoleColumn := `
	ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS role_id INT REFERENCES roles(id) DEFAULT NULL;
	`
	DB.Exec(context.Background(), addRoleColumn)
}

func seedRoles() {
	seeds := []struct {
		name   string
		policy string
	}{
		{
			name: "admin",
			policy: `{
				"allowed_models": [],
				"max_tokens_per_req": 0,
				"rate_limit": 0,
				"allow_chat": true,
				"allow_usage": true
			}`,
		},
		{
			name: "standard",
			policy: `{
				"allowed_models": ["gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo", "llama3", "mistral"],
				"max_tokens_per_req": 4096,
				"rate_limit": 60,
				"allow_chat": true,
				"allow_usage": true
			}`,
		},
		{
			name: "restricted",
			policy: `{
				"allowed_models": ["gpt-4o-mini", "gpt-3.5-turbo"],
				"max_tokens_per_req": 1024,
				"rate_limit": 10,
				"allow_chat": true,
				"allow_usage": false
			}`,
		},
	}

	for _, s := range seeds {
		_, err := DB.Exec(context.Background(),
			`INSERT INTO roles (name, policy) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING`,
			s.name, s.policy,
		)
		if err != nil {
			log.Printf("Failed seeding role %s: %v", s.name, err)
		}
	}
}