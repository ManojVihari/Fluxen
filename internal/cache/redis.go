package cache

import (
	"context"
	"log"
	"os"

	"github.com/redis/go-redis/v9"
)

var Client *redis.Client

func InitRedis() {
	addr := os.Getenv("REDIS_URL")
	if addr == "" {
		addr = "localhost:6379"
	}

	Client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "", // no password set
		DB:       0,
	})

	if err := Client.Ping(context.Background()).Err(); err != nil {
		log.Printf("Warning: Redis not available (%v) — semantic cache disabled", err)
		Client = nil
	} else {
		log.Println("Redis connected — semantic cache enabled")
	}
}
