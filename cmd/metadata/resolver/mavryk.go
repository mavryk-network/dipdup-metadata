package resolver

import (
	"context"

	"github.com/mavryk-network/dipdup-metadata/cmd/metadata/mavrykkeys"
	"github.com/mavryk-network/dipdup-metadata/internal/mavryk"
)

type mavrykData struct {
	Data []byte
	URI  mavryk.URI
}

// MavrykStorage -
type MavrykStorage struct {
	tk *mavrykkeys.MavrykKeys
}

// NewMavrykStorage -
func NewMavrykStorage(ctx *mavrykkeys.MavrykKeys) MavrykStorage {
	return MavrykStorage{ctx}
}

// Resolve -
func (s MavrykStorage) Resolve(ctx context.Context, network, address, value string) (mavrykData, error) {
	var uri mavryk.URI
	if err := uri.Parse(value); err != nil {
		return mavrykData{}, newResolvingError(0, ErrorTypeMavrykURIParsing, err)
	}

	if uri.Network == "" {
		uri.Network = network
	}

	if uri.Address == "" {
		uri.Address = address
	}

	item, err := s.tk.Get(uri.Network, uri.Address, uri.Key)
	if err != nil {
		return mavrykData{
			URI: uri,
		}, newResolvingError(0, ErrorTypeKeyMavrykNotFond, ErrMavrykStorageKeyNotFound)
	}

	return mavrykData{
		URI:  uri,
		Data: item.Value,
	}, nil
}

// Is -
func (s MavrykStorage) Is(link string) bool {
	return mavryk.Is(link)
}
