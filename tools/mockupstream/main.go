// mockupstream is a throwaway OpenAI-compatible stub used only for local
// load testing (see docs/self-hosting.md's load-testing note) — never
// built into the production image. It returns a fixed, valid
// chat-completion response instantly, so a load test measures Fluxen's
// own overhead (auth, policy, accounting, DB writes) rather than a real
// provider's latency.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-mock",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello from the mock upstream.",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     20,
				"completion_tokens": 8,
				"total_tokens":      28,
			},
		})
	})

	addr := ":3099"
	log.Printf("mockupstream: listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
