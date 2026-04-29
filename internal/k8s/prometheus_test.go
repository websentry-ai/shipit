package k8s

import (
	"strings"
	"testing"
)

// parsePromMatrix is the only piece in prometheus.go with real logic; the
// HTTP path is exercised by integration testing against a real cluster.
// Cover the tricky bits: NaN/Inf values that ParseFloat rejects, missing
// labels, error envelope handling.
func TestParsePromMatrix(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [
				{
					"metric": {"pod": "p1"},
					"values": [[1700000000, "0.5"], [1700000060, "0.6"]]
				},
				{
					"metric": {"pod": "p2"},
					"values": [[1700000000, "NaN"], [1700000060, "0.1"]]
				}
			]
		}
	}`)
	m, err := parsePromMatrix(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Series) != 2 {
		t.Fatalf("series=%d, want 2", len(m.Series))
	}
	if len(m.Series[0].Points) != 2 {
		t.Errorf("p1 points=%d, want 2", len(m.Series[0].Points))
	}
	// NaN value must be dropped, not propagate as a JSON parse error.
	if len(m.Series[1].Points) != 1 || m.Series[1].Points[0].V != 0.1 {
		t.Errorf("p2 should have 1 point with value 0.1 (NaN dropped), got %+v", m.Series[1].Points)
	}
}

func TestParsePromMatrix_ErrorEnvelope(t *testing.T) {
	body := []byte(`{"status":"error","error":"unknown function"}`)
	if _, err := parsePromMatrix(body); err == nil || !strings.Contains(err.Error(), "unknown function") {
		t.Errorf("expected error envelope to surface, got %v", err)
	}
}
