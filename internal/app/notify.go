package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/ethan2047rr/kikri/internal/scheduler"
	"github.com/ethan2047rr/kikri/internal/store"
	"github.com/ethan2047rr/kikri/internal/wake"
)

// notifier wraps the Wails notifications service. It is started by hand
// rather than registered as a Wails service: on macOS it needs a bundle ID,
// so it fails when the binary runs outside Kikri.app, and a failing service
// would stop the whole app. Notifications are then simply unavailable.
type notifier struct {
	mu  sync.Mutex
	ns  *notifications.NotificationService
	err string // why notifications are unavailable
}

func (n *notifier) start() {
	ns := notifications.New()
	if err := ns.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		slog.Info("notifications unavailable", "err", err)
		n.mu.Lock()
		n.err = err.Error()
		n.mu.Unlock()
		return
	}
	n.mu.Lock()
	n.ns = ns
	n.mu.Unlock()
}

// unavailable returns why notifications cannot be sent ("" when they can).
func (n *notifier) unavailable() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ns == nil && n.err == "" {
		return "not started"
	}
	return n.err
}

// authorize asks the system for permission (macOS shows its prompt once).
func (n *notifier) authorize() {
	n.mu.Lock()
	ns := n.ns
	n.mu.Unlock()
	if ns == nil {
		return
	}
	if ok, err := ns.CheckNotificationAuthorization(); err == nil && ok {
		return
	}
	if _, err := ns.RequestNotificationAuthorization(); err != nil {
		slog.Info("notification authorization", "err", err)
	}
}

func (n *notifier) send(id, title, body string) {
	n.mu.Lock()
	ns := n.ns
	n.mu.Unlock()
	if ns == nil {
		return
	}
	if err := ns.SendNotification(notifications.NotificationOptions{ID: id, Title: title, Body: body}); err != nil {
		slog.Info("send notification", "err", err)
	}
}

// slotNotice decides whether a scheduled slot's outcome deserves a
// notification: only when the user has to act (not logged in) or the wake
// kept failing (F5: "只在未登录或连续失败时提醒"). A skipped slot, a used-up
// quota or a slot still waiting for the network stays quiet.
func slotNotice(plan scheduler.PlanDef, sl store.Slot, res wake.Result) (title, body string, ok bool) {
	if sl.State != store.SlotFailed {
		return "", "", false
	}
	at := sl.At.Format("15:04")
	switch res.Outcome {
	case wake.OutcomeNotLoggedIn:
		cmd := map[string]string{"claude": "claude auth login", "codex": "codex login"}[plan.Tool]
		if cmd == "" {
			cmd = plan.Tool
		}
		return plan.Name + " 未登录", fmt.Sprintf("%s 的唤醒没有发出。请在终端运行 %s 登录。", at, cmd), true
	case wake.OutcomeQuotaExhausted:
		return "", "", false
	default:
		msg := res.Message
		if len([]rune(msg)) > 120 {
			msg = string([]rune(msg)[:120]) + "…"
		}
		body := fmt.Sprintf("%s 的唤醒尝试了 %d 次都失败了", at, sl.Attempts)
		if msg != "" {
			body += "：" + msg
		}
		return plan.Name + " 唤醒失败", body, true
	}
}

// noticeID keeps one notification per slot, so a retry replaces it.
func noticeID(plan scheduler.PlanDef, at time.Time) string {
	return fmt.Sprintf("kikri-%s-%d", plan.ID, at.Unix())
}
