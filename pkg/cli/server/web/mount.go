// Plan 07.3-04: Mount() wires the dashboard's three routes onto the
// shared http.ServeMux, starts the broadcaster + poller goroutines,
// and returns a MountResult the caller can drain on shutdown.
package web

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/deliveries"
	"github.com/mikelalcon/skytime/pkg/cli/server/web/events"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// MountOptions configures Mount. All fields except Logger and
// PollInterval are required; Logger defaults to slog.Default(),
// PollInterval defaults to 2 * time.Second.
type MountOptions struct {
	Client                 client.Client
	TaskQueue              string
	Registry               *interpreter.FlowRegistry
	Buffer                 *deliveries.RingBuffer
	Addr                   string // listener address — used to build the same-origin allowed Origin
	TemporalWebUI          string
	Logger                 *slog.Logger
	Namespace              string
	PollInterval           time.Duration
	ReplayHistoryThreshold int64
}

// MountResult is what Mount returns to the caller in pkg/cli/server.go.
//
// B3 (Phase 7.3 checker): callers MUST invoke Shutdown() BEFORE
// srv.Shutdown(drainCtx) so the broadcaster publishes its final
// "event: shutdown" SSE frame WHILE the HTTP listener is still
// accepting writes from in-flight SSE handlers. Calling
// srv.Shutdown first would cancel SSE request contexts and the
// handlers would exit without seeing the shutdown channel close.
//
// OnDelivery is the post-append callback the caller wires into
// receiver.Deps so each successful or failed delivery appends to
// the ring buffer AND fires a "delivery_received" SSE event.
type MountResult struct {
	Broadcaster *events.Broadcaster
	Poller      *events.Poller
	Shutdown    func()
	OnDelivery  func(deliveries.Delivery)
}

// Mount wires the dashboard's three routes onto mux, starts the
// broadcaster + poller goroutines, and returns a MountResult the
// caller can plug into receiver.Deps + drain on shutdown.
//
// The poller goroutine is owned by the returned Shutdown func: the
// caller must call Shutdown during drain — see B3 note on MountResult
// for the ordering constraint.
func Mount(ctx context.Context, mux *http.ServeMux, opts MountOptions) *MountResult {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 2 * time.Second
	}

	// Forward declaration: broadcaster needs poller; poller needs
	// broadcaster. Solution: build broadcaster first with a closure
	// that reads through a *Poller indirection set after poller
	// construction.
	var pollerRef *events.Poller
	snapshotFn := func() events.Snapshot {
		s := events.Snapshot{}
		if pollerRef != nil {
			s.Workflows = pollerRef.CurrentSnapshot()
		}
		if opts.Buffer != nil {
			s.Deliveries = opts.Buffer.Snapshot(deliveries.DefaultCap)
		}
		return s
	}
	b := events.NewBroadcaster(snapshotFn)
	pollerRef = events.NewPoller(opts.Client, b, events.PollerConfig{
		Namespace:              opts.Namespace,
		PollInterval:           opts.PollInterval,
		MaxPageSize:            100,
		ReplayHistoryThreshold: opts.ReplayHistoryThreshold,
	}, opts.Logger)

	// Run poller in a goroutine; its lifecycle is owned by a derived
	// context cancelled via the returned Shutdown closure.
	pollerCtx, cancelPoller := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pollerRef.Run(pollerCtx)
	}()

	handlers := NewHandlers(Options{
		Client:        opts.Client,
		TaskQueue:     opts.TaskQueue,
		Registry:      opts.Registry,
		Broadcaster:   b,
		Buffer:        opts.Buffer,
		Logger:        opts.Logger,
		AllowedOrigin: sanitizeOriginFromAddr(opts.Addr),
		TemporalWebUI: opts.TemporalWebUI,
		WorkflowsFn:   pollerRef.CurrentSnapshot,
	})

	mux.HandleFunc("/", handlers.dashboardHandler)
	mux.HandleFunc("/api/events", handlers.sseHandler)
	mux.HandleFunc("/api/trigger", handlers.triggerHandler)

	// OnDelivery callback that the caller wires into receiver.Deps.
	// Publishes a delivery_received event so all subscribers see the
	// new row immediately. Receiver appends to the buffer first
	// (Plan 02), so by the time this fires the buffer already
	// contains the delivery.
	onDelivery := func(d deliveries.Delivery) {
		b.Publish(events.Event{Name: "delivery_received", Payload: d})
	}

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			cancelPoller()
			b.Shutdown()
			wg.Wait()
		})
	}

	return &MountResult{
		Broadcaster: b,
		Poller:      pollerRef,
		Shutdown:    shutdown,
		OnDelivery:  onDelivery,
	}
}
