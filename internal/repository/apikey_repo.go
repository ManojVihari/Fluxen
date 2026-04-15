package repository

import (
	"context"
	"encoding/json"

	"github.com/ManojVihari/Fluxen/internal/models"
)

func GetAPIKey(keyHash string) (*models.APIKey, error) {
	query := `
	SELECT a.id, a.monthly_budget, a.provider, a.provider_api_key,
	       COALESCE(a.role_id, 0), COALESCE(r.name, 'standard')
	FROM api_keys a
	LEFT JOIN roles r ON r.id = a.role_id
	WHERE a.key_hash=$1
	`

	var apiKey models.APIKey

	err := DB.QueryRow(context.Background(), query, keyHash).Scan(
		&apiKey.ID,
		&apiKey.Budget,
		&apiKey.Provider,
		&apiKey.ProviderAPIKey,
		&apiKey.RoleID,
		&apiKey.RoleName,
	)

	if err != nil {
		return nil, err
	}

	return &apiKey, nil
}

func GetRolePolicy(roleName string) (*models.RolePolicy, error) {
	query := `SELECT policy FROM roles WHERE name = $1`

	var raw []byte
	err := DB.QueryRow(context.Background(), query, roleName).Scan(&raw)
	if err != nil {
		// Default permissive policy if role not found
		return &models.RolePolicy{
			AllowChat:  true,
			AllowUsage: true,
		}, nil
	}

	var p models.RolePolicy
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}