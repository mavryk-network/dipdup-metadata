package models

import (
	"github.com/dipdup-net/go-lib/database"
	"github.com/dipdup-net/go-lib/tzkt/data"
	"github.com/mavryk-network/dipdup-metadata/cmd/metadata/helpers"
)

// Action -
type Action string

// Actions
const (
	ActionDelete Action = "delete"
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
)

// MavrykKey -
type MavrykKey struct {
	//nolint
	tableName struct{} `pg:"mavryk_keys"`

	ID      uint64 `json:"-"  pg:",notnull"`
	Network string `pg:",unique:mavryk_key"`
	Address string `pg:",unique:mavryk_key"`
	Key     string `pg:",unique:mavryk_key"`
	Value   []byte
}

// TableName -
func (MavrykKey) TableName() string {
	return "mavryk_keys"
}

// ContextFromUpdate -
func ContextFromUpdate(update data.BigMapUpdate, network string) (MavrykKey, error) {
	var ctx MavrykKey
	ctx.Address = update.Contract.Address
	ctx.Network = network
	ctx.Key = helpers.Trim(string(update.Content.Key))

	data, err := helpers.Decode(update.Content.Value)
	if err != nil {
		return ctx, err
	}
	ctx.Value = data
	return ctx, nil
}

// MavrykKeys -
type MavrykKeys struct {
	db *database.PgGo
}

// NewMavrykKeys -
func NewMavrykKeys(db *database.PgGo) *MavrykKeys {
	return &MavrykKeys{db}
}

// Get -
func (keys *MavrykKeys) Get(network, address, key string) (tk MavrykKey, err error) {
	query := keys.db.DB().Model(&tk)

	if network != "" {
		query.Where("network = ?", network)
	}
	if address != "" {
		query.Where("address = ?", address)
	}
	if key != "" {
		query.Where("key = ?", key)
	}

	err = query.First()
	return
}

// Save -
func (keys *MavrykKeys) Save(tk MavrykKey) error {
	_, err := keys.db.DB().Model(&tk).OnConflict("(network, address, key) DO UPDATE").Set("value = excluded.value").Insert()
	return err
}

// Delete -
func (keys *MavrykKeys) Delete(tk MavrykKey) error {
	query := keys.db.DB().Model(&tk)

	if tk.Network != "" {
		query.Where("network = ?", tk.Network)
	}
	if tk.Address != "" {
		query.Where("address = ?", tk.Address)
	}
	if tk.Key != "" {
		query.Where("key = ?", tk.Key)
	}

	_, err := query.Delete()
	return err
}
