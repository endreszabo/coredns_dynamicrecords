package dynamicrecords

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	framestream "github.com/farsightsec/golang-framestream"
	"github.com/miekg/dns"

	"github.com/yourusername/dynamicrecords/protocol"
)

// FstrmContentType and FstrmFrame are re-exported from the protocol package
// so existing code in this package continues to compile unchanged.
const FstrmContentType = protocol.FstrmContentType

// FstrmFrame is a type alias for protocol.FstrmFrame.
type FstrmFrame = protocol.FstrmFrame

// acceptFstrmConns accepts incoming FrameStreams TLS connections and dispatches
// each one to a handler goroutine.
func (s *SharedServer) acceptFstrmConns(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener closed — normal shutdown.
			return
		}
		go s.handleFstrmConn(conn)
	}
}

// handleFstrmConn drives the server side of a single FrameStreams connection.
// NewReader performs the bidirectional handshake (READY → ACCEPT → START).
// After each data frame the server writes a JSON ACK/NACK directly on conn
// (the library only uses the write side for ACCEPT and FINISH, both outside
// the data loop, so there is no conflict).
func (s *SharedServer) handleFstrmConn(conn net.Conn) {
	defer conn.Close()

	r, err := framestream.NewReader(conn, &framestream.ReaderOptions{
		ContentTypes:  [][]byte{[]byte(FstrmContentType)},
		Bidirectional: true,
		Timeout:       10 * time.Second,
	})
	if err != nil {
		fmt.Printf("dynamicrecords: fstrm handshake from %s: %v\n", conn.RemoteAddr(), err)
		return
	}

	enc := json.NewEncoder(conn)
	buf := make([]byte, framestream.DEFAULT_MAX_PAYLOAD_SIZE)
	for {
		n, err := r.ReadFrame(buf)
		if err != nil {
			if err != framestream.EOF {
				fmt.Printf("dynamicrecords: fstrm read from %s: %v\n", conn.RemoteAddr(), err)
			}
			return
		}

		var f FstrmFrame
		if err := json.Unmarshal(buf[:n], &f); err != nil {
			enc.Encode(APIResponse{OK: false, Error: err.Error()})
			continue
		}

		msg, err := s.processFstrmFrame(&f)
		if err != nil {
			enc.Encode(APIResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(APIResponse{OK: true, Message: msg})
		}
	}
}

// processFstrmFrame applies a decoded FrameStreams frame to the record buffer.
// It returns a human-readable success message and any error.
func (s *SharedServer) processFstrmFrame(f *FstrmFrame) (string, error) {
	if len(f.Records) == 0 {
		return "", fmt.Errorf("missing records field")
	}

	var records []dns.RR
	for _, rs := range f.Records {
		rr, err := dns.NewRR(rs)
		if err != nil {
			return "", fmt.Errorf("invalid record %q: %v", rs, err)
		}
		records = append(records, rr)
	}

	first := records[0]
	qname := first.Header().Name
	qtype := first.Header().Rrtype

	for i, rr := range records[1:] {
		if rr.Header().Name != qname || rr.Header().Rrtype != qtype {
			return "", fmt.Errorf("record %d: mixed qname/qtype in single frame", i+1)
		}
	}

	switch f.Op {
	case "add":
		var expiry time.Time
		if f.Expiry > 0 {
			expiry = time.Unix(f.Expiry, 0)
		} else {
			ttl := f.TTL
			if ttl == 0 {
				ttl = first.Header().Ttl
			}
			if ttl == 0 {
				ttl = s.defaultTTL
			}
			expiry = time.Now().Add(time.Duration(ttl) * time.Second)
		}
		s.buffer.Add(qname, qtype, records, expiry)
		return fmt.Sprintf("Added %d records for %s/%s", len(records), qname, dns.TypeToString[qtype]), nil

	case "delete":
		deleted := s.buffer.DeleteRecords(qname, qtype, records)
		return fmt.Sprintf("Deleted %d records for %s/%s", deleted, qname, dns.TypeToString[qtype]), nil

	default:
		return "", fmt.Errorf("unknown op %q (want \"add\" or \"delete\")", f.Op)
	}
}
