package main

import (
	"github.com/joho/godotenv"
	"log"
	"net/http"

	"github.com/ManojVihari/Fluxen/internal/cache"
	"github.com/ManojVihari/Fluxen/internal/gateway"
	"github.com/ManojVihari/Fluxen/internal/repository"
)

func main() {
	godotenv.Load()
	repository.InitDB()
	repository.CreateTables()
	cache.InitRedis()

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Fluxen is running 🚀"))
	})

	// Chat completion endpoint
	http.HandleFunc("/v1/chat/completions", gateway.ChatHandler)

	// Usage endpoint
	http.HandleFunc("/v1/usage", gateway.UsageHandler)

	// Cache stats endpoint
	http.HandleFunc("/v1/cache/stats", gateway.CacheStatsHandler)

	log.Println("Fluxen running on :8080 🚀")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
