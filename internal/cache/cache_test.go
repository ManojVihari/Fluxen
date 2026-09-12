package cache

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"fluxen/pkg/types"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based cache test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping: could not start redis testcontainer (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get redis connection string: %v", err)
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("failed to parse redis url: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestStore_SetThenGetRoundTrips(t *testing.T) {
	client := newTestRedis(t)
	s := NewStore(client, nil)
	appID := types.AppID("app-1")
	key := []byte("cache-key-1")

	if _, ok := s.Get(context.Background(), appID, key); ok {
		t.Fatal("expected a miss before anything is stored")
	}

	entry := Entry{Body: []byte(`{"ok":true}`), Model: "gpt-4o-mini", InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CostMicro: 42}
	s.Set(context.Background(), appID, key, entry, time.Hour)

	got, ok := s.Get(context.Background(), appID, key)
	if !ok {
		t.Fatal("expected a hit after storing")
	}
	if string(got.Body) != string(entry.Body) || got.CostMicro != entry.CostMicro {
		t.Errorf("expected the round-tripped entry to match, got %+v", got)
	}
}

func TestStore_DifferentAppsNeverCollide(t *testing.T) {
	client := newTestRedis(t)
	s := NewStore(client, nil)
	key := []byte("same-key")

	s.Set(context.Background(), "app-1", key, Entry{Body: []byte("a")}, time.Hour)
	if _, ok := s.Get(context.Background(), "app-2", key); ok {
		t.Fatal("expected app-2 to never see app-1's cache entry")
	}
}

func TestStore_EmptyCacheKeyNeverHits(t *testing.T) {
	client := newTestRedis(t)
	s := NewStore(client, nil)

	s.Set(context.Background(), "app-1", nil, Entry{Body: []byte("a")}, time.Hour)
	if _, ok := s.Get(context.Background(), "app-1", nil); ok {
		t.Fatal("expected an empty cache key to never hit")
	}
}

func TestStore_ExpiresAfterTTL(t *testing.T) {
	client := newTestRedis(t)
	s := NewStore(client, nil)
	key := []byte("short-lived")

	s.Set(context.Background(), "app-1", key, Entry{Body: []byte("a")}, 500*time.Millisecond)
	if _, ok := s.Get(context.Background(), "app-1", key); !ok {
		t.Fatal("expected a hit immediately after storing")
	}

	time.Sleep(700 * time.Millisecond)
	if _, ok := s.Get(context.Background(), "app-1", key); ok {
		t.Fatal("expected a miss after the TTL has elapsed")
	}
}

func TestStore_StreamedChunksRoundTrip(t *testing.T) {
	client := newTestRedis(t)
	s := NewStore(client, nil)
	key := []byte("streamed-key")

	entry := Entry{Streamed: true, Chunks: [][]byte{[]byte("data: chunk1\n\n"), []byte("data: chunk2\n\n"), []byte("data: [DONE]\n\n")}}
	s.Set(context.Background(), "app-1", key, entry, time.Hour)

	got, ok := s.Get(context.Background(), "app-1", key)
	if !ok {
		t.Fatal("expected a hit")
	}
	if len(got.Chunks) != 3 || string(got.Chunks[1]) != "data: chunk2\n\n" {
		t.Errorf("expected streamed chunks to round-trip in order, got %+v", got.Chunks)
	}
}
