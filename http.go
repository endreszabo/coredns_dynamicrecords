package dynamicrecords

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/miekg/dns"

	"github.com/endreszabo/coredns_dynamicrecords/protocol"
)

// APIResponse is a type alias for protocol.APIResponse so existing code in
// this package continues to compile unchanged.
type APIResponse = protocol.APIResponse

// jsonError writes a JSON-encoded APIResponse with ok:false and the given
// HTTP status code. It replaces http.Error so all error responses are JSON.
func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(APIResponse{OK: false, Error: msg})
}

// RRSetRequest represents the JSON payload for adding/deleting records
type RRSetRequest struct {
	TTL     uint32   `json:"ttl"`     // Optional: override TTL from records
	Expiry  int64    `json:"expiry"`  // Unix timestamp, optional
	Records []string `json:"records"` // RFC1035 zone file format (required)
}

// handleRecords handles POST requests to add RRsets
func (s *SharedServer) handleRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		operationsCount.WithLabelValues("add", "http", "error").Inc()
		jsonError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req RRSetRequest
	if err := json.Unmarshal(body, &req); err != nil {
		operationsCount.WithLabelValues("add", "http", "error").Inc()
		jsonError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Records) == 0 {
		operationsCount.WithLabelValues("add", "http", "error").Inc()
		jsonError(w, "Missing required field: records", http.StatusBadRequest)
		return
	}

	var records []dns.RR
	for _, recordStr := range req.Records {
		rr, err := dns.NewRR(recordStr)
		if err != nil {
			operationsCount.WithLabelValues("add", "http", "error").Inc()
			jsonError(w, fmt.Sprintf("Invalid record format: %v", err), http.StatusBadRequest)
			return
		}
		records = append(records, rr)
	}

	firstRecord := records[0]
	qname := firstRecord.Header().Name
	qtype := firstRecord.Header().Rrtype

	for i, rr := range records {
		if rr.Header().Name != qname {
			operationsCount.WithLabelValues("add", "http", "error").Inc()
			jsonError(w, fmt.Sprintf("Record %d has different qname: expected %s, got %s", i, qname, rr.Header().Name), http.StatusBadRequest)
			return
		}
		if rr.Header().Rrtype != qtype {
			operationsCount.WithLabelValues("add", "http", "error").Inc()
			jsonError(w, fmt.Sprintf("Record %d has different qtype: expected %s, got %s", i, dns.TypeToString[qtype], dns.TypeToString[rr.Header().Rrtype]), http.StatusBadRequest)
			return
		}
	}

	var expiry time.Time
	if req.Expiry > 0 {
		expiry = time.Unix(req.Expiry, 0)
	} else {
		ttl := req.TTL
		if ttl == 0 {
			ttl = firstRecord.Header().Ttl
		}
		if ttl == 0 {
			ttl = s.defaultTTL
		}
		expiry = time.Now().Add(time.Duration(ttl) * time.Second)
	}

	s.buffer.Add(qname, qtype, records, expiry)
	operationsCount.WithLabelValues("add", "http", "success").Inc()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(APIResponse{
		OK:      true,
		Message: fmt.Sprintf("Added %d records for %s/%s", len(records), qname, dns.TypeToString[qtype]),
	})
}

// handleDelete handles DELETE/POST requests to remove specific RRs
func (s *SharedServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		operationsCount.WithLabelValues("remove", "http", "error").Inc()
		jsonError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req RRSetRequest
	if err := json.Unmarshal(body, &req); err != nil {
		operationsCount.WithLabelValues("remove", "http", "error").Inc()
		jsonError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Records) == 0 {
		operationsCount.WithLabelValues("remove", "http", "error").Inc()
		jsonError(w, "Missing required field: records", http.StatusBadRequest)
		return
	}

	var recordsToDelete []dns.RR
	for _, recordStr := range req.Records {
		rr, err := dns.NewRR(recordStr)
		if err != nil {
			operationsCount.WithLabelValues("remove", "http", "error").Inc()
			jsonError(w, fmt.Sprintf("Invalid record format: %v", err), http.StatusBadRequest)
			return
		}
		recordsToDelete = append(recordsToDelete, rr)
	}

	firstRecord := recordsToDelete[0]
	qname := firstRecord.Header().Name
	qtype := firstRecord.Header().Rrtype

	for i, rr := range recordsToDelete {
		if rr.Header().Name != qname {
			operationsCount.WithLabelValues("remove", "http", "error").Inc()
			jsonError(w, fmt.Sprintf("Record %d has different qname: expected %s, got %s", i, qname, rr.Header().Name), http.StatusBadRequest)
			return
		}
		if rr.Header().Rrtype != qtype {
			operationsCount.WithLabelValues("remove", "http", "error").Inc()
			jsonError(w, fmt.Sprintf("Record %d has different qtype: expected %s, got %s", i, dns.TypeToString[qtype], dns.TypeToString[rr.Header().Rrtype]), http.StatusBadRequest)
			return
		}
	}

	deleted := s.buffer.DeleteRecords(qname, qtype, recordsToDelete)
	operationsCount.WithLabelValues("remove", "http", "success").Inc()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(APIResponse{
		OK:      true,
		Message: fmt.Sprintf("Deleted %d records for %s/%s", deleted, qname, dns.TypeToString[qtype]),
	})
}

// handleHealth handles health check requests
func (s *SharedServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"buffer_size": s.buffer.Size(),
		"plugin":    "dynamicrecords",
		"instances": s.instances,
	})
}
