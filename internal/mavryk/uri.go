package mavryk

import (
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// URI -
type URI struct {
	Address string
	Network string
	Key     string
}

// Parse -
func (uri *URI) Parse(value string) (err error) {
	if !strings.HasPrefix(value, PrefixMavrykStorage) {
		return errors.Wrap(ErrInvalidMavrykStoragePrefix, value)
	}

	uri.Key = strings.TrimPrefix(value, PrefixMavrykStorage)
	if strings.HasPrefix(uri.Key, "//") {
		uri.Key = strings.TrimPrefix(uri.Key, "//")
		parts := strings.Split(uri.Key, "/")
		if len(parts) > 1 {
			uri.parseHost(parts[0])

			if len(parts) == 2 {
				uri.Key, err = url.QueryUnescape(parts[1])
				if err != nil {
					return
				}
			}
		}
	}
	return
}

func (uri *URI) parseHost(host string) {
	parts := strings.Split(host, ".")
	if isAddress(parts[0]) {
		uri.Address = parts[0]
	}

	if len(parts) == 2 {
		uri.Network = parts[1]
	}
}

func isAddress(str string) bool {
	return len(str) == 36 && (strings.HasPrefix(str, "KT") || strings.HasPrefix(str, "mv1") || strings.HasPrefix(str, "mv2") || strings.HasPrefix(str, "mv3"))
}
