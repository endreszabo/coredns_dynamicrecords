package dynamicrecords

import (
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	framestream "github.com/farsightsec/golang-framestream"
	"github.com/miekg/dns"
)

// --- processFstrmFrame unit tests ---

func TestProcessFstrmFrame_Add(t *testing.T) {
	s := newTestServer()
	msg, err := s.processFstrmFrame(&FstrmFrame{
		Op:      "add",
		Records: []string{"svc.example.com. 60 IN A 10.0.0.1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == "" {
		t.Error("expected non-empty success message")
	}

	records := s.buffer.Get("svc.example.com.", dns.TypeA)
	if len(records) != 1 {
		t.Fatalf("buffer: got %d records, want 1", len(records))
	}
}

func TestProcessFstrmFrame_AddMultiple(t *testing.T) {
	s := newTestServer()
	_, err := s.processFstrmFrame(&FstrmFrame{
		Op: "add",
		Records: []string{
			"multi.example.com. 60 IN A 10.0.0.1",
			"multi.example.com. 60 IN A 10.0.0.2",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := s.buffer.Get("multi.example.com.", dns.TypeA)
	if len(records) != 2 {
		t.Fatalf("buffer: got %d records, want 2", len(records))
	}
}

func TestProcessFstrmFrame_AddWithTTL(t *testing.T) {
	s := newTestServer()
	// TTL field should override record TTL for expiry calculation
	_, err := s.processFstrmFrame(&FstrmFrame{
		Op:      "add",
		TTL:     10,
		Records: []string{"ttl.example.com. 300 IN A 1.2.3.4"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.buffer.Get("ttl.example.com.", dns.TypeA) == nil {
		t.Error("record not found in buffer")
	}
}

func TestProcessFstrmFrame_AddWithExpiry(t *testing.T) {
	s := newTestServer()
	future := time.Now().Add(1 * time.Hour).Unix()
	_, err := s.processFstrmFrame(&FstrmFrame{
		Op:      "add",
		Expiry:  future,
		Records: []string{"exp.example.com. 60 IN A 1.2.3.4"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.buffer.Get("exp.example.com.", dns.TypeA) == nil {
		t.Error("record not found in buffer")
	}
}

func TestProcessFstrmFrame_Delete(t *testing.T) {
	s := newTestServer()

	// Add first
	s.processFstrmFrame(&FstrmFrame{
		Op:      "add",
		Records: []string{"del.example.com. 60 IN A 10.0.0.1"},
	})

	msg, err := s.processFstrmFrame(&FstrmFrame{
		Op:      "delete",
		Records: []string{"del.example.com. 60 IN A 10.0.0.1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == "" {
		t.Error("expected non-empty success message")
	}
	if s.buffer.Get("del.example.com.", dns.TypeA) != nil {
		t.Error("record still present after delete")
	}
}

func TestProcessFstrmFrame_EmptyRecords(t *testing.T) {
	s := newTestServer()
	_, err := s.processFstrmFrame(&FstrmFrame{Op: "add", Records: nil})
	if err == nil {
		t.Error("expected error for empty records, got nil")
	}
}

func TestProcessFstrmFrame_InvalidRecord(t *testing.T) {
	s := newTestServer()
	_, err := s.processFstrmFrame(&FstrmFrame{
		Op:      "add",
		Records: []string{"not a valid dns record"},
	})
	if err == nil {
		t.Error("expected error for invalid record, got nil")
	}
}

func TestProcessFstrmFrame_MixedQNameQType(t *testing.T) {
	tests := []struct {
		name    string
		records []string
	}{
		{
			name:    "mixed qname",
			records: []string{"a.example.com. 60 IN A 1.2.3.4", "b.example.com. 60 IN A 5.6.7.8"},
		},
		{
			name:    "mixed qtype",
			records: []string{"x.example.com. 60 IN A 1.2.3.4", "x.example.com. 60 IN AAAA ::1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer()
			_, err := s.processFstrmFrame(&FstrmFrame{Op: "add", Records: tc.records})
			if err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}

func TestProcessFstrmFrame_UnknownOp(t *testing.T) {
	s := newTestServer()
	_, err := s.processFstrmFrame(&FstrmFrame{
		Op:      "upsert",
		Records: []string{"x.example.com. 60 IN A 1.2.3.4"},
	})
	if err == nil {
		t.Error("expected error for unknown op, got nil")
	}
}

// --- handleFstrmConn integration test ---

// readACKLine reads one newline-terminated JSON line from r one byte at a time,
// avoiding any read-ahead buffering that would interfere with the FrameStreams
// CONTROL_FINISH frame read by Writer.Close().
func readACKLine(t *testing.T, r io.Reader) APIResponse {
	t.Helper()
	var line []byte
	buf := make([]byte, 1)
	for {
		if _, err := r.Read(buf); err != nil {
			t.Fatalf("read ack byte: %v", err)
		}
		line = append(line, buf[0])
		if buf[0] == '\n' {
			break
		}
	}
	var resp APIResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal ack %q: %v", line, err)
	}
	return resp
}

func TestHandleFstrmConn_AckNack(t *testing.T) {
	s := newTestServer()
	serverConn, clientConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleFstrmConn(serverConn)
	}()

	// FrameStreams writer performs READY → ACCEPT → START handshake.
	w, err := framestream.NewWriter(clientConn, &framestream.WriterOptions{
		ContentTypes:  [][]byte{[]byte(FstrmContentType)},
		Bidirectional: true,
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	writeFrame := func(frame []byte) {
		t.Helper()
		if _, err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
		// WriteFrame uses an internal bufio.Writer; Flush is required to
		// actually send bytes to the server.
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush: %v", err)
		}
	}

	// Frame 1: valid add — expect ACK with ok:true.
	frame1, _ := json.Marshal(FstrmFrame{
		Op:      "add",
		Records: []string{"conn.example.com. 60 IN A 192.0.2.1"},
	})
	writeFrame(frame1)
	ack1 := readACKLine(t, clientConn)
	if !ack1.OK {
		t.Errorf("frame 1: expected ok:true, got error: %s", ack1.Error)
	}
	if ack1.Message == "" {
		t.Error("frame 1: expected non-empty message")
	}

	// Frame 2: invalid JSON — expect NACK with ok:false.
	writeFrame([]byte("not-json"))
	ack2 := readACKLine(t, clientConn)
	if ack2.OK {
		t.Error("frame 2: expected ok:false for bad JSON")
	}
	if ack2.Error == "" {
		t.Error("frame 2: expected non-empty error field")
	}

	// Frame 3: unknown op — expect NACK.
	frame3, _ := json.Marshal(FstrmFrame{
		Op:      "unknown",
		Records: []string{"conn.example.com. 60 IN A 192.0.2.1"},
	})
	writeFrame(frame3)
	ack3 := readACKLine(t, clientConn)
	if ack3.OK {
		t.Error("frame 3: expected ok:false for unknown op")
	}

	// Verify the valid record was actually buffered.
	if recs := s.buffer.Get("conn.example.com.", dns.TypeA); len(recs) != 1 {
		t.Errorf("buffer: got %d records for conn.example.com./A, want 1", len(recs))
	}

	// Close writer: sends CONTROL_STOP, reads CONTROL_FINISH.
	if err := w.Close(); err != nil {
		t.Errorf("Writer.Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("handleFstrmConn did not exit after STOP")
	}
}

func TestHandleFstrmConn_WrongContentType(t *testing.T) {
	s := newTestServer()
	serverConn, clientConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleFstrmConn(serverConn)
	}()

	// Writer offers a content type the server does not accept.
	_, err := framestream.NewWriter(clientConn, &framestream.WriterOptions{
		ContentTypes:  [][]byte{[]byte("application/x-wrong")},
		Bidirectional: true,
	})
	// Handshake should fail (ErrContentTypeMismatch) and server should close.
	if err == nil {
		t.Error("expected handshake error for wrong content type")
	}
	clientConn.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("handleFstrmConn did not exit after failed handshake")
	}
}
