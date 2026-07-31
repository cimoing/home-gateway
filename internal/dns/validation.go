package dns

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"home-gateway/internal/cloudflare"
)

var domainPattern = regexp.MustCompile(
	`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))*$`,
)

func validateRecord(input cloudflare.RecordInput) error {
	recordType := strings.ToUpper(strings.TrimSpace(input.Type))
	if input.Name != "@" && !validDomain(strings.TrimSuffix(input.Name, ".")) {
		return fmt.Errorf("%w: invalid record name", ErrInvalidInput)
	}
	if input.TTL != 1 && (input.TTL < 60 || input.TTL > 86400) {
		return fmt.Errorf("%w: TTL must be 1 (automatic) or 60 to 86400", ErrInvalidInput)
	}
	if input.Proxied != nil && recordType != "A" && recordType != "AAAA" && recordType != "CNAME" {
		return fmt.Errorf("%w: proxying is only supported for A, AAAA, and CNAME", ErrInvalidInput)
	}

	switch recordType {
	case "A":
		ip := net.ParseIP(input.Content)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("%w: A record content must be IPv4", ErrInvalidInput)
		}
	case "AAAA":
		ip := net.ParseIP(input.Content)
		if ip == nil || ip.To4() != nil {
			return fmt.Errorf("%w: AAAA record content must be IPv6", ErrInvalidInput)
		}
	case "CNAME":
		if !validTarget(input.Content) {
			return fmt.Errorf("%w: invalid CNAME target", ErrInvalidInput)
		}
	case "TXT":
		if len(input.Content) > 4096 {
			return fmt.Errorf("%w: TXT content is too long", ErrInvalidInput)
		}
	case "MX":
		if !validTarget(input.Content) || input.Priority == nil ||
			*input.Priority < 0 || *input.Priority > 65535 {
			return fmt.Errorf("%w: MX requires a target and priority from 0 to 65535", ErrInvalidInput)
		}
	case "CAA":
		if !validCAA(input.Data) {
			return fmt.Errorf("%w: CAA requires flags, tag, and value", ErrInvalidInput)
		}
	case "SRV":
		if !validSRV(input.Data) {
			return fmt.Errorf(
				"%w: SRV requires priority, weight, port, and target",
				ErrInvalidInput,
			)
		}
	default:
		return fmt.Errorf("%w: unsupported DNS record type %q", ErrInvalidInput, recordType)
	}
	return nil
}

func validDomain(value string) bool {
	return len(value) > 0 && len(value) <= 253 && domainPattern.MatchString(value)
}

func validTarget(value string) bool {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	return value == "@" || validDomain(value)
}

func validCAA(data map[string]any) bool {
	if data == nil {
		return false
	}
	flags, ok := numberField(data, "flags")
	tag, tagOK := data["tag"].(string)
	value, valueOK := data["value"].(string)
	return ok && flags >= 0 && flags <= 255 && tagOK && tag != "" && valueOK && value != ""
}

func validSRV(data map[string]any) bool {
	if data == nil {
		return false
	}
	priority, priorityOK := numberField(data, "priority")
	weight, weightOK := numberField(data, "weight")
	port, portOK := numberField(data, "port")
	target, targetOK := data["target"].(string)
	return priorityOK && priority >= 0 && priority <= 65535 &&
		weightOK && weight >= 0 && weight <= 65535 &&
		portOK && port >= 0 && port <= 65535 &&
		targetOK && validTarget(target)
}

func numberField(data map[string]any, name string) (int, bool) {
	switch value := data[name].(type) {
	case int:
		return value, true
	case float64:
		integer := int(value)
		return integer, float64(integer) == value
	default:
		return 0, false
	}
}
