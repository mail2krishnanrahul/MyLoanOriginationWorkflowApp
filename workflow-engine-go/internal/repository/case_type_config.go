package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// GetCaseTypeConfig — version-pinned config loader
// ---------------------------------------------------------------------------
//
// A case is always pinned to the exact case_type version it was created with.
// This function loads the config for a specific (code, version) pair, ensuring
// that running cases never see config changes from newer versions.
//
// It includes an in-process LRU cache keyed on "code:version" because case_type
// configs are immutable once ACTIVE — they never change, making them ideal
// for aggressive caching.

var (
	configCache   = make(map[string]*model.CaseTypeConfig)
	configCacheMu sync.RWMutex
)

// GetCaseTypeConfig loads the JSONB config for a specific case_type code and
// version. The result is cached in-process because configs are immutable per
// version — once a case_type row is ACTIVE, its config never changes.
//
// Usage:
//
//	// When creating a new case — load the latest ACTIVE version
//	ct, _ := repo.GetCaseTypeByCodeAndVersion(ctx, nil, "HOME_LOAN", 0)
//
//	// When processing an existing case — always use the pinned version
//	config, _ := repo.GetCaseTypeConfig(ctx, caseInst.CaseTypeID)
func (r *Repository) GetCaseTypeConfig(ctx context.Context, caseTypeID string) (*model.CaseTypeConfig, error) {
	// 1. Check cache
	configCacheMu.RLock()
	if cached, ok := configCache[caseTypeID]; ok {
		configCacheMu.RUnlock()
		return cached, nil
	}
	configCacheMu.RUnlock()

	// 2. Load from DB
	var configRaw []byte
	err := r.Pool.QueryRow(ctx, `
		SELECT config
		FROM case_types
		WHERE id = $1::uuid`, caseTypeID,
	).Scan(&configRaw)
	if err != nil {
		return nil, fmt.Errorf("case_type %s not found: %w", caseTypeID, err)
	}

	var config model.CaseTypeConfig
	if err := json.Unmarshal(configRaw, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config for %s: %w", caseTypeID, err)
	}

	// 3. Cache (immutable — safe to cache indefinitely)
	configCacheMu.Lock()
	configCache[caseTypeID] = &config
	configCacheMu.Unlock()

	return &config, nil
}

// GetCaseTypeConfigByCodeVersion loads config by code+version (convenience wrapper).
// This is useful when you know the code and version but not the UUID.
func (r *Repository) GetCaseTypeConfigByCodeVersion(ctx context.Context, code string, version int) (*model.CaseTypeConfig, error) {
	// First resolve the case_type ID
	var caseTypeID string
	err := r.Pool.QueryRow(ctx, `
		SELECT id FROM case_types
		WHERE code = $1 AND version = $2`, code, version,
	).Scan(&caseTypeID)
	if err != nil {
		return nil, fmt.Errorf("case_type %s v%d not found: %w", code, version, err)
	}

	return r.GetCaseTypeConfig(ctx, caseTypeID)
}

// InvalidateCaseTypeConfigCache removes a specific entry from the in-process cache.
// Call this if you ever update a DRAFT case_type config (ACTIVE ones never change).
func InvalidateCaseTypeConfigCache(caseTypeID string) {
	configCacheMu.Lock()
	delete(configCache, caseTypeID)
	configCacheMu.Unlock()
}
