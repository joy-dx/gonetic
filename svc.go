package gonetic

import (
	"sync"

	"github.com/joy-dx/gonetic/config"
	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/lockablemap"
	relayDTO "github.com/joy-dx/relay/dto"
)

// NetSvc Wrapper for imroc/req to normalize usage and shorten implementation
type NetSvc struct {
	cfg            *config.NetSvcConfig
	relay          relayDTO.RelayInterface
	clients        map[string]dto.NetClientInterface
	transferState  lockablemap.LockableMap[string, dto.TransferNotification]
	muListeners    sync.Mutex
	listenersByURL map[string][]chan dto.TransferNotification
}

func (s *NetSvc) RegisterClient(ref string, client dto.NetClientInterface) {
	s.clients[ref] = client
}

// TransferListener returns a channel of updates for a particular URL
func (s *NetSvc) TransferListener(sourceURL string) (<-chan dto.TransferNotification, func()) {
	s.muListeners.Lock()
	defer s.muListeners.Unlock()

	ch := make(chan dto.TransferNotification, 10)
	s.listenersByURL[sourceURL] = append(s.listenersByURL[sourceURL], ch)

	unsub := func() {
		s.muListeners.Lock()
		defer s.muListeners.Unlock()

		chans := s.listenersByURL[sourceURL]
		out := chans[:0]
		for _, c := range chans {
			if c != ch {
				out = append(out, c)
			}
		}
		if len(out) == 0 {
			delete(s.listenersByURL, sourceURL)
		} else {
			s.listenersByURL[sourceURL] = out
		}
		close(ch)
	}

	return ch, unsub
}

// TransferListenerClose closes all channels for a given URL manually
func (s *NetSvc) TransferListenerClose(sourceURL string) {
	s.muListeners.Lock()
	defer s.muListeners.Unlock()
	if chans, ok := s.listenersByURL[sourceURL]; ok {
		for _, c := range chans {
			close(c)
		}
		delete(s.listenersByURL, sourceURL)
	}
}
