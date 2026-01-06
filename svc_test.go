package gonetic

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/joy-dx/gonetic/config"
	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/lockablemap"
	relayDTO "github.com/joy-dx/relay/dto"
)

// ---------- fakes ----------

type fakeRelay struct {
	mu   sync.Mutex
	msgs []string
	evts []relayDTO.RelayEventInterface
}

func (r *fakeRelay) Debug(data relayDTO.RelayEventInterface) { r.add(data) }
func (r *fakeRelay) Info(data relayDTO.RelayEventInterface)  { r.add(data) }
func (r *fakeRelay) Warn(data relayDTO.RelayEventInterface)  { r.add(data) }
func (r *fakeRelay) Error(data relayDTO.RelayEventInterface) { r.add(data) }
func (r *fakeRelay) Fatal(data relayDTO.RelayEventInterface) { r.add(data) }
func (r *fakeRelay) Meta(data relayDTO.RelayEventInterface)  { r.add(data) }

func (r *fakeRelay) add(e relayDTO.RelayEventInterface) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evts = append(r.evts, e)
	if e != nil {
		r.msgs = append(r.msgs, e.Message())
	}
}

func (r *fakeRelay) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.evts)
}

// Optional helper if you want a dummy event in tests.
type fakeRelayEvent struct{ msg string }

func (e fakeRelayEvent) RelayChannel() relayDTO.EventChannel { return "" }
func (e fakeRelayEvent) RelayType() relayDTO.EventRef        { return "" }
func (e fakeRelayEvent) Message() string                     { return e.msg }
func (e fakeRelayEvent) ToSlog() []slog.Attr                 { return nil }

type fakeNetClient struct {
	ref  string
	typ  dto.NetClientType
	fn   func(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error)
	call int
	mu   sync.Mutex
}

func (c *fakeNetClient) Ref() string             { return c.ref }
func (c *fakeNetClient) Type() dto.NetClientType { return c.typ }
func (c *fakeNetClient) ProcessRequest(
	ctx context.Context,
	cfg *dto.RequestConfig,
) (dto.Response, error) {
	c.mu.Lock()
	c.call++
	c.mu.Unlock()
	return c.fn(ctx, cfg)
}

type tempErr struct{ msg string }

func (e tempErr) Error() string   { return e.msg }
func (e tempErr) Temporary() bool { return true }

// ---------- helpers ----------

func newTestSvc(t *testing.T) *NetSvc {
	t.Helper()

	cfg := config.DefaultNetSvcConfig()
	// If your NetSvc constructor exists, use it. Here we assemble directly.
	s := &NetSvc{
		cfg:            &cfg,
		relay:          &fakeRelay{},
		clients:        map[string]dto.NetClientInterface{},
		transferState:  *lockablemap.NewLockableMap[string, dto.TransferNotification](),
		listenersByURL: map[string][]chan dto.TransferNotification{},
	}
	return s
}

type noWaitDelay struct{}

func (d noWaitDelay) Wait(taskName string, attempt int) {}

func TestNetSvc_RegisterClient_Golden(t *testing.T) {
	t.Parallel()

	s := newTestSvc(t)
	c := &fakeNetClient{ref: "x", fn: func(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error) {
		return dto.Response{StatusCode: 200}, nil
	}}

	s.RegisterClient("x", c)

	if _, ok := s.clients["x"]; !ok {
		t.Fatalf("client not registered")
	}
}

func TestNetSvc_TransferListeners_Golden(t *testing.T) {
	t.Parallel()

	s := newTestSvc(t)

	url := "https://example.com/file"
	ch1, _ := s.TransferListener(url)
	ch2, _ := s.TransferListener(url)

	s.publishTransferUpdate(dto.TransferNotification{
		Source:      url,
		Destination: "/tmp/x",
		Status:      dto.IN_PROGRESS,
		Percentage:  50,
	})

	// Both should receive IN_PROGRESS.
	select {
	case n := <-ch1:
		if n.Status != dto.IN_PROGRESS {
			t.Fatalf("ch1 status=%s want %s", n.Status, dto.IN_PROGRESS)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for ch1 update")
	}

	select {
	case n := <-ch2:
		if n.Status != dto.IN_PROGRESS {
			t.Fatalf("ch2 status=%s want %s", n.Status, dto.IN_PROGRESS)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for ch2 update")
	}

	// Terminal state should be delivered (channel may remain open now).
	s.publishTransferUpdate(dto.TransferNotification{
		Source:      url,
		Destination: "/tmp/x",
		Status:      dto.COMPLETE,
		Percentage:  100,
	})

	select {
	case n := <-ch1:
		if n.Status != dto.COMPLETE {
			t.Fatalf("ch1 status=%s want %s", n.Status, dto.COMPLETE)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for ch1 COMPLETE")
	}

	select {
	case n := <-ch2:
		if n.Status != dto.COMPLETE {
			t.Fatalf("ch2 status=%s want %s", n.Status, dto.COMPLETE)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for ch2 COMPLETE")
	}
}
