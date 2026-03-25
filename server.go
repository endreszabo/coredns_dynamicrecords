package dynamicrecords

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// SharedServer is a singleton HTTP server shared across all plugin instances
type SharedServer struct {
	mu         sync.Mutex
	server     *http.Server
	buffer     *RRBuffer
	instances  int
	httpAddr   string
	tlsConfig  *tls.Config
	started    bool
	defaultTTL uint32
}

var (
	sharedServerMu sync.Mutex
	sharedServer   *SharedServer
)

// GetOrCreateSharedServer returns the singleton shared server
func GetOrCreateSharedServer(httpAddr string, certFile, keyFile, caFile string, defaultTTL uint32) (*SharedServer, error) {
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
		buffer:     NewRRBuffer(),
		httpAddr:   httpAddr,
		tlsConfig:  tlsConfig,
		instances:  1,
		defaultTTL: defaultTTL,
	}

	return sharedServer, nil
}

// Start starts the shared HTTP server if not already started
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

	s.server = &http.Server{
		Addr:      s.httpAddr,
		Handler:   mux,
		TLSConfig: s.tlsConfig,
	}

	go func() {
		if err := s.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Shared HTTP server error: %v\n", err)
		}
	}()

	s.started = true
	return nil
}

// Unregister decrements the instance counter and stops the server if this was the last instance
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

// createTLSConfig creates a TLS configuration for mTLS
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
