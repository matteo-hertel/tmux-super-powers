package cmd

import (
	"fmt"
	"strings"
)

// normalizeAddress takes a user-provided address and ensures it's a full URL.
// Rules:
//   - "http://..." → use as-is
//   - "host:port" → prepend http://
//   - "host" → prepend http://, append default port
func normalizeAddress(addr string, defaultPort int) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	if strings.Contains(addr, ":") {
		return "http://" + addr
	}
	return fmt.Sprintf("http://%s:%d", addr, defaultPort)
}
