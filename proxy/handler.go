package proxy

import (
	"context"
	"errors"
	"log/slog"
)

var _ Handler = (*handler)(nil)

var errSessionMissing = errors.New("session is missing")

type Session struct {
	ID       string
	Username string
	Password []byte
}

type sessionKey struct{}

type Handler interface {
	Connect(ctx context.Context) error

	Disconnect(ctx context.Context) error

	Publish(ctx context.Context, topic *string, payload *[]byte) error
}

type handler struct {
	logger *slog.Logger
}

func New(logger *slog.Logger) *handler {
	return &handler{
		logger: logger,
	}
}

func (h *handler) Connect(ctx context.Context) error {
	return h.logAction(ctx, "Connect", nil, nil)
}

func (h *handler) Publish(ctx context.Context, topic *string, payload *[]byte) error {
	return h.logAction(ctx, "Publish", &[]string{*topic}, payload)
}

func (h *handler) Disconnect(ctx context.Context) error {
	return h.logAction(ctx, "Disconnect", nil, nil)
}

func (h *handler) logAction(ctx context.Context, action string, topics *[]string, payload *[]byte) error {
	s, ok := FromContext(ctx)
	args := []interface{}{
		slog.Group("session", slog.String("id", s.ID), slog.String("username", s.Username)),
	}

	if topics != nil {
		args = append(args, slog.Any("topics", *topics))
	}
	if payload != nil {
		args = append(args, slog.Any("payload", *payload))
	}

	if !ok {
		args = append(args, slog.Any("error", errSessionMissing))
		h.logger.Error(action+"() failed to complete", args...)
		return errSessionMissing
	}

	h.logger.Info(action+"() completed successfully", args...)

	return nil
}

func FromContext(ctx context.Context) (*Session, bool) {
	if s, ok := ctx.Value(sessionKey{}).(*Session); ok && s != nil {
		return s, true
	}
	return nil, false
}
