package snippets

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"go.temporal.io/sdk/client"
)

// MTLSReloader holds a live Temporal client whose mTLS configuration is
// rebuilt from the on-disk cert/key/CA paths on every SIGHUP. Production
// cert-rotation tools (cert-manager, vault-agent, your own cron job) drop
// new files at the same paths and signal the worker; the next gRPC
// connection uses the freshly-loaded chain. Existing in-flight RPCs
// continue against the old client until they complete; the old client
// closes after the swap.
//
// Concurrency: Current() returns the latest *client.Client without
// contention via atomic.Pointer. Reload runs under a mutex so two
// back-to-back SIGHUPs serialize instead of racing on file reads.
type MTLSReloader struct {
	hostPort   string
	namespace  string
	certFile   string
	keyFile    string
	caFile     string
	serverName string

	mu      sync.Mutex
	current atomic.Pointer[client.Client]
}

// newMTLSReloader builds the initial client and starts a goroutine that
// re-dials on SIGHUP. The returned reloader's Current() method always
// yields the most recent client; callers use it just-in-time for every
// RPC (do NOT cache the *client.Client across RPCs — that's exactly the
// staleness this whole machinery prevents).
//
// The caller owns the lifecycle: call reloader.Close() at process
// shutdown to close the current client and stop the signal goroutine.
func newMTLSReloader(hostPort, namespace, certFile, keyFile, caFile, serverName string) (*MTLSReloader, error) {
	r := &MTLSReloader{
		hostPort:   hostPort,
		namespace:  namespace,
		certFile:   certFile,
		keyFile:    keyFile,
		caFile:     caFile,
		serverName: serverName,
	}
	if err := r.reload(); err != nil {
		return nil, fmt.Errorf("mtls: initial dial: %w", err)
	}

	go r.signalLoop()
	return r, nil
}

// Current returns the most recent Temporal client. Always call this
// just-in-time for each RPC; never cache the returned value across RPCs.
func (r *MTLSReloader) Current() client.Client {
	return *r.current.Load()
}

// Close shuts down the current client. Production callers should also
// signal.Stop the SIGHUP channel; for snippet brevity that wiring lives
// in the caller-site example in temporal-auth.md.
func (r *MTLSReloader) Close() {
	if c := r.current.Load(); c != nil {
		(*c).Close()
	}
}

func (r *MTLSReloader) signalLoop() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	for range ch {
		if err := r.reload(); err != nil {
			// Production: log via slog. Snippet keeps it stderr to avoid
			// a logger dependency.
			fmt.Fprintf(os.Stderr, "mtls: reload on SIGHUP failed: %v\n", err)
		}
	}
}

func (r *MTLSReloader) reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("mtls: load cert/key (%s, %s): %w", r.certFile, r.keyFile, err)
	}
	caPEM, err := os.ReadFile(r.caFile)
	if err != nil {
		return fmt.Errorf("mtls: read CA (%s): %w", r.caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("mtls: no PEM blocks parsed from CA file %s", r.caFile)
	}

	c, err := client.Dial(client.Options{
		HostPort:  r.hostPort,
		Namespace: r.namespace,
		ConnectionOptions: client.ConnectionOptions{
			TLS: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      pool,
				ServerName:   r.serverName,
				MinVersion:   tls.VersionTLS12,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("mtls: dial %s: %w", r.hostPort, err)
	}

	old := r.current.Swap(&c)
	if old != nil {
		(*old).Close()
	}
	return nil
}
