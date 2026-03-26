package client

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestConfigWithDefaults_SetsReasonableValues(t *testing.T) {
	cfg := (Config{
		Endpoints: []string{"127.0.0.1:5672"},
	}).WithDefaults()

	if cfg.ConnectionTimeout <= 0 {
		t.Fatalf("expected positive connection timeout, got %s", cfg.ConnectionTimeout)
	}
	if cfg.Heartbeat <= 0 {
		t.Fatalf("expected positive heartbeat, got %s", cfg.Heartbeat)
	}
	if cfg.ConsumerPrefetch <= 0 {
		t.Fatalf("expected positive consumer prefetch, got %d", cfg.ConsumerPrefetch)
	}
	if !cfg.PublisherConfirm {
		t.Fatal("expected publisher confirm to default to enabled")
	}
}

func TestConfigValidate_RejectsMissingEndpoints(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("expected missing endpoints to fail validation")
	}
}

func TestConfigURLs_EncodesCredentialsAndVHost(t *testing.T) {
	cfg := Config{
		Endpoints: []string{"rabbitmq-1:5672", "rabbitmq-2:5672"},
		Username:  "astra",
		Password:  "astra-dev",
		VHost:     "/astra",
	}

	got, err := cfg.URLs()
	if err != nil {
		t.Fatalf("build urls: %v", err)
	}

	want := []string{
		"amqp://astra:astra-dev@rabbitmq-1:5672/%2Fastra",
		"amqp://astra:astra-dev@rabbitmq-2:5672/%2Fastra",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected urls: got %#v want %#v", got, want)
	}
}

func TestManagerDial_TriesEndpointsInOrder(t *testing.T) {
	var called []string
	manager, err := NewManager(Config{
		Endpoints: []string{"rabbitmq-1:5672", "rabbitmq-2:5672"},
		Username:  "astra",
		Password:  "astra-dev",
		VHost:     "/astra",
	}, WithDialer(func(ctx context.Context, endpoint string, cfg Config) (Connection, error) {
		called = append(called, endpoint)
		if endpoint == "rabbitmq-1:5672" {
			return nil, errors.New("first endpoint down")
		}
		return &fakeConnection{}, nil
	}))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.Dial(context.Background()); err != nil {
		t.Fatalf("dial: %v", err)
	}

	if !reflect.DeepEqual(called, []string{"rabbitmq-1:5672", "rabbitmq-2:5672"}) {
		t.Fatalf("unexpected endpoint dial order %#v", called)
	}
	if got := manager.ActiveEndpoint(); got != "rabbitmq-2:5672" {
		t.Fatalf("expected active endpoint rabbitmq-2:5672, got %q", got)
	}
}

func TestPreparePublisherChannel_EnablesConfirmMode(t *testing.T) {
	ch := &fakePublisherChannel{}
	if err := preparePublisherChannel(ch, true); err != nil {
		t.Fatalf("prepare publisher channel: %v", err)
	}
	if len(ch.confirmCalls) != 1 || !ch.confirmCalls[0] {
		t.Fatalf("expected confirm(true) call, got %#v", ch.confirmCalls)
	}
}

func TestPrepareConsumerChannel_SetsQoS(t *testing.T) {
	ch := &fakeConsumerChannel{}
	if err := prepareConsumerChannel(ch, 64); err != nil {
		t.Fatalf("prepare consumer channel: %v", err)
	}
	if ch.prefetchCount != 64 {
		t.Fatalf("expected prefetch 64, got %d", ch.prefetchCount)
	}
	if ch.prefetchSize != 0 {
		t.Fatalf("expected prefetch size 0, got %d", ch.prefetchSize)
	}
	if ch.global {
		t.Fatal("expected qos global=false")
	}
}

func TestHealthSummary_ReportsManagerState(t *testing.T) {
	manager, err := NewManager(Config{
		Endpoints:         []string{"rabbitmq-1:5672"},
		ConnectionTimeout: 2 * time.Second,
		Heartbeat:         5 * time.Second,
		ConsumerPrefetch:  32,
		PublisherConfirm:  true,
	}, WithDialer(func(ctx context.Context, endpoint string, cfg Config) (Connection, error) {
		return &fakeConnection{}, nil
	}))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := manager.Dial(context.Background()); err != nil {
		t.Fatalf("dial: %v", err)
	}

	summary := manager.HealthSummary()
	if summary.Endpoint != "rabbitmq-1:5672" {
		t.Fatalf("unexpected health endpoint %q", summary.Endpoint)
	}
	if !summary.Connected {
		t.Fatal("expected health summary to report connected=true")
	}
	if !summary.PublisherConfirm {
		t.Fatal("expected health summary to report publisher confirm enabled")
	}
}

type fakeConnection struct{}

func (f *fakeConnection) Close() error { return nil }

func (f *fakeConnection) IsClosed() bool { return false }

func (f *fakeConnection) OpenChannel() (*amqp.Channel, error) { return nil, nil }

func (f *fakeConnection) URL() *url.URL { return nil }

type fakePublisherChannel struct {
	confirmCalls []bool
}

func (f *fakePublisherChannel) Confirm(noWait bool) error {
	f.confirmCalls = append(f.confirmCalls, noWait)
	return nil
}

type fakeConsumerChannel struct {
	prefetchCount int
	prefetchSize  int
	global        bool
}

func (f *fakeConsumerChannel) Qos(prefetchCount, prefetchSize int, global bool) error {
	f.prefetchCount = prefetchCount
	f.prefetchSize = prefetchSize
	f.global = global
	return nil
}
