package dynamicrecords

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/miekg/dns"
)

// RRSetRequest represents the JSON payload for adding/deleting records
type RRSetRequest struct {
	TTL     uint32   `json:"ttl"`     // Optional: override TTL from records
	Expiry  int64    `json:"expiry"`  // Unix timestamp, optional
	Records []string `json:"records"` // RFC1035 zone file format (required)
}

// handleRecords handles POST requests to add RRsets
func (s *SharedServer) handleRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req RRSetRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if len(req.Records) == 0 {
		http.Error(w, "Missing required field: records", http.StatusBadRequest)
		return
	}

	// Parse records from RFC1035 format
	var records []dns.RR
	for _, recordStr := range req.Records {
		rr, err := dns.NewRR(recordStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid record format: %v", err), http.StatusBadRequest)
			return
		}
		records = append(records, rr)
	}

	// Extract qname and qtype from the first record
	if len(records) == 0 {
		http.Error(w, "No valid records provided", http.StatusBadRequest)
		return
	}

	firstRecord := records[0]
	qname := firstRecord.Header().Name
	qtype := firstRecord.Header().Rrtype

	// Validate all records have the same qname and qtype
	for i, rr := range records {
		if rr.Header().Name != qname {
			http.Error(w, fmt.Sprintf("Record %d has different qname: expected %s, got %s", i, qname, rr.Header().Name), http.StatusBadRequest)
			return
		}
		if rr.Header().Rrtype != qtype {
			http.Error(w, fmt.Sprintf("Record %d has different qtype: expected %s, got %s", i, dns.TypeToString[qtype], dns.TypeToString[rr.Header().Rrtype]), http.StatusBadRequest)
			return
		}
	}

	// Use TTL override if provided, otherwise use TTL from records
	var expiry time.Time
	if req.Expiry > 0 {
		expiry = time.Unix(req.Expiry, 0)
	} else {
		// Use TTL from request or from first record
		ttl := req.TTL
		if ttl == 0 {
			ttl = firstRecord.Header().Ttl
		}
		if ttl == 0 {
			ttl = s.defaultTTL
		}
		expiry = time.Now().Add(time.Duration(ttl) * time.Second)
	}

	// Add to buffer
	s.buffer.Add(qname, qtype, records, expiry)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("Added %d records for %s/%s", len(records), qname, dns.TypeToString[qtype]),
	})
}

// handleDelete handles DELETE requests to remove specific RRs
func (s *SharedServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req RRSetRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if len(req.Records) == 0 {
		http.Error(w, "Missing required field: records", http.StatusBadRequest)
		return
	}

	// Parse records from RFC1035 format
	var recordsToDelete []dns.RR
	for _, recordStr := range req.Records {
		rr, err := dns.NewRR(recordStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid record format: %v", err), http.StatusBadRequest)
			return
		}
		recordsToDelete = append(recordsToDelete, rr)
	}

	// Extract qname and qtype from the first record
	if len(recordsToDelete) == 0 {
		http.Error(w, "No valid records provided", http.StatusBadRequest)
		return
	}

	firstRecord := recordsToDelete[0]
	qname := firstRecord.Header().Name
	qtype := firstRecord.Header().Rrtype

	// Validate all records have the same qname and qtype
	for i, rr := range recordsToDelete {
		if rr.Header().Name != qname {
			http.Error(w, fmt.Sprintf("Record %d has different qname: expected %s, got %s", i, qname, rr.Header().Name), http.StatusBadRequest)
			return
		}
		if rr.Header().Rrtype != qtype {
			http.Error(w, fmt.Sprintf("Record %d has different qtype: expected %s, got %s", i, dns.TypeToString[qtype], dns.TypeToString[rr.Header().Rrtype]), http.StatusBadRequest)
			return
		}
	}

	// Delete specific records from buffer
	deleted := s.buffer.DeleteRecords(qname, qtype, recordsToDelete)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("Deleted %d records for %s/%s", deleted, qname, dns.TypeToString[qtype]),
	})
}

// handleHealth handles health check requests
func (s *SharedServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "healthy",
		"buffer_size":   s.buffer.Size(),
		"plugin":        "dynamicrecords",
		"instances":     s.instances,
	})
}
