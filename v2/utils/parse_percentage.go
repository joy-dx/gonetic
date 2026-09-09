package utils

import (
	"fmt"
	"strconv"
	"strings"
)

func ParsePercentage(input string) (float64, error) {
	input = strings.TrimSpace(input)
	input = strings.TrimSuffix(input, "%")

	value, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %v", err)
	}

	return value, nil
}
