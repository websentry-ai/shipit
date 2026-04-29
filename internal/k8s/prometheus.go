package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"k8s.io/client-go/rest"
)

// PromQL query proxy. Reaches the in-cluster Prometheus through the
// kube-apiserver's service proxy ("/api/v1/namespaces/<ns>/services/<svc>:<port>/proxy/...")
// so we don't have to open a port-forward from shipit. Auth/TLS material
// comes from the same rest.Config that drives DeployApp.

// PromMatrix is the shape Prometheus returns for /api/v1/query_range with
// resultType=matrix. We only decode the bits the UI consumes.
type PromMatrix struct {
	Series []PromSeries `json:"series"`
}

type PromSeries struct {
	Labels map[string]string `json:"labels"`
	Points []PromPoint       `json:"points"`
}

type PromPoint struct {
	T float64 `json:"t"` // unix seconds
	V float64 `json:"v"`
}

// QueryPrometheusRange runs a PromQL range query against the in-cluster
// Prometheus over the apiserver service proxy. Returns one series per
// distinct label-set. step < 1s is clamped to 1s.
func (c *Client) QueryPrometheusRange(ctx context.Context, query string, from, to time.Time, step time.Duration) (*PromMatrix, error) {
	if step < time.Second {
		step = time.Second
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("start", strconv.FormatInt(from.Unix(), 10))
	q.Set("end", strconv.FormatInt(to.Unix(), 10))
	q.Set("step", strconv.FormatInt(int64(step.Seconds()), 10))

	body, err := c.apiserverProxyGet(ctx, promServiceURL("/api/v1/query_range", q))
	if err != nil {
		return nil, err
	}
	return parsePromMatrix(body)
}

// apiserverProxyGet issues an authenticated GET to the kube-apiserver at the
// given absolute path. The path must already include the
// "/api/v1/namespaces/.../services/.../proxy/..." prefix. Auth headers and
// TLS root CAs come from rest.Config; the URL host is config.Host.
func (c *Client) apiserverProxyGet(ctx context.Context, path string) ([]byte, error) {
	httpClient, err := rest.HTTPClientFor(c.config)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}
	u := c.config.Host + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("apiserver proxy %d: %s", resp.StatusCode, truncate(string(body)))
	}
	return body, nil
}

// parsePromMatrix decodes the Prometheus query_range JSON envelope into the
// flatter shape the UI consumes. Skips points whose value won't parse as
// float (Prometheus emits "NaN" / "+Inf" for some sums).
func parsePromMatrix(body []byte) (*PromMatrix, error) {
	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]interface{}  `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w (body: %s)", err, truncate(string(body)))
	}
	if raw.Status != "success" {
		return nil, fmt.Errorf("prometheus error: %s", raw.Error)
	}
	out := &PromMatrix{Series: make([]PromSeries, 0, len(raw.Data.Result))}
	for _, r := range raw.Data.Result {
		s := PromSeries{Labels: r.Metric, Points: make([]PromPoint, 0, len(r.Values))}
		for _, v := range r.Values {
			ts, _ := v[0].(float64)
			vs, _ := v[1].(string)
			f, err := strconv.ParseFloat(vs, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				// Prometheus emits "NaN"/"+Inf" for divisions-by-zero etc.;
				// drop them since recharts can't render them anyway.
				continue
			}
			s.Points = append(s.Points, PromPoint{T: ts, V: f})
		}
		out.Series = append(out.Series, s)
	}
	return out, nil
}

func truncate(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
