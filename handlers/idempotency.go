package handlers

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// idemRecord holds the in-flight lock and cached result for one idempotency key.
type idemRecord struct {
	mu        sync.Mutex // serializes requests sharing this key
	done      bool       // true once a successful response is cached
	status    int
	body      gin.H
	expiresAt time.Time
}

// idemStore is a TTL cache that gives at-most-once semantics to handlers keyed
// by a client-supplied Idempotency-Key. Requests with the same key are
// serialized; once one produces a cached result, the rest replay it instead of
// re-running the work (e.g. building a second on-chain order on a double-click).
type idemStore struct {
	mu      sync.Mutex
	records map[string]*idemRecord
	ttl     time.Duration
}

func newIdemStore(ttl time.Duration) *idemStore {
	return &idemStore{records: map[string]*idemRecord{}, ttl: ttl}
}

// getOrCreate returns the record for key, creating it if absent, and sweeps
// expired records so the map can't grow without bound.
func (s *idemStore) getOrCreate(key string) *idemRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, r := range s.records {
		if now.After(r.expiresAt) {
			delete(s.records, k)
		}
	}

	if r, ok := s.records[key]; ok {
		return r
	}
	r := &idemRecord{expiresAt: now.Add(s.ttl)}
	s.records[key] = r
	return r
}

// orderIdem caches CreateOrder responses. The TTL only needs to cover the burst
// of retries around a single user action.
var orderIdem = newIdemStore(2 * time.Minute)
