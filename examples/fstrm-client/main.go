// fstrm-client is an example client for the dynamicrecords CoreDNS plugin's
// FrameStreams ingestion endpoint.
//
// Usage:
//
//	fstrm-client -addr localhost:8054 -cert client.crt -key client.key -ca ca.crt
//
// The binary demonstrates a complete session: connect with mTLS, send an "add"
// batch, receive per-frame ACK/NACK responses, send a "delete", then close
// cleanly with the FrameStreams STOP/FINISH handshake.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	framestream "github.com/farsightsec/golang-framestream"

	"github.com/endreszabo/coredns_dynamicrecords/protocol"
)

func main() {
	addr := flag.String("addr", "localhost:8054", "FrameStreams server address")
	certFile := flag.String("cert", "", "client certificate (PEM)")
	keyFile := flag.String("key", "", "client private key (PEM)")
	caFile := flag.String("ca", "", "CA certificate for server verification (PEM)")
	flag.Parse()

	if *certFile == "" || *keyFile == "" || *caFile == "" {
		fmt.Fprintln(os.Stderr, "error: -cert, -key, and -ca are required")
		flag.Usage()
		os.Exit(1)
	}

	tlsCfg, err := buildTLSConfig(*certFile, *keyFile, *caFile)
	if err != nil {
		log.Fatalf("TLS config: %v", err)
	}

	conn, err := tls.Dial("tcp", *addr, tlsCfg)
	if err != nil {
		log.Fatalf("dial %s: %v", *addr, err)
	}
	defer conn.Close()
	log.Printf("connected to %s (ALPN: %s)", *addr, conn.ConnectionState().NegotiatedProtocol)

	// Perform the FrameStreams bidirectional handshake:
	//   client → CONTROL_READY {content-type}
	//   server → CONTROL_ACCEPT {content-type}
	//   client → CONTROL_START
	w, err := framestream.NewWriter(conn, &framestream.WriterOptions{
		ContentTypes:  [][]byte{[]byte(protocol.FstrmContentType)},
		Bidirectional: true,
		Timeout:       10 * time.Second,
	})
	if err != nil {
		log.Fatalf("FrameStreams handshake: %v", err)
	}
	log.Printf("handshake complete (content-type: %s)", w.ContentType())

	// conn is used directly as the ACK reader.  readACK reads one byte at a
	// time, so it never consumes the CONTROL_FINISH bytes that w.Close() needs
	// to read from the same connection after CONTROL_STOP is sent.
	ackReader := conn

	// --- Example 1: add a batch of A records ---
	send(w, ackReader, protocol.FstrmFrame{
		Op: "add",
		Records: []string{
			"svc.example.com. 60 IN A 10.0.0.1",
			"svc.example.com. 60 IN A 10.0.0.2",
		},
	})

	// --- Example 2: add a record with an explicit expiry ---
	send(w, ackReader, protocol.FstrmFrame{
		Op:     "add",
		Expiry: time.Now().Add(5 * time.Minute).Unix(),
		Records: []string{
			"tmp.example.com. 60 IN A 192.0.2.99",
		},
	})

	// --- Example 3: demonstrate a NACK (bad record string) ---
	send(w, ackReader, protocol.FstrmFrame{
		Op:      "add",
		Records: []string{"this is not a valid dns record"},
	})

	// --- Example 4: delete a record ---
	send(w, ackReader, protocol.FstrmFrame{
		Op:      "delete",
		Records: []string{"svc.example.com. 60 IN A 10.0.0.1"},
	})

	// Send CONTROL_STOP and wait for the server's CONTROL_FINISH.
	if err := w.Close(); err != nil {
		log.Fatalf("close: %v", err)
	}
	log.Println("session closed cleanly")
}

// send marshals a frame, writes it (flushing immediately), then reads and
// prints the server's ACK/NACK response.
func send(w *framestream.Writer, ackReader io.Reader, frame protocol.FstrmFrame) {
	data, err := json.Marshal(frame)
	if err != nil {
		log.Fatalf("marshal frame: %v", err)
	}

	if _, err := w.WriteFrame(data); err != nil {
		log.Fatalf("WriteFrame: %v", err)
	}
	// WriteFrame uses an internal bufio.Writer; Flush is required to actually
	// push the bytes onto the network.
	if err := w.Flush(); err != nil {
		log.Fatalf("Flush: %v", err)
	}

	ack, err := readACK(ackReader)
	if err != nil {
		log.Fatalf("read ACK: %v", err)
	}

	if ack.OK {
		log.Printf("ACK  op=%-6s %s", frame.Op, ack.Message)
	} else {
		log.Printf("NACK op=%-6s %s", frame.Op, ack.Error)
	}
}

// readACK reads one newline-terminated JSON line from r and decodes it as an
// APIResponse.  Reading one byte at a time prevents the internal bufio buffer
// from consuming the CONTROL_FINISH frame that the FrameStreams Writer reads
// when w.Close() is called.
func readACK(r io.Reader) (protocol.APIResponse, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		if _, err := r.Read(buf); err != nil {
			return protocol.APIResponse{}, fmt.Errorf("read: %w", err)
		}
		line = append(line, buf[0])
		if buf[0] == '\n' {
			break
		}
	}
	var resp protocol.APIResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return protocol.APIResponse{}, fmt.Errorf("unmarshal %q: %w", line, err)
	}
	return resp, nil
}

// buildTLSConfig constructs a mTLS configuration that:
//   - presents the client certificate to the server
//   - verifies the server certificate against the provided CA
//   - requires ALPN "fstrm" (the dynamicrecords FrameStreams listener advertises this)
func buildTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse CA certificate")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		// NextProtos is the ALPN list.  The dynamicrecords FrameStreams listener
		// only accepts connections that negotiate protocol.FstrmALPN ("fstrm").
		// This is distinct from the FrameStreams content-type, which is
		// negotiated at the application layer inside the framing handshake.
		NextProtos: []string{protocol.FstrmALPN},
		MinVersion: tls.VersionTLS13,
	}, nil
}
