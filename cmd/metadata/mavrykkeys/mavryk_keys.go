package mavrykkeys

import (
	"github.com/dipdup-net/go-lib/tzkt/data"
	"github.com/mavryk-network/dipdup-metadata/cmd/metadata/helpers"
	"github.com/mavryk-network/dipdup-metadata/cmd/metadata/models"
)

// MavrykKeysAction -
type MavrykKeysAction struct {
	Action models.Action
	Update models.MavrykKey
}

// MavrykKeys -
type MavrykKeys struct {
	repo *models.MavrykKeys
}

// NewMavrykKeys -
func NewMavrykKeys(repo *models.MavrykKeys) *MavrykKeys {
	return &MavrykKeys{repo}
}

// Add -
func (tk *MavrykKeys) Add(update data.BigMapUpdate, network string) error {
	val := string(update.Content.Value)
	if !helpers.IsJSON(val) { // wait only JSON
		return nil
	}
	item, err := models.ContextFromUpdate(update, network)
	if err != nil {
		return err
	}

	switch update.Action {
	case "add_key", "update_key":
		return tk.repo.Save(item)
	case "remove_key":
		return tk.repo.Delete(item)
	}
	return nil
}

// Get -
func (tk *MavrykKeys) Get(network, address, key string) (models.MavrykKey, error) {
	return tk.repo.Get(network, address, key)
}
