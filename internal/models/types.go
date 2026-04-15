package models

// This file contains all the data models used across the application

// Models for OpenAI API
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type APIKey struct {
	ID             int
	Budget         float64
	Provider       string
	ProviderAPIKey string
	RoleID         int
	RoleName       string
}

type Role struct {
	ID     int        `json:"id"`
	Name   string     `json:"name"`
	Policy RolePolicy `json:"policy"`
}

type RolePolicy struct {
	AllowedModels  []string `json:"allowed_models"`  // empty = all allowed
	MaxTokensPerReq int     `json:"max_tokens_per_req"` // 0 = unlimited
	RateLimit       int     `json:"rate_limit"`         // requests per minute, 0 = unlimited
	AllowChat       bool    `json:"allow_chat"`
	AllowUsage      bool    `json:"allow_usage"`
}

type UsageSummary struct {
	TotalRequests int     `json:"total_requests"`
	TotalTokens   int     `json:"total_tokens"`
	TotalCost     float64 `json:"total_cost"`
}