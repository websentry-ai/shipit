package api

import (
	"net/url"
	"testing"
	"time"
)

// parseTimeRange has three input shapes (unix, RFC3339, missing) plus
// validation rules — small but easy to break. Use times near now() so the
// "range > 60d" guard doesn't fire when only one bound is supplied.
func TestParseTimeRange(t *testing.T) {
	now := time.Now()
	hourAgo := strFromUnix(now.Add(-time.Hour).Unix())
	twoHoursAgo := strFromUnix(now.Add(-2 * time.Hour).Unix())
	soon := strFromUnix(now.Add(time.Minute).Unix())

	tests := []struct {
		name    string
		q       url.Values
		wantErr bool
	}{
		{"both missing → defaults to last hour", url.Values{}, false},
		{"unix-seconds both, near now", url.Values{"from": {twoHoursAgo}, "to": {hourAgo}}, false},
		{"rfc3339 both", url.Values{"from": {"2026-04-01T00:00:00Z"}, "to": {"2026-04-02T00:00:00Z"}}, false},
		{"only from (recent), to defaults to now", url.Values{"from": {hourAgo}}, false},
		{"only to (slightly future), from defaults to now-1h", url.Values{"to": {soon}}, false},
		{"from after to is rejected", url.Values{"from": {hourAgo}, "to": {twoHoursAgo}}, true},
		{"garbage rfc", url.Values{"from": {"not-a-time"}}, true},
		{"range > 60d", url.Values{"from": {"1"}, "to": {"99999999999"}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseTimeRange(tc.q)
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func strFromUnix(n int64) string {
	return time.Unix(n, 0).UTC().Format(time.RFC3339)
}

// pickStep is critical: too-small steps overload Prometheus, too-large make
// the chart blocky. Verify both clamps and the override path.
func TestPickStep(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []struct {
		name string
		dur  time.Duration
		over string
		want time.Duration
	}{
		{"1h range → 30s step (~120 points)", time.Hour, "", 30 * time.Second},
		{"1m range clamps to 15s floor", time.Minute, "", 15 * time.Second},
		{"30d range clamps to 30m ceiling", 30 * 24 * time.Hour, "", 30 * time.Minute},
		{"explicit override honored", time.Hour, "60", 60 * time.Second},
		{"override that would yield > 1000 points is downscaled", 24 * time.Hour, "10", (24 * time.Hour) / 1000},
		{"bad override falls through to derived", time.Hour, "garbage", 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from := now
			to := now.Add(tc.dur)
			got := pickStep(from, to, tc.over)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
