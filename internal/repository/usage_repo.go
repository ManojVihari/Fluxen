package repository

import (
	"context"
	"log"
	"time"

	"github.com/ManojVihari/Fluxen/internal/models"
)

func InsertUsage(
	apiKeyID int,
	requestID string,
	model string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	cost float64,
) {
	query := `
	INSERT INTO usage_logs
	(api_key_id, request_id, model, prompt_tokens, completion_tokens, total_tokens, cost, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := DB.Exec(
		context.Background(),
		query,
		apiKeyID,
		requestID,
		model,
		promptTokens,
		completionTokens,
		totalTokens,
		cost,
		time.Now(),
	)

	if err != nil {
		log.Println("Failed inserting usage:", err)
	}
}

func GetMonthlyUsage(apiKeyID int) (float64, error) {
	query := `
	SELECT COALESCE(SUM(cost),0)
	FROM usage_logs
	WHERE api_key_id = $1
	AND created_at >= date_trunc('month', CURRENT_DATE)
	`

	var total float64
	err := DB.QueryRow(context.Background(), query, apiKeyID).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}

func GetUsageSummary(apiKeyID int) (*models.UsageSummary, error) {
	query := `
	SELECT 
		COUNT(*) as total_requests,
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost), 0)
	FROM usage_logs
	WHERE api_key_id = $1
	`

	var summary models.UsageSummary

	err := DB.QueryRow(context.Background(), query, apiKeyID).Scan(
		&summary.TotalRequests,
		&summary.TotalTokens,
		&summary.TotalCost,
	)

	if err != nil {
		return nil, err
	}

	return &summary, nil
}