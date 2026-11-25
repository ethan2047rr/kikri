package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ethan2047rr/kikri/internal/appdir"
	"github.com/ethan2047rr/kikri/internal/detect"
	"github.com/ethan2047rr/kikri/internal/schedule"
	"github.com/ethan2047rr/kikri/internal/scheduler"
	"github.com/ethan2047rr/kikri/internal/store"
	"github.com/ethan2047rr/kikri/internal/wake"
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

	Plans      []scheduler.PlanStatus `json:"plans,omitempty"`
	StoreError string                 `json:"storeError,omitempty"`
}

// Service is bound to the frontend; its exported methods are callable from
// TypeScript as "github.com/ethan2047rr/kikri/internal/app.Service.<Method>".
type Service struct {
	version string
	emit    func(Snapshot)

	mu       sync.Mutex
	snap     Snapshot
	inflight chan struct{} // non-nil while a detection pass runs

	store  *store.Store
	engine *scheduler.Engine // nil when the data directory or database failed
}

// NewService creates the service. emit may be nil.
func NewService(version string, emit func(Snapshot)) *Service {
	return &Service{version: version, emit: emit, snap: Snapshot{Version: version}}
}

// Prepare creates the data directory and sandbox and opens the database.
// It is called once at startup.
func (s *Service) Prepare() {
	layout, err := appdir.Ensure()
	s.mu.Lock()
	s.snap.AppDir = layout
	if err != nil {
		s.snap.AppDirError = err.Error()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	st, err := store.Open(layout.Database)
	if err != nil {
		s.mu.Lock()
		s.snap.StoreError = err.Error()
		s.mu.Unlock()
		return
	}
	s.store = st
	s.engine = newEngine(layout, st, s.plansChanged)
	s.plansChanged()
}

// Close releases the database.
func (s *Service) Close() {
	if s.store != nil {
		s.store.Close()
	}
}

// StartScheduler runs the scheduler until ctx is done.
func (s *Service) StartScheduler(ctx context.Context) {
	if s.engine != nil {
		go s.engine.Run(ctx)
	}
}

// Kick asks the scheduler for an immediate pass (after sleep, for example).
func (s *Service) Kick() {
	if s.engine != nil {
		s.engine.Kick()
	}
}

func (s *Service) plansChanged() {
	if s.engine == nil {
		return
	}
	plans, err := s.engine.Status()
	s.mu.Lock()
	if err == nil {
		s.snap.Plans = plans
	}
	snap := s.snap
	s.mu.Unlock()
	s.publish(snap)
}

var errNoEngine = errors.New("scheduler unavailable: the data directory or database could not be opened")

// WakeNow wakes a plan right away, without Smart Skip ("立即唤醒").
func (s *Service) WakeNow(ctx context.Context, planID string) (wake.Result, error) {
	if s.engine == nil {
		return wake.Result{}, errNoEngine
	}
	// A wake must finish even if the popup that asked for it closes.
	return s.engine.WakeNow(context.WithoutCancel(ctx), planID)
}

// SetPlan stores one plan's schedule.
func (s *Service) SetPlan(planID string, plan schedule.Plan) error {
	if s.engine == nil {
		return errNoEngine
	}
	return s.engine.SetPlan(planID, plan)
}

// PauseToday skips (or, with false, restores) the rest of today's wakes.
func (s *Service) PauseToday(pause bool) error {
	if s.engine == nil {
		return errNoEngine
	}
	return s.engine.PauseToday(pause)
}

// Log returns the newest wake log entries (all plans when planID is empty).
func (s *Service) Log(planID string, limit int) ([]store.Entry, error) {
	if s.store == nil {
		return nil, errNoEngine
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.store.Entries(planID, limit)
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
