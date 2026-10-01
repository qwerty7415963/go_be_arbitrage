package trader

import (
	"errors"
	"testing"
	"time"
)

// OBS-U-01: health counters track runs, outcomes and rate-limit classes.
func TestObserve_Counters(t *testing.T) {
	if !isRateLimit(errors.New("upstream 429 busy")) ||
		!isRateLimit(errors.New("Rate Limit exceeded")) ||
		isRateLimit(errors.New("connection reset")) ||
		isRateLimit(nil) {
		t.Error("rate-limit classification")
	}

	svc := &SyncService{}
	svc.recordWallet(1500*time.Millisecond, nil)
	svc.recordWallet(0, errors.New("upstream 429"))
	st := svc.Stats()
	if st.WalletsDone != 1 || st.WalletsFailed != 1 || st.FetchErrors != 1 {
		t.Errorf("wallet counters: %+v", st)
	}
	if st.RateLimitErrors != 1 {
		t.Errorf("rate-limit counter: %+v", st)
	}
	if st.UpstreamLatencyMs != 1500 {
		t.Errorf("latency: %+v", st)
	}
	if st.ConsecutiveErrors != 1 || st.LastError == "" {
		t.Errorf("error streak: %+v", st)
	}
	svc.recordWallet(0, nil)
	if st := svc.Stats(); st.ConsecutiveErrors != 0 {
		t.Errorf("success must reset streak: %+v", st)
	}
	svc.recordRun(time.Now().UTC().Add(-time.Second))
	if st := svc.Stats(); st.Runs != 1 || st.LastDuration <= 0 {
		t.Errorf("run counters: %+v", st)
	}

	d := &DiscoveryService{logf: func(string, ...any) {}}
	d.recordRun(time.Now().UTC(), &DiscoveryResult{Fetched: 5, Inserted: 2, Updated: 2, Skipped: 1}, nil)
	dst := d.Stats()
	if dst.Runs != 1 || dst.Fetched != 5 || dst.Inserted != 2 || dst.Updated != 2 || dst.Skipped != 1 {
		t.Errorf("discovery counters: %+v", dst)
	}
	d.recordRun(time.Now().UTC(), nil, errors.New("boom"))
	if dst := d.Stats(); dst.ConsecutiveErrors != 1 || dst.LastError == "" {
		t.Errorf("discovery errors: %+v", dst)
	}
}
