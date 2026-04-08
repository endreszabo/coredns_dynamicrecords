package dynamicrecords

import (
	"sync"
	"time"

	"github.com/miekg/dns"
)

// RREntry represents a single batch of resource records with its own expiry.
// Multiple RREntry values can coexist for the same (QName, QType) tuple.
type RREntry struct {
	QName   string
	QType   uint16
	Records []dns.RR
	Expiry  time.Time
}

// RRBuffer is a thread-safe in-memory buffer for DNS records.
// Each (qname, qtype) key maps to an ordered slice of batches; each batch
// was created by a single Add() call and carries its own expiry.
type RRBuffer struct {
	mu              sync.RWMutex
	entries         map[string]map[uint16][]*RREntry // map[qname]map[qtype][]batch
	cleanupInterval time.Duration
}

// NewRRBuffer creates a new RRBuffer. cleanupInterval controls how often the
// background goroutine scans for and removes expired batches; 0 uses the
// default of 60 seconds.
func NewRRBuffer(cleanupInterval time.Duration) *RRBuffer {
	if cleanupInterval <= 0 {
		cleanupInterval = 60 * time.Second
	}
	buf := &RRBuffer{
		entries:         make(map[string]map[uint16][]*RREntry),
		cleanupInterval: cleanupInterval,
	}

	// Start cleanup goroutine
	go buf.cleanupExpired()

	return buf
}

// Add inserts a new batch of records for the given (qname, qtype).
// When replace is false (the default), the batch is appended to any existing
// batches so that multiple independent sets of records can coexist under the
// same name and type (e.g. for ACME DNS-01 wildcard challenges).
// When replace is true, all existing batches for (qname, qtype) are discarded
// and replaced by this single new batch.
func (b *RRBuffer) Add(qname string, qtype uint16, records []dns.RR, expiry time.Time, replace bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Normalize qname (lowercase, ensure trailing dot)
	qname = dns.Fqdn(dns.CanonicalName(qname))

	if b.entries[qname] == nil {
		b.entries[qname] = make(map[uint16][]*RREntry)
	}

	entry := &RREntry{
		QName:   qname,
		QType:   qtype,
		Records: records,
		Expiry:  expiry,
	}

	if replace {
		b.entries[qname][qtype] = []*RREntry{entry}
	} else {
		b.entries[qname][qtype] = append(b.entries[qname][qtype], entry)
	}
}

// Get retrieves all non-expired records matching the qname and qtype,
// aggregated across all batches in insertion order.
func (b *RRBuffer) Get(qname string, qtype uint16) []dns.RR {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Normalize qname
	qname = dns.Fqdn(dns.CanonicalName(qname))

	typeMap, exists := b.entries[qname]
	if !exists {
		return nil
	}

	batches, exists := typeMap[qtype]
	if !exists {
		return nil
	}

	now := time.Now()
	var result []dns.RR
	for _, entry := range batches {
		if now.After(entry.Expiry) {
			continue
		}
		// Append a copy of each record to prevent external modification
		for _, rr := range entry.Records {
			result = append(result, dns.Copy(rr))
		}
	}
	return result
}

// Delete removes all batches for the given (qname, qtype).
func (b *RRBuffer) Delete(qname string, qtype uint16) {
	b.mu.Lock()
	defer b.mu.Unlock()

	qname = dns.Fqdn(dns.CanonicalName(qname))

	if typeMap, exists := b.entries[qname]; exists {
		delete(typeMap, qtype)
		if len(typeMap) == 0 {
			delete(b.entries, qname)
		}
	}
}

// DeleteRecords removes the first batch whose records exactly match
// recordsToDelete (multiset equality, order-insensitive).
// Returns the number of records that were in the removed batch, or 0 if no
// matching batch was found. Exactly one batch is removed per call, so
// two identical batches require two separate DeleteRecords calls.
func (b *RRBuffer) DeleteRecords(qname string, qtype uint16, recordsToDelete []dns.RR) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	qname = dns.Fqdn(dns.CanonicalName(qname))

	typeMap, exists := b.entries[qname]
	if !exists {
		return 0
	}

	batches, exists := typeMap[qtype]
	if !exists {
		return 0
	}

	for i, batch := range batches {
		if batchMatches(batch, recordsToDelete) {
			removed := len(batch.Records)
			// Remove this batch from the slice
			updated := append(batches[:i:i], batches[i+1:]...)
			if len(updated) == 0 {
				delete(typeMap, qtype)
				if len(typeMap) == 0 {
					delete(b.entries, qname)
				}
			} else {
				typeMap[qtype] = updated
			}
			return removed
		}
	}

	return 0
}

// batchMatches reports whether batch.Records is multiset-equal to want.
// Order does not matter; duplicate records within a batch are counted.
func batchMatches(batch *RREntry, want []dns.RR) bool {
	if len(batch.Records) != len(want) {
		return false
	}
	used := make([]bool, len(batch.Records))
	for _, w := range want {
		found := false
		for i, r := range batch.Records {
			if !used[i] && rrEqual(r, w) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// rrEqual compares two DNS resource records for equality
func rrEqual(a, b dns.RR) bool {
	if a.Header().Name != b.Header().Name {
		return false
	}
	if a.Header().Rrtype != b.Header().Rrtype {
		return false
	}
	if a.Header().Class != b.Header().Class {
		return false
	}

	// Compare the string representation of the record data (RDATA)
	// This works for all record types
	return a.String() == b.String()
}

// cleanupExpired periodically removes expired batches.
func (b *RRBuffer) cleanupExpired() {
	ticker := time.NewTicker(b.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		b.mu.Lock()
		now := time.Now()

		for qname, typeMap := range b.entries {
			for qtype, batches := range typeMap {
				var alive []*RREntry
				for _, entry := range batches {
					if !now.After(entry.Expiry) {
						alive = append(alive, entry)
					}
				}
				if len(alive) == 0 {
					delete(typeMap, qtype)
				} else {
					typeMap[qtype] = alive
				}
			}
			if len(typeMap) == 0 {
				delete(b.entries, qname)
			}
		}

		b.mu.Unlock()
	}
}

// Size returns the total number of live batches across all (qname, qtype) keys.
func (b *RRBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := 0
	for _, typeMap := range b.entries {
		for _, batches := range typeMap {
			count += len(batches)
		}
	}
	return count
}
