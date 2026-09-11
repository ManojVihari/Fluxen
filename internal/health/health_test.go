package health

import (
	"context"
	"errors"
	"testing"
)

type fakePinger struct {
	err error
}

func (f fakePinger) Ping(_ context.Context) error { return f.err }

func TestCheck_AllHealthy(t *testing.T) {
	deps := map[string]Pinger{
		"postgres": fakePinger{},
		"redis":    fakePinger{},
	}

	result := Check(context.Background(), deps)

	if !AllOK(result) {
		t.Fatalf("expected all dependencies healthy, got: %+v", result)
	}
	if !result["postgres"].OK || !result["redis"].OK {
		t.Fatalf("expected both dependencies OK, got: %+v", result)
	}
}

func TestCheck_OneUnhealthy(t *testing.T) {
	deps := map[string]Pinger{
		"postgres": fakePinger{},
		"redis":    fakePinger{err: errors.New("connection refused")},
	}

	result := Check(context.Background(), deps)

	if AllOK(result) {
		t.Fatalf("expected AllOK to be false when redis is down, got: %+v", result)
	}
	if !result["postgres"].OK {
		t.Errorf("expected postgres to still report healthy, got: %+v", result["postgres"])
	}
	if result["redis"].OK {
		t.Errorf("expected redis to report unhealthy, got: %+v", result["redis"])
	}
	if result["redis"].Error != "connection refused" {
		t.Errorf("expected redis error message to be preserved, got: %q", result["redis"].Error)
	}
}

func TestAllOK_EmptySetIsTrue(t *testing.T) {
	if !AllOK(map[string]Status{}) {
		t.Fatal("expected AllOK on an empty dependency set to be true")
	}
}
