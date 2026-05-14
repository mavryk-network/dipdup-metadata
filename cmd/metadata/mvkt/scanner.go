package mvkt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dipdup-net/go-lib/config"
	"github.com/dipdup-net/go-lib/tzkt/api"
	"github.com/dipdup-net/go-lib/tzkt/data"
	"github.com/dipdup-net/go-lib/tzkt/events"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	pageSize        = 1000
	syncLevelRange  = 100000
	reconnectDelay  = 5 * time.Second
	watchdogTimeout = 2 * time.Minute
)

// Scanner -
type Scanner struct {
	api       *api.API
	client    *events.TzKT
	wsURL     string
	baseURL   string
	lastID    uint64
	level     uint64
	msg       Message
	contracts []string

	diffs  chan Message
	blocks chan data.Block
	wg     *sync.WaitGroup
}

// New -
func New(cfg config.DataSource, contracts ...string) (*Scanner, error) {
	baseURL, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	eventsURL := baseURL.JoinPath("v1/ws")

	return &Scanner{
		wsURL:     eventsURL.String(),
		baseURL:   baseURL.String(),
		api:       api.New(baseURL.String()),
		msg:       newMessage(),
		contracts: contracts,
		diffs:     make(chan Message, 1024),
		blocks:    make(chan data.Block, 10),
		wg:        new(sync.WaitGroup),
	}, nil
}

// Start -
func (scanner *Scanner) Start(ctx context.Context, startLevel, endLevel uint64) {
	if endLevel > 0 && startLevel > 0 && startLevel > endLevel {
		return
	}

	scanner.wg.Add(1)
	go scanner.synchronization(ctx, startLevel, endLevel)
}

