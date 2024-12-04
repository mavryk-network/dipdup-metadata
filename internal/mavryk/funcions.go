package mavryk

import "strings"

// prefixes
const (
	PrefixMavrykStorage = "mavryk-storage:"
)

// Is -
func Is(link string) bool {
	return strings.HasPrefix(link, PrefixMavrykStorage)
}
