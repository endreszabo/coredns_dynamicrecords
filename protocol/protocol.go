// Package protocol defines the wire types shared between the dynamicrecords
// CoreDNS plugin (server) and any client that ingests records via the
// FrameStreams or HTTPS transports.
//
// Importing this package only requires the Go standard library, making it
// suitable for lightweight client binaries that should not pull in the full
// CoreDNS dependency tree.
package protocol

// FstrmALPN is the TLS ALPN protocol identifier advertised by the
// FrameStreams listener.  It distinguishes the FrameStreams port from the
// HTTPS port at the TLS handshake layer, before any application data flows.
// Clients must include this value in tls.Config.NextProtos.
const FstrmALPN = "fstrm"

// FstrmContentType is the FrameStreams content-type negotiated during the
// bidirectional handshake.  Both client and server must agree on this value.
// It is distinct from DNSTAP's "protobuf:dnstap.Dnstap".
const FstrmContentType = "application/x-dynamicrecords"

// FstrmFrame is the JSON payload carried in each FrameStreams data frame.
// The same fields are accepted by the HTTPS /records and /records/delete
// endpoints via RRSetRequest.
type FstrmFrame struct {
	Op      string   `json:"op"`      // "add" or "delete" (required)
	TTL     uint32   `json:"ttl"`     // optional TTL override in seconds
	Expiry  int64    `json:"expiry"`  // optional Unix timestamp expiry
	Records []string `json:"records"` // RFC1035 zone file records (required)
}

// APIResponse is the uniform JSON envelope returned after every operation on
// both the HTTPS and FrameStreams transports.
//
//	{"ok": true,  "message": "Added 2 records for svc.example.com./A"}
//	{"ok": false, "error":   "invalid record format: ..."}
type APIResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}
