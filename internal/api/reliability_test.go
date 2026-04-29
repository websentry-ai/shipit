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

func strPtrAPI(s string) *string { return &s }
