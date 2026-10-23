package gonetic

import (
	"sync"

	"github.com/joy-dx/gonetic/v2/config"
	"github.com/joy-dx/gonetic/v2/dto"
	"github.com/joy-dx/lockablemap"
	relayDTO "github.com/joy-dx/relay/v2/dto"
)

// NetSvc Wrapper for imroc/req to normalize usage and shorten implementation
type NetSvc struct {
	cfg             *config.NetSvcConfig
	relay           relayDTO.RelayInterface
	clients         map[string]dto.NetClientInterface
	transferState   lockablemap.LockableMap[string, dto.TransferNotification]
	muListeners     sync.Mutex
	listenersByURL  map[string][]chan dto.TransferNotification
	downloadMu      sync.Mutex
	activeDownloads map[string]bool
}

func (s *NetSvc) RegisterClient(ref string, client dto.NetClientInterface) {
	s.clients[ref] = client
}
