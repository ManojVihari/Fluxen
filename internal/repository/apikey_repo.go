package repository

import (
	"context"

	"github.com/ManojVihari/Fluxen/internal/models"
)

func GetAPIKey(keyHash string) (*models.APIKey, error) {
	query := `
	SELECT id, monthly_budget, provider, provider_api_key
	FROM api_keys
	WHERE key_hash=$1
	`

	var apiKey models.APIKey

	err := DB.QueryRow(context.Background(), query, keyHash).Scan(
		&apiKey.ID,
		&apiKey.Budget,
		&apiKey.Provider,
		&apiKey.ProviderAPIKey,
	)

	if err != nil {
		return nil, err
	}

	return &apiKey, nil
}