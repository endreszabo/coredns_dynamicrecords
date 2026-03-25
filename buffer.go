package dynamicrecords

import (
	"sync"
	"time"

	"github.com/miekg/dns"
)

// RREntry represents a resource record set with expiry
type RREntry struct {
	QName   string
	QType   uint16
	Records []dns.RR
	Expiry  time.Time
}

// RRBuffer is a thread-safe in-memory buffer for DNS records
type RRBuffer struct {
	mu      sync.RWMutex
	entries map[string]map[uint16]*RREntry // map[qname]map[qtype]entry
}

// NewRRBuffer creates a new RRBuffer
func NewRRBuffer() *RRBuffer {
	buf := &RRBuffer{
		entries: make(map[string]map[uint16]*RREntry),
	}

	// Start cleanup goroutine
	go buf.cleanupExpired()

	return buf
}

// Add inserts or updates an RRset in the buffer
func (b *RRBuffer) Add(qname string, qtype uint16, records []dns.RR, expiry time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Normalize qname (lowercase, ensure trailing dot)
	qname = dns.Fqdn(dns.CanonicalName(qname))

	if b.entries[qname] == nil {
		b.entries[qname] = make(map[uint16]*RREntry)
	}

	b.entries[qname][qtype] = &RREntry{
		QName:   qname,
		QType:   qtype,
		Records: records,
		Expiry:  expiry,
	}
}

// Get retrieves records matching the qname and qtype
func (b *RRBuffer) Get(qname string, qtype uint16) []dns.RR {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Normalize qname
	qname = dns.Fqdn(dns.CanonicalName(qname))

	typeMap, exists := b.entries[qname]
	if !exists {
		return nil
	}

	entry, exists := typeMap[qtype]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().After(entry.Expiry) {
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]dns.RR, len(entry.Records))
	copy(result, entry.Records)
	return result
}

// Delete removes an entire RRset from the buffer (all records for qname/qtype)
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

// DeleteRecords removes specific records from the buffer, matching by RR value
// Returns the number of records deleted
func (b *RRBuffer) DeleteRecords(qname string, qtype uint16, recordsToDelete []dns.RR) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	qname = dns.Fqdn(dns.CanonicalName(qname))

	typeMap, exists := b.entries[qname]
	if !exists {
		return 0
	}

	entry, exists := typeMap[qtype]
	if !exists {
		return 0
	}

	// Build a new slice without the records to delete
	var remainingRecords []dns.RR
	deletedCount := 0

	for _, existingRR := range entry.Records {
		shouldDelete := false
		for _, deleteRR := range recordsToDelete {
			if rrEqual(existingRR, deleteRR) {
				shouldDelete = true
				deletedCount++
				break
			}
		}
		if !shouldDelete {
			remainingRecords = append(remainingRecords, existingRR)
		}
	}

	// If all records were deleted, remove the entry
	if len(remainingRecords) == 0 {
		delete(typeMap, qtype)
		if len(typeMap) == 0 {
			delete(b.entries, qname)
		}
	} else {
		// Update the entry with remaining records
		entry.Records = remainingRecords
	}

	return deletedCount
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

// cleanupExpired periodically removes expired entries
func (b *RRBuffer) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.mu.Lock()
		now := time.Now()

		for qname, typeMap := range b.entries {
			for qtype, entry := range typeMap {
				if now.After(entry.Expiry) {
					delete(typeMap, qtype)
				}
			}
			if len(typeMap) == 0 {
				delete(b.entries, qname)
			}
		}

		b.mu.Unlock()
	}
}

// Size returns the total number of RRsets in the buffer
func (b *RRBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := 0
	for _, typeMap := range b.entries {
		count += len(typeMap)
	}
	return count
}
