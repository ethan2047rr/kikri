// Package store keeps Kikri's state in one SQLite file (D10): settings, the
// state of every scheduled slot, and the wake log. Slot state is keyed by
// (plan, slot time), which is what makes the scheduler idempotent across
// restarts, sleep and system clock changes.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo
)

// SlotState is the lifecycle of one scheduled slot.
type SlotState string

const (
	SlotPending    SlotState = "pending"    // waiting for a retry (offline, network error, busy)
	SlotDone       SlotState = "done"       // wake succeeded
	SlotSkipped    SlotState = "skipped"    // Smart Skip: a window was already running
	SlotFailed     SlotState = "failed"     // gave up (not logged in, quota, retries used up, ...)
	SlotExpired    SlotState = "expired"    // missed and past its make-up deadline
	SlotSuperseded SlotState = "superseded" // several slots were missed; a later one ran instead
	SlotPaused     SlotState = "paused"     // "pause today" was on
)

// Final reports whether the slot needs no more work.
func (s SlotState) Final() bool { return s != SlotPending }

// Slot is the stored state of one slot.
type Slot struct {
	Plan     string    `json:"plan"`
	At       time.Time `json:"at"`
	State    SlotState `json:"state"`
	Attempts int       `json:"attempts"`
	NextTry  time.Time `json:"nextTry,omitempty"`
}

// Entry is one row of the wake log.
type Entry struct {
	ID        int64           `json:"id"`
	Plan      string          `json:"plan"`
	SlotAt    *time.Time      `json:"slotAt,omitempty"` // nil for manual wakes
	Trigger   string          `json:"trigger"`          // scheduled, makeup, retry, manual
	StartedAt time.Time       `json:"startedAt"`
	Outcome   string          `json:"outcome"` // wake.Outcome, or expired / paused
	Result    json.RawMessage `json:"result,omitempty"`
}

// Store is safe for concurrent use.
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS slots (
	plan     TEXT    NOT NULL,
	slot_at  INTEGER NOT NULL,
	state    TEXT    NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	next_try INTEGER NOT NULL DEFAULT 0,
	updated  INTEGER NOT NULL,
	PRIMARY KEY (plan, slot_at)
);
CREATE TABLE IF NOT EXISTS wakes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	plan       TEXT    NOT NULL,
	slot_at    INTEGER,
	trigger    TEXT    NOT NULL,
	started_at INTEGER NOT NULL,
	outcome    TEXT    NOT NULL,
	result     TEXT
);
CREATE INDEX IF NOT EXISTS wakes_plan_started ON wakes (plan, started_at);
`

// Open opens (creating if needed) the database at path.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; keeps SQLite simple and avoids SQLITE_BUSY
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// GetSetting decodes the JSON value stored under key into v. ok is false
// when the key is not set (v is left untouched).
func (s *Store) GetSetting(key string, v any) (ok bool, err error) {
	var raw string
	err = s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

// PutSetting stores v as JSON under key.
func (s *Store) PutSetting(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, string(b))
	return err
}

// Slots returns the stored slots of plan at or after from, keyed by Unix time.
func (s *Store) Slots(plan string, from time.Time) (map[int64]Slot, error) {
	rows, err := s.db.Query(`SELECT slot_at, state, attempts, next_try FROM slots
		WHERE plan = ? AND slot_at >= ?`, plan, from.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Slot{}
	for rows.Next() {
		var at, next int64
		var sl Slot
		if err := rows.Scan(&at, &sl.State, &sl.Attempts, &next); err != nil {
			return nil, err
		}
		sl.Plan, sl.At = plan, time.Unix(at, 0)
		if next > 0 {
			sl.NextTry = time.Unix(next, 0)
		}
		out[at] = sl
	}
	return out, rows.Err()
}

// PutSlot writes a slot's state.
func (s *Store) PutSlot(sl Slot, now time.Time) error {
	var next int64
	if !sl.NextTry.IsZero() {
		next = sl.NextTry.Unix()
	}
	_, err := s.db.Exec(`INSERT INTO slots (plan, slot_at, state, attempts, next_try, updated)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (plan, slot_at) DO UPDATE SET
			state = excluded.state, attempts = excluded.attempts,
			next_try = excluded.next_try, updated = excluded.updated`,
		sl.Plan, sl.At.Unix(), string(sl.State), sl.Attempts, next, now.Unix())
	return err
}

// AddEntry appends to the wake log and returns the row ID.
func (s *Store) AddEntry(e Entry) (int64, error) {
	var slot any
	if e.SlotAt != nil {
		slot = e.SlotAt.Unix()
	}
	var result any
	if len(e.Result) > 0 {
		result = string(e.Result)
	}
	res, err := s.db.Exec(`INSERT INTO wakes (plan, slot_at, trigger, started_at, outcome, result)
		VALUES (?, ?, ?, ?, ?, ?)`, e.Plan, slot, e.Trigger, e.StartedAt.Unix(), e.Outcome, result)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Entries returns up to limit log rows, newest first. An empty plan means all.
func (s *Store) Entries(plan string, limit int) ([]Entry, error) {
	q := `SELECT id, plan, slot_at, trigger, started_at, outcome, result FROM wakes`
	args := []any{}
	if plan != "" {
		q += ` WHERE plan = ?`
		args = append(args, plan)
	}
	q += ` ORDER BY started_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	return s.queryEntries(q, args...)
}

// LastEntry returns the newest log row of plan whose outcome is one of
// outcomes (any outcome when none are given).
func (s *Store) LastEntry(plan string, outcomes ...string) (*Entry, error) {
	q := `SELECT id, plan, slot_at, trigger, started_at, outcome, result FROM wakes WHERE plan = ?`
	args := []any{plan}
	if len(outcomes) > 0 {
		q += ` AND outcome IN (?` + repeat(",?", len(outcomes)-1) + `)`
		for _, o := range outcomes {
			args = append(args, o)
		}
	}
	q += ` ORDER BY started_at DESC, id DESC LIMIT 1`
	es, err := s.queryEntries(q, args...)
	if err != nil || len(es) == 0 {
		return nil, err
	}
	return &es[0], nil
}

// CountEntries counts log rows with the given outcome since t (all plans).
func (s *Store) CountEntries(outcome string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM wakes WHERE outcome = ? AND started_at >= ?`, outcome, since.Unix()).Scan(&n)
	return n, err
}

func (s *Store) queryEntries(q string, args ...any) ([]Entry, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var slot sql.NullInt64
		var started int64
		var result sql.NullString
		if err := rows.Scan(&e.ID, &e.Plan, &slot, &e.Trigger, &started, &e.Outcome, &result); err != nil {
			return nil, err
		}
		e.StartedAt = time.Unix(started, 0)
		if slot.Valid {
			t := time.Unix(slot.Int64, 0)
			e.SlotAt = &t
		}
		if result.Valid {
			e.Result = json.RawMessage(result.String)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
