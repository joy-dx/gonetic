package gonetic

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"github.com/joy-dx/gonetic/client/httpclient"
	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/gonetic/relays"
)

func (s *NetSvc) State() *dto.NetState {

	return &dto.NetState{
		ExtraHeaders:             s.cfg.ExtraHeaders,
		RequestTimeout:           s.cfg.RequestTimeout,
		UserAgent:                s.cfg.UserAgent,
		BlacklistDomains:         s.cfg.BlacklistDomains,
		WhitelistDomains:         s.cfg.WhitelistDomains,
		DownloadCallbackInterval: s.cfg.DownloadCallbackInterval,
		PreferCurlDownloads:      s.cfg.PreferCurlDownloads,
		TransfersStatus:          s.transferState.GetAll(),
	}
}

func isCurlAvailable() bool {
	_, err := exec.LookPath("curl")
	return err == nil
}

func (s *NetSvc) Hydrate(ctx context.Context) error {
	if s.cfg == nil {
		return errors.New("no net config")
	}
	if s.relay == nil {
		return errors.New("no relay implementation")
	}
	// On Mac, to conform to download security policy, force curl
	if runtime.GOOS == "darwin" {
		s.cfg.WithPreferCurl(true)
	}
	if s.cfg.PreferCurlDownloads && !isCurlAvailable() {
		s.relay.Warn(relays.RlyNetLog{Msg: "Curl set as preference but, not available"})
		s.cfg.WithPreferCurl(false)
	}

	defaultClientCfg := httpclient.DefaultHTTPClientConfig()
	defaultClient := httpclient.NewHTTPClient(dto.NET_DEFAULT_CLIENT_REF, s.cfg, &defaultClientCfg)
	s.clients[dto.NET_DEFAULT_CLIENT_REF] = defaultClient

	return nil
}
