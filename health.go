package supermarket

import (
	"context"
	"net/http"
)

// Health is the status object returned by /health (and the /status alias).
// Status is "ok" when PostgreSQL, Redis and Sidekiq are all reachable. A
// subsystem that could not be reached reports only its Status
// ("unreachable" or "unknown"), leaving its metrics zero.
type Health struct {
	Status      string            `json:"status"`
	Supermarket HealthSupermarket `json:"supermarket"`
	PostgreSQL  HealthPostgreSQL  `json:"postgresql"`
	Sidekiq     HealthSidekiq     `json:"sidekiq"`
	Redis       HealthRedis       `json:"redis"`
	// Features maps each feature flag (tools, fieri, announcement, github,
	// no_crawl) to whether it is enabled.
	Features map[string]bool `json:"features"`
}

// HealthSupermarket holds application-level health figures.
type HealthSupermarket struct {
	ExpiredOCIDTokens int `json:"expired_ocid_tokens"`
}

// HealthPostgreSQL describes the database connection.
type HealthPostgreSQL struct {
	Status        string `json:"status"`
	WaitingOnLock int    `json:"waiting_on_lock"`
	Connections   int    `json:"connections"`
}

// HealthSidekiq describes the background job queue.
type HealthSidekiq struct {
	Status         string  `json:"status"`
	Latency        float64 `json:"latency"` // seconds the oldest queued job has waited
	QueuedJobs     int     `json:"queued_jobs"`
	ScheduledJobs  int     `json:"scheduled_jobs"`
	RetryableJobs  int     `json:"retryable_jobs"`
	DeadJobs       int     `json:"dead_jobs"`
	TotalProcessed int64   `json:"total_processed"`
	TotalFailed    int64   `json:"total_failed"`
	ActiveWorkers  int     `json:"active_workers"`
}

// HealthRedis describes the Redis server.
type HealthRedis struct {
	Status           string `json:"status"`
	UptimeInSeconds  int64  `json:"uptime_in_seconds"`
	ConnectedClients int    `json:"connected_clients"`
	UsedMemory       int64  `json:"used_memory"`
	UsedMemoryPeak   int64  `json:"used_memory_peak"`
}

// HealthService accesses the health / metrics endpoints.
type HealthService struct{ client *Client }

// Status hits /api/v1/health and returns the parsed status object.
func (s *HealthService) Status(ctx context.Context) (*Health, *Response, error) {
	h, resp, err := doJSON[Health](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/api/v1/health",
	})
	if err != nil {
		return nil, resp, err
	}
	return &h, resp, nil
}

// Metrics hits /api/v1/metrics and returns the raw decoded JSON. The
// shape is server-version-specific so callers receive map[string]any.
func (s *HealthService) Metrics(ctx context.Context) (map[string]any, *Response, error) {
	return doJSON[map[string]any](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/api/v1/metrics",
	})
}
