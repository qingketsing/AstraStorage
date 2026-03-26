package idempotency

import (
	"context"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
)

const defaultTTL = 10 * time.Minute

type Handler struct {
	store Store
	ttl   time.Duration
}

func NewHandler(store Store, ttl time.Duration) *Handler {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &Handler{store: store, ttl: ttl}
}

func (h *Handler) Execute(ctx context.Context, envelope contracts.Envelope, fn func(context.Context) error) (bool, error) {
	if h == nil || h.store == nil {
		if fn == nil {
			return false, nil
		}
		return false, fn(ctx)
	}
	key, err := KeyForEnvelope(envelope)
	if err != nil {
		return false, err
	}
	duplicate, err := h.store.Claim(ctx, key, h.ttl)
	if err != nil || duplicate {
		return duplicate, err
	}
	if fn == nil {
		return false, h.store.Complete(ctx, key, h.ttl)
	}
	if err := fn(ctx); err != nil {
		_ = h.store.Release(ctx, key)
		return false, err
	}
	if err := h.store.Complete(ctx, key, h.ttl); err != nil {
		_ = h.store.Release(ctx, key)
		return false, err
	}
	return false, nil
}
