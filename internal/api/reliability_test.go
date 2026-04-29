package api

import "testing"

// resolveUnavailableToInt must reject the percent-bypass case. K8s rounds
// down (floor) for maxUnavailable, so 100% on 2 replicas = 2 = drains
// everything. Earlier the validator only checked integer values, letting
// "100%" silently persist a zero-pod configuration.
func TestResolveUnavailableToInt(t *testing.T) {
	cases := []struct {
		name     string
		s        *string
		replicas int
		want     int
		wantOK   bool
	}{
		{"nil override", nil, 3, 0, false},
		{"int", strPtrAPI("1"), 3, 1, true},
		{"int equals replicas", strPtrAPI("3"), 3, 3, true},
		{"100% on 2 replicas", strPtrAPI("100%"), 2, 2, true},
		{"50% on 2 replicas floors to 1", strPtrAPI("50%"), 2, 1, true},
		{"99% on 2 replicas floors to 1", strPtrAPI("99%"), 2, 1, true},
		{"25% on 10 replicas", strPtrAPI("25%"), 10, 2, true},
		{"garbage override", strPtrAPI("xyz"), 3, 0, false},
		{"garbage percent", strPtrAPI("xyz%"), 3, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := resolveUnavailableToInt(tc.s, tc.replicas)
			if n != tc.want || ok != tc.wantOK {
				t.Errorf("got (%d,%v), want (%d,%v)", n, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// On fleets with <=3 replicas the renderer derives maxUnavailable="0", so
// the deadlock check must evaluate *effective* values: explicit surge="0"
// + omitted unavail still produces a 0/0 rollout that can never make
// progress. Earlier the check only inspected the explicit overrides.
func TestDerivedRollingUpdate_DeadlockCases(t *testing.T) {
	cases := []struct {
		name           string
		replicas       int32
		surgeOverride  *string
		unavailOverr   *string
		wantSurge      string
		wantUnavail    string
		wantDeadlocked bool
	}{
		{"small fleet defaults", 3, nil, nil, "1", "0", false},
		{"large fleet defaults", 10, nil, nil, "25%", "25%", false},
		{"surge=0 + nil unavail on small fleet", 3, strPtrAPI("0"), nil, "0", "0", true},
		{"surge=0% + nil unavail on small fleet", 3, strPtrAPI("0%"), nil, "0%", "0", true},
		{"both explicit zero", 5, strPtrAPI("0"), strPtrAPI("0"), "0", "0", true},
		{"explicit surge=0 on large fleet (unavail derives to 25%)", 10, strPtrAPI("0"), nil, "0", "25%", false},
		{"explicit unavail=0 on large fleet (surge derives to 25%)", 10, nil, strPtrAPI("0"), "25%", "0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, u := derivedRollingUpdate(tc.replicas, tc.surgeOverride, tc.unavailOverr)
			if s != tc.wantSurge || u != tc.wantUnavail {
				t.Errorf("got (%q,%q), want (%q,%q)", s, u, tc.wantSurge, tc.wantUnavail)
			}
			deadlocked := isZero(&s) && isZero(&u)
			if deadlocked != tc.wantDeadlocked {
				t.Errorf("deadlock=%v, want %v", deadlocked, tc.wantDeadlocked)
			}
		})
	}
}

func strPtrAPI(s string) *string { return &s }
