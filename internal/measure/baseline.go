package measure

import (
	"context"
	"time"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

// BaselineWindowDays is Part G.5's fixed baseline window: "the 14 days
// immediately preceding applied_at."
const BaselineWindowDays = 14

// FreezeBaseline computes the pre-apply baseline from real requests —
// called once, at apply time (Part G.5 step 4), and never recomputed:
// the resulting numbers are written into the measurements row and stay
// fixed even if the pricing catalog or retention policy later changes
// what a fresh query over the same dates would return (Rule 10 — no
// silent semantic drift).
func FreezeBaseline(ctx context.Context, requests *store.Requests, appID types.AppID, appliedAt time.Time) (start, end time.Time, reqCount, costMicro, costPer1k int64, err error) {
	end = appliedAt
	start = end.AddDate(0, 0, -BaselineWindowDays)
	reqCount, costMicro, err = requests.PeriodStats(ctx, appID, start, end)
	if err != nil {
		return
	}
	costPer1k = CostPer1k(costMicro, reqCount)
	return
}
