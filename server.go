package dynamicrecords

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/endreszabo/coredns_dynamicrecords/protocol"
)

// SharedServer is a singleton HTTP server shared across all plugin instances
type SharedServer struct {
	mu              sync.Mutex
	server          *http.Server
	buffer          *RRBuffer
	instances       int
	httpAddr        string
	fstrmAddr       string
	tlsConfig       *tls.Config
	started         bool
	fstrmListener   net.Listener
	defaultTTL      uint32
	cleanupInterval time.Duration
}

var (
	sharedServerMu sync.Mutex
	sharedServer   *SharedServer
)

// GetOrCreateSharedServer returns the singleton shared server.
// fstrmAddr may be empty to disable the FrameStreams listener.
// cleanupInterval sets how often expired batches are pruned; 0 uses the default (60s).
func GetOrCreateSharedServer(httpAddr, fstrmAddr string, certFile, keyFile, caFile string, defaultTTL uint32, cleanupInterval time.Duration) (*SharedServer, error) {
	sharedServerMu.Lock()
	defer sharedServerMu.Unlock()

	// If server exists, validate configuration matches
	if sharedServer != nil {
		if sharedServer.httpAddr != httpAddr {
			return nil, fmt.Errorf("shared server already configured with different address: %s (requested: %s)",
				sharedServer.httpAddr, httpAddr)
		}
		sharedServer.instances++
		return sharedServer, nil
	}

	// Create TLS config
	tlsConfig, err := createTLSConfig(certFile, keyFile, caFile)
	if err != nil {
		return nil, err
	}

	// Create new shared server
	sharedServer = &SharedServer{
		buffer:          NewRRBuffer(cleanupInterval),
		httpAddr:        httpAddr,
		fstrmAddr:       fstrmAddr,
		tlsConfig:       tlsConfig,
		instances:       1,
		defaultTTL:      defaultTTL,
		cleanupInterval: cleanupInterval,
	}

	return sharedServer, nil
}

// Start starts the shared HTTP server (and optional FrameStreams listener) if
// not already started.
func (s *SharedServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/records", s.handleRecords)
	mux.HandleFunc("/records/delete", s.handleDelete)
	mux.HandleFunc("/health", s.handleHealth)

	// Clone TLS config and set ALPN for HTTP.
	httpTLS := s.tlsConfig.Clone()
	httpTLS.NextProtos = []string{"h2", "http/1.1"}

	s.server = &http.Server{
		Addr:      s.httpAddr,
		Handler:   mux,
		TLSConfig: httpTLS,
	}

	go func() {
		if err := s.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			fmt.Printf("dynamicrecords: HTTP server error: %v\n", err)
		}
	}()

	// Start FrameStreams listener if configured.
	if s.fstrmAddr != "" {
		fstrmTLS := s.tlsConfig.Clone()
		fstrmTLS.NextProtos = []string{protocol.FstrmALPN}

		ln, err := tls.Listen("tcp", s.fstrmAddr, fstrmTLS)
		if err != nil {
			return fmt.Errorf("dynamicrecords: fstrm listener on %s: %v", s.fstrmAddr, err)
		}
		s.fstrmListener = ln
		go s.acceptFstrmConns(ln)
	}

	s.started = true
	return nil
}

// Unregister decrements the instance counter and stops the server if this was
// the last instance.
func (s *SharedServer) Unregister() error {
	sharedServerMu.Lock()
	defer sharedServerMu.Unlock()

	if sharedServer == nil {
		return nil
	}

	sharedServer.instances--

	// Stop the server if no more instances
	if sharedServer.instances <= 0 {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Close FrameStreams listener first; this unblocks acceptFstrmConns.
		if s.fstrmListener != nil {
			s.fstrmListener.Close()
			s.fstrmListener = nil
		}

		if s.server != nil && s.started {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := s.server.Shutdown(ctx)
			s.started = false
			sharedServer = nil
			return err
		}
	}

	return nil
}

// createTLSConfig creates a base TLS configuration for mTLS.
// NextProtos is intentionally left unset so callers can set it per-listener.
func createTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	// Load server certificate
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %v", err)
	}

	// Load CA certificate for client verification
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %v", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS13,
	}

	return tlsConfig, nil
}
