package notify

import (
	"context"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/telegram"
)

type Worker struct {
	repo     *Repository
	sender   *telegram.Sender
	interval time.Duration
}

func NewWorker(args ...interface{}) *Worker {
	w := &Worker{interval: 500 * time.Millisecond}
	for _, a := range args {
		switch v := a.(type) {
		case *Repository:
			w.repo = v
		case Repository:
			w.repo = &v
		case *telegram.Sender:
			w.sender = v
		case time.Duration:
			if v > 0 {
				w.interval = v
			}
		}
	}
	return w
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil {
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = w.RunOnce(ctx)
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) error {
	if w == nil || w.repo == nil {
		return nil
	}
	rows, err := w.repo.ListPendingOutbox(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		attempts := row.Attempts + 1
		var sendErr error
		if w.sender != nil {
			sendErr = w.sender.SendMessage(ctx, row.ChatID, row.Body)
		}
		if sendErr == nil {
			_ = w.repo.MarkSent(ctx, row.ID)
			continue
		}
		backoff := time.Duration(1<<uint(attempts)) * time.Second
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
		next := time.Now().Add(backoff)
		_ = w.repo.MarkAttempt(ctx, row.ID, attempts, next, sendErr.Error())
	}
	return nil
}
