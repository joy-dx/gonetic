package dto

import (
	"encoding/json"
	"strings"
)

// ExtraHeaders type is a comma seperated key=value string defined for use with Viper appconfig parsing
type ExtraHeaders map[string]string

func (e ExtraHeaders) String() string {
	data, _ := json.MarshalIndent(e, "", "  ")
	return string(data)
}

// Set Value should be a comma seperated key=value string
func (e ExtraHeaders) Set(s string) error {
	// First split by comma
	for _, header := range strings.Split(s, ",") {
		// Then split by = sign
		headerList := strings.Split(header, "=")
		// Set map
		e[headerList[0]] = headerList[1]
	}
	return nil
}

func (e ExtraHeaders) Type() string {
	return "ExtraHeaders"
}
