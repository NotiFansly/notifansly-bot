// internal/health/aggregator.go
package health

import (
	"github.com/NotiFansly/notifansly-bot/internal/database"
	"log"
	"sync/atomic"
	"time"
)

// Aggregator holds API health stats in memory to reduce database writes.
type Aggregator struct {
	repo               *database.Repository
	serviceName        string
	totalRequests      atomic.Uint64
	successfulRequests atomic.Uint64
}

// NewAggregator creates a new health aggregator.
func NewAggregator(repo *database.Repository, serviceName string) *Aggregator {
	return &Aggregator{
		repo:        repo,
		serviceName: serviceName,
	}
}

// RecordCall increments the in-memory counters for an API call. This is non-blocking and fast.
func (a *Aggregator) RecordCall(success bool) {
	a.totalRequests.Add(1)
	if success {
		a.successfulRequests.Add(1)
	}
}

// FlushToDB writes the aggregated counts to the database and resets the counters.
func (a *Aggregator) FlushToDB() {
	// Atomically load and reset the counters
	total := a.totalRequests.Swap(0)
	successful := a.successfulRequests.Swap(0)

	if total == 0 {
		return // No activity to report
	}

	if err := a.repo.UpdateAPIHealthBulk(a.serviceName, total, successful); err != nil {
		log.Printf("ERROR: Failed to flush API health stats to DB for service %s: %v", a.serviceName, err)
	}
}

// Start starts a background goroutine to periodically flush stats to the database.
func (a *Aggregator) Start(interval time.Duration) {
	log.Printf("Health Aggregator for '%s' started with a %s flush interval", a.serviceName, interval)
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			a.FlushToDB()
		}
	}()
}
