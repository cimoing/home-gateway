package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ByteRate is a bytes-per-second value stored in YAML as a human-readable rate
// such as 100K or 10M. Plain integers remain raw bytes/s for compatibility.
type ByteRate int64

func (r ByteRate) Int64() int64 { return int64(r) }

func (r ByteRate) MarshalYAML() (any, error) {
	return FormatByteRate(int64(r)), nil
}

func (r *ByteRate) UnmarshalYAML(src []byte) error {
	text := strings.TrimSpace(string(src))
	if text == "" || text == "~" || text == "null" {
		*r = 0
		return nil
	}
	if len(text) >= 2 {
		quote := text[0]
		if (quote == '"' || quote == '\'') && text[len(text)-1] == quote {
			text = text[1 : len(text)-1]
		}
	}
	parsed, err := ParseByteRate(text)
	if err != nil {
		return err
	}
	*r = ByteRate(parsed)
	return nil
}

func (r ByteRate) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(r))
}

func (r *ByteRate) UnmarshalJSON(src []byte) error {
	src = bytes.TrimSpace(src)
	if len(src) > 0 && src[0] == '"' {
		var text string
		if err := json.Unmarshal(src, &text); err != nil {
			return err
		}
		parsed, err := ParseByteRate(text)
		if err != nil {
			return err
		}
		*r = ByteRate(parsed)
		return nil
	}
	var value int64
	if err := json.Unmarshal(src, &value); err != nil {
		return err
	}
	*r = ByteRate(value)
	return nil
}

// ParseByteRate parses rates like 102400, 100K, 10M, 1.5MiB.
// K/KB/KiB = 1024, M/MB/MiB = 1024², G/GB/GiB = 1024³.
func ParseByteRate(text string) (int64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	negative := false
	if text[0] == '+' {
		text = text[1:]
	} else if text[0] == '-' {
		negative = true
		text = text[1:]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("invalid byte rate %q", text)
	}

	unitStart := len(text)
	for index, r := range text {
		if unicode.IsLetter(r) {
			unitStart = index
			break
		}
	}
	numberText := strings.TrimSpace(text[:unitStart])
	unit := strings.ToLower(strings.TrimSpace(text[unitStart:]))
	if numberText == "" {
		return 0, fmt.Errorf("invalid byte rate %q", text)
	}

	multiplier := int64(1)
	switch unit {
	case "", "b", "bps":
		multiplier = 1
	case "k", "kb", "kib", "ki":
		multiplier = 1024
	case "m", "mb", "mib", "mi":
		multiplier = 1024 * 1024
	case "g", "gb", "gib", "gi":
		multiplier = 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("invalid byte rate unit %q", unit)
	}

	if strings.ContainsAny(numberText, ".eE") {
		value, err := strconv.ParseFloat(numberText, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid byte rate %q: %w", text, err)
		}
		if value < 0 {
			return 0, fmt.Errorf("byte rate must be zero or positive")
		}
		result := int64(value*float64(multiplier) + 0.5)
		if negative {
			return 0, fmt.Errorf("byte rate must be zero or positive")
		}
		return result, nil
	}

	value, err := strconv.ParseInt(numberText, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte rate %q: %w", text, err)
	}
	if value < 0 || negative {
		return 0, fmt.Errorf("byte rate must be zero or positive")
	}
	if value != 0 && multiplier > 1 && value > (1<<63-1)/multiplier {
		return 0, fmt.Errorf("byte rate overflow %q", text)
	}
	return value * multiplier, nil
}

// FormatByteRate renders a bytes/s value as 0, 100K, 10M, or a raw integer.
func FormatByteRate(value int64) string {
	if value <= 0 {
		return "0"
	}
	const (
		kib = 1024
		mib = 1024 * 1024
		gib = 1024 * 1024 * 1024
	)
	switch {
	case value%gib == 0:
		return strconv.FormatInt(value/gib, 10) + "G"
	case value%mib == 0:
		return strconv.FormatInt(value/mib, 10) + "M"
	case value%kib == 0:
		return strconv.FormatInt(value/kib, 10) + "K"
	default:
		return strconv.FormatInt(value, 10)
	}
}
