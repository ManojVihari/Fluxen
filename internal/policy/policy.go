package policy

import (
	"fmt"

	"github.com/ManojVihari/Fluxen/internal/models"
)

// CheckModelAccess verifies the requested model is allowed by the role policy.
func CheckModelAccess(p models.RolePolicy, model string) error {
	if len(p.AllowedModels) == 0 {
		return nil // empty = all models allowed
	}
	for _, m := range p.AllowedModels {
		if m == model {
			return nil
		}
	}
	return fmt.Errorf("❌ model %q not allowed for this role. Allowed models: %v", model, p.AllowedModels)
}

// CheckChatPermission verifies the role can use the chat endpoint.
func CheckChatPermission(p models.RolePolicy) error {
	if !p.AllowChat {
		return fmt.Errorf("❌ chat access denied for this role")
	}
	return nil
}

// CheckUsagePermission verifies the role can view usage data.
func CheckUsagePermission(p models.RolePolicy) error {
	if !p.AllowUsage {
		return fmt.Errorf("❌ usage access denied for this role")
	}
	return nil
}

// CheckTokenLimit verifies the prompt token count is within role limits.
// Returns nil if no limit is set (0 = unlimited).
func CheckTokenLimit(p models.RolePolicy, promptTokens int) error {
	if p.MaxTokensPerReq > 0 && promptTokens > p.MaxTokensPerReq {
		return fmt.Errorf("❌ prompt exceeds max tokens per request (%d > %d)", promptTokens, p.MaxTokensPerReq)
	}
	return nil
}
