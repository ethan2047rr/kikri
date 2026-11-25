package app

import (
	"context"
	"sync"
	"time"

	"github.com/ethan2047rr/kikri/internal/appdir"
	"github.com/ethan2047rr/kikri/internal/detect"
)

// EventSnapshot is emitted to the frontend whenever a new Snapshot is ready.
const EventSnapshot = "kikri:snapshot"

// Snapshot is everything the M1 popup shows.
type Snapshot struct {
	Version      string        `json:"version"`
	AppDir       appdir.Layout `json:"appDir"`
	AppDirError  string        `json:"appDirError,omitempty"`
	Detecting    bool          `json:"detecting"`
	Report       detect.Report `json:"report"`
	DetectedOnce bool          `json:"detectedOnce"`
}

// Service is bound to the frontend; its exported methods are callable from
// TypeScript as "github.com/ethan2047rr/kikri/internal/app.Service.<Method>".
type Service struct {
	version string
	emit    func(Snapshot)

	mu       sync.Mutex
	snap     Snapshot
	inflight chan struct{} // non-nil while a detection pass runs
}

// NewService creates the service. emit may be nil.
func NewService(version string, emit func(Snapshot)) *Service {
	return &Service{version: version, emit: emit, snap: Snapshot{Version: version}}
}

// Prepare creates the data directory and sandbox. It is called once at startup.
func (s *Service) Prepare() {
	layout, err := appdir.Ensure()
	s.mu.Lock()
	s.snap.AppDir = layout
	if err != nil {
		s.snap.AppDirError = err.Error()
	}
	s.mu.Unlock()
}

// Snapshot returns the latest state without running detection.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

// Refresh runs a detection pass (joining one already in flight) and returns
// the resulting state.
func (s *Service) Refresh(ctx context.Context) Snapshot {
	s.mu.Lock()
	if ch := s.inflight; ch != nil {
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
		}
		return s.Snapshot()
	}
	ch := make(chan struct{})
	s.inflight = ch
	s.snap.Detecting = true
	dir := s.snap.AppDir.Sandbox
	if s.snap.AppDirError != "" {
		dir = "" // never run a CLI inside a sandbox that failed validation
	}
	pending := s.snap
	s.mu.Unlock()
	s.publish(pending)

	// Detection must not be cut short by the caller going away (e.g. the
	// popup closing); it has its own per-command timeouts.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	report := detect.Run(runCtx, detect.Options{Dir: dir})
	cancel()

	s.mu.Lock()
	s.snap.Report = report
	s.snap.Detecting = false
	s.snap.DetectedOnce = true
	s.inflight = nil
	done := s.snap
	s.mu.Unlock()
	close(ch)
	s.publish(done)
	return done
}

func (s *Service) publish(snap Snapshot) {
	if s.emit != nil {
		s.emit(snap)
	}
}
