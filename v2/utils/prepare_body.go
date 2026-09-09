package utils

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func PrepareBody(body map[string]interface{}, bodyType string) ([]byte, string, error) {
	if body == nil {
		return nil, "", nil
	}

	switch strings.ToLower(bodyType) {
	case "application/json":
		buf, err := json.Marshal(body)
		return buf, "application/json", err
	case "application/x-www-form-urlencoded":
		vals := url.Values{}
		for k, v := range body {
			vals.Set(k, fmt.Sprintf("%v", v))
		}
		return []byte(vals.Encode()), "application/x-www-form-urlencoded", nil
	default:
		return nil, "", fmt.Errorf("unsupported body_type: %s", bodyType)
	}
}