func (scanner *Scanner) synchronization(ctx context.Context, startLevel, endLevel uint64) {
	defer scanner.wg.Done()

	scanner.level = startLevel

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if endLevel > 0 && scanner.level > endLevel {
			log.Warn().Msgf("synchronization was stopped due to last_level in config is equal to current level")
			return
		}

		// Reset cursor for fresh sync cycle
		scanner.lastID = 0

		headLevel, err := scanner.headLevelWithRetry(ctx)
		if err != nil {
			return // context cancelled
		}
		log.Info().Msgf("Current head is %d. Indexer state is %d.", headLevel, scanner.level)

		// Catch up to head via REST API
		for headLevel > scanner.level {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := scanner.sync(ctx, headLevel); err != nil {
				log.Err(err).Msg("sync")
			}

			headLevel, err = scanner.headLevelWithRetry(ctx)
			if err != nil {
				return
			}
		}

		// Start WebSocket listener — blocks until connection is lost
		scanner.start(ctx)

		// Listener stopped — wait then loop back to re-sync
		log.Warn().Msg("WebSocket connection lost, will re-sync and reconnect...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

// headLevel fetches only the head level from the API.
// Uses a local struct to avoid go-lib's data.Head which defines QuoteLevel
// as uint64, incompatible with the Mavryk API returning -1.
func (scanner *Scanner) headLevel(ctx context.Context) (uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scanner.baseURL+"/v1/head", nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var head struct {
		Level uint64 `json:"level"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&head); err != nil {
		return 0, err
	}
	return head.Level, nil
}

func (scanner *Scanner) headLevelWithRetry(ctx context.Context) (uint64, error) {
	for {
		level, err := scanner.headLevel(ctx)
		if err == nil {
			return level, nil
		}
		log.Err(err).Msg("GetHead, retrying...")
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(reconnectDelay):
		}
	}
}

func (scanner *Scanner) start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		client := events.NewTzKT(scanner.wsURL)
		if err := client.Connect(ctx); err != nil {
			log.Err(err).Msg("Connect, retrying...")
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay):
				continue
			}
		}

		scanner.client = client
		if err := scanner.subscribe(); err != nil {
			log.Err(err).Msg("subscribe, retrying...")
			if client.IsConnected() {
				client.Close()
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay):
				continue
			}
		}

		log.Info().Msg("WebSocket connected and subscribed")

		// Block until the listener returns (connection lost or context cancelled)
		scanner.listen(ctx)

		// Clean up the old connection before reconnecting
		if scanner.client.IsConnected() {
			scanner.client.Close()
		}

		log.Warn().Msg("WebSocket listener stopped, reconnecting...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

// Close -
func (scanner *Scanner) Close() error {
	scanner.wg.Wait()

	if scanner.client != nil && scanner.client.IsConnected() {
		if err := scanner.client.Close(); err != nil {
			return err
		}
	}

	close(scanner.diffs)
	close(scanner.blocks)
	return nil
}

// BigMaps -
func (scanner *Scanner) BigMaps() <-chan Message {
	return scanner.diffs
}

// Blocks -
func (scanner *Scanner) Blocks() <-chan data.Block {
	return scanner.blocks
}

func (scanner *Scanner) subscribe() error {
	if err := scanner.client.SubscribeToBlocks(); err != nil {
		return err
	}

	if len(scanner.contracts) == 0 {
		if err := scanner.client.SubscribeToBigMaps(nil, "", "", events.BigMapTagMetadata, events.BigMapTagTokenMetadata); err != nil {
			return err
		}
	} else {
		for i := range scanner.contracts {
			if err := scanner.client.SubscribeToBigMaps(nil, scanner.contracts[i], "", events.BigMapTagMetadata, events.BigMapTagTokenMetadata); err != nil {
				return err
			}
		}
	}

	return nil
}

func (scanner *Scanner) listen(ctx context.Context) {
	watchdog := time.NewTimer(watchdogTimeout)
	defer watchdog.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-watchdog.C:
			log.Warn().Msg("no messages received within watchdog timeout, assuming connection lost")
			return
		case msg, ok := <-scanner.client.Listen():
			if !ok {
				log.Warn().Msg("message channel closed")
				return
			}
			watchdog.Reset(watchdogTimeout)

			switch msg.Type {
			case events.MessageTypeState:
				// on reconnect
				if msg.Channel != events.ChannelBlocks {
					continue
				}

				if scanner.level < msg.State {
					// if blocks was missed in some reason we should index missed blocks
					log.Warn().Uint64("old_state", scanner.level).Uint64("new_level", msg.State).Msg("detect missed blocks. resync...")

					if err := scanner.sync(ctx, msg.State); err != nil {
						log.Err(err).Msg("resync error")
						continue
					}
				}
				scanner.level = msg.State

			case events.MessageTypeData:
				switch msg.Channel {
				case events.ChannelBlocks:
					if err := scanner.handleBlocks(msg); err != nil {
						log.Err(err).Msg("handleBlocks")
					}
				case events.ChannelBigMap:
					if err := scanner.handleBigMaps(msg); err != nil {
						log.Err(err).Msg("handleBigMaps")
					}
				default:
					log.Error().Msgf("Unknown channel %s", msg.Channel)
				}
			case events.MessageTypeReorg, events.MessageTypeSubscribed:
			}
		}
	}
}

func (scanner *Scanner) sync(ctx context.Context, headLevel uint64) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			if headLevel <= scanner.level {
				if scanner.msg.Level > 0 {
					scanner.level = scanner.msg.Level
					scanner.diffs <- scanner.msg.copy()
					scanner.msg.clear()
				}
				return nil
			}

			// Cap the query range to avoid API timeouts on large level spans
			queryLevel := headLevel
			if queryLevel > scanner.level+syncLevelRange {
				queryLevel = scanner.level + syncLevelRange
			}

			updates, err := scanner.getSyncUpdates(ctx, queryLevel)
			if err != nil {
				log.Err(err).Msg("getSyncUpdates")
				time.Sleep(time.Second)
				continue
			}

			if len(updates) > 0 {
				scanner.processSyncUpdates(ctx, updates)
			} else {
				scanner.level = queryLevel
				scanner.lastID = 0
			}
		}
	}
}

func (scanner *Scanner) getSyncUpdates(ctx context.Context, headLevel uint64) ([]data.BigMapUpdate, error) {
	filters := map[string]string{
		"path.as":   "*metadata",
		"action.in": "add_key,update_key",
		"limit":     fmt.Sprintf("%d", pageSize),
		"level.le":  fmt.Sprintf("%d", headLevel),
		"sort.asc":  "id",
	}

	if scanner.lastID == 0 {
		filters["level.gt"] = fmt.Sprintf("%d", scanner.level)
	} else {
		filters["offset.cr"] = fmt.Sprintf("%d", scanner.lastID)
	}

	if len(scanner.contracts) > 0 {
		filters["contract.in"] = strings.Join(scanner.contracts, ",")
	}

	return scanner.api.GetBigmapUpdates(ctx, filters)
}

func (scanner *Scanner) processSyncUpdates(ctx context.Context, updates []data.BigMapUpdate) {
	for i := range updates {
		select {
		case <-ctx.Done():
			return
		default:
			if scanner.msg.Level != 0 && scanner.msg.Level != updates[i].Level {
				scanner.level = scanner.msg.Level
				scanner.diffs <- scanner.msg.copy()
				scanner.blocks <- data.Block{
					Level:     scanner.msg.Level,
					Timestamp: updates[i].Timestamp.UTC(),
				}
				scanner.msg.clear()
			}

			scanner.msg.Body = append(scanner.msg.Body, updates[i])
			scanner.msg.Level = updates[i].Level
			scanner.lastID = updates[i].ID
		}
	}
}

func (scanner *Scanner) handleBlocks(msg events.Message) error {
	body, ok := msg.Body.([]data.Block)
	if !ok {
		return errors.Errorf("Invalid body type: %T", msg.Body)
	}
	if len(body) == 0 {
		return errors.Errorf("Empty body: %v", body)
	}

	scanner.level = body[0].Level
	scanner.blocks <- body[0]
	return nil
}

func (scanner *Scanner) handleBigMaps(msg events.Message) error {
	body, ok := msg.Body.([]data.BigMapUpdate)
	if !ok {
		return errors.Errorf("Invalid body type: %T", msg.Body)
	}
	if len(body) == 0 {
		return nil
	}

	diffs := make([]data.BigMapUpdate, len(body))
	for i := range body {
		diffs[i] = data.BigMapUpdate{
			ID:        body[i].ID,
			Level:     body[i].Level,
			Timestamp: body[i].Timestamp,
			Bigmap:    body[i].Bigmap,
			Contract:  body[i].Contract,
			Path:      body[i].Path,
			Action:    body[i].Action,
		}

		if body[i].Content != nil {
			diffs[i].Content = &data.BigMapUpdateContent{
				Hash:  body[i].Content.Hash,
				Key:   body[i].Content.Key,
				Value: body[i].Content.Value,
			}
		}
	}

	scanner.diffs <- Message{
		Type:  msg.Type,
		Body:  diffs,
		Level: msg.State,
	}
	return nil
}
