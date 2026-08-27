package utils

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

func ParseInt(value string) (int, error) {
	num, err := strconv.Atoi(value)

	if err != nil {
		return 0, fmt.Errorf("Error parsing integer: %w", err)
	}

	return num, nil
}

func ParseTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC1123, value)

	if err != nil {
		return time.Time{}, fmt.Errorf("Error parsing timestamp: %w", err)
	}

	return t, nil
}

func ParseStringsSlice(value string) ([]string, error) {
	var links []string

	err := json.Unmarshal([]byte(value), &links)

	if err != nil {
		return nil, fmt.Errorf("Error parsing JSON string slice: %w", err)
	}

	return links, nil
}
