package dto

import (
	"net/http"
	"time"
)

type TransferNotification struct {
	Source      string `json:"source" yaml:"source"`
	Destination string `json:"destination" yaml:"destination"`
	Message     string `json:"message,omitempty" yaml:"message,omitempty"`
	// Status MetaType of message
	Status TransferStatus `json:"status" yaml:"status"`
	// Percentage completion status as a percentage
	Percentage float64 `json:"percentage" yaml:"percentage"`
	// TotalSize length content in bytes. The value -1 indicates that the length is unknown
	TotalSize int64 `json:"total_size,omitempty" yaml:"total_size,omitempty"`
	// Downloaded downloaded body length in bytes
	Downloaded int64 `json:"downloaded,omitempty" yaml:"downloaded,omitempty"`
}

type NetState struct {
	ExtraHeaders             ExtraHeaders  `json:"net_extra_headers,omitempty" yaml:"net_extra_headers,omitempty"`
	RequestTimeout           time.Duration `json:"net_request_timeout,omitempty" yaml:"net_request_timeout,omitempty"`
	UserAgent                string        `json:"net_user_agent,omitempty" yaml:"net_user_agent,omitempty"`
	BlacklistDomains         []string      `json:"net_blacklist_domains,omitempty" yaml:"net_blacklist_domains,omitempty"`
	WhitelistDomains         []string      `json:"net_whitelist_domains,omitempty" yaml:"net_whitelist_domains,omitempty"`
	DownloadCallbackInterval time.Duration `json:"net_download_callback_interval,omitempty" yaml:"net_download_callback_interval,omitempty"`
	// PreferCurlDownloads Instead of using imroc/req for downloads, prefer to use curl found on $PATH if available
	PreferCurlDownloads bool                            `json:"prefer_curl_downloads,omitempty" yaml:"net_prefer_curl_downloads,omitempty"`
	TransfersStatus     map[string]TransferNotification `json:"net_transfers_status,omitempty" yaml:"net_transfers_status,omitempty"`
}

// Download File
type DownloadFileConfig struct {
	// Blocking Determine whether or not program execution should wait
	Blocking bool
	Checksum string
	URL      string
	// DestinationFolder Used if path not set appending
	DestinationFolder string
	OutputFileName    string
	SkipAllowedPaths  bool
}

type Response struct {
	StatusCode int
	Headers    http.Header
	// As well as casting to ResponseObject if set, return as byes
	Body []byte
}
