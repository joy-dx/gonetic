package gonetic

import (
	"sync"

	"github.com/joy-dx/gonetic/config"
	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/gonetic/relays"
	"github.com/joy-dx/lockablemap"
)

var (
	service     *NetSvc
	serviceOnce sync.Once
)

func ProvideNetSvc(cfg *config.NetSvcConfig) *NetSvc {
	serviceOnce.Do(func() {
		service = &NetSvc{
			cfg:            cfg,
			relay:          cfg.Relay(),
			listenersByURL: make(map[string][]chan dto.TransferNotification),
			transferState:  *lockablemap.NewLockableMap[string, dto.TransferNotification](),
			clients:        make(map[string]dto.NetClientInterface),
		}
		cfg.Relay().Debug(relays.RlyNetLog{Msg: "Net service started"})
	})
	return service
}
