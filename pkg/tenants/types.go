// Package tenants provides multi-tenant GPU sharing management (Module 11 - Phase 2).
//
// This package builds on top of pkg/scheduler's GPUSharingManager to expose
// tenant-aware APIs for creating GPU pools and assigning MIG/MPS slices to tenants.
//
// Data Model:
//   - TenantPool: A pool of GPUs with a specific type (A100, H100) and mode (MIG or MPS)
//   - TenantMember: Individual tenant allocated resources within a pool
//   - MIGSlice: Represents a MIG instance slice assigned to a tenant
//
// Storage:
//   FS-based JSON persistence under <store>/tenants/v0.1.
//   Phase 2: Every write operation is attested through pkg/evidence.Ledger.
//   Actors: "cafctl-tenant" (CLI), all receipts Ed25519-signed and hash-chained.
package tenants

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// PoolMode defines how GPUs in the pool are shared
type PoolMode string

const (
	// PoolModeMIG uses Multi-Instance GPU (isolated slices, A100/H100)
	PoolModeMIG PoolMode = "mig"
	// PoolModeMPS uses Multi-Process Service (shared compute)
	PoolModeMPS PoolMode = "mps"
)

// TenantStatus represents lifecycle state of a tenant member
type TenantStatus string

const (
	// TenantStatusPending means allocation in progress
	TenantStatusPending TenantStatus = "pending"
	// TenantStatusActive means resources allocated and usable
	TenantStatusActive TenantStatus = "active"
	// TenantStatusSuspended means temporarily paused
	TenantStatusSuspended TenantStatus = "suspended"
	// TenantStatusDeleted means removed (kept for audit trail)
	TenantStatusDeleted TenantStatus = "deleted"
)

// TenantPool represents a GPU pool that hosts tenant workloads
type TenantPool struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	GPUType     string         `json:"gpu_type"`
	MigProfile  string         `json:"mig_profile,omitempty"`
	Mode        PoolMode       `json:"mode"`
	NodeIndex   int            `json:"node_index"`
	GPUIndices  []int          `json:"gpu_indices"`
	Status      string         `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Members     []TenantMember `json:"members"`
	TotalSlices int            `json:"total_slices"`
}

// TenantMember represents a tenant's allocation within a pool
type TenantMember struct {
	ID           string     `json:"id"`
	PoolID       string     `json:"pool_id"`
	Name         string     `json:"name"`
	UID          string     `json:"uid,omitempty"`
	Status       TenantStatus `json:"status"`
	ResourceMode string     `json:"resource_mode"` // "mig-slice" | "mps-share"
	MIGSlices    []MIGSlice `json:"mig_slices,omitempty"`
	MaxClients   int        `json:"max_clients,omitempty"`   // MPS only
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// MIGSlice represents a GPU slice from MIG partition
type MIGSlice struct {
	ID            string `json:"id"`
	MIGInstanceID string `json:"mig_instance_id"`
	GPUIndex      int    `json:"gpu_index"`
	Profile       string `json:"profile"`
	MemoryMB      int    `json:"memory_mb"`
	InUse         bool   `json:"in_use"`
}

// PoolInput is used when creating a new pool
type PoolInput struct {
	Name        string
	GPUType     string
	MigProfile  string
	Mode        PoolMode
	NodeIndex   int
	GPUIndices  []int
	TotalSlices int
}

// MemberInput is used when adding a tenant to a pool
type MemberInput struct {
	Name         string
	UID          string
	ResourceMode string
	Slices       int
	MaxClients   int
}

// Resource mode constants for MemberInput.ResourceMode
const (
	ResourceModeMIGSlice = "mig-slice"
	ResourceModeMPSShare = "mps-share"
)

// Store implements simple FS-backed JSON persistence for tenant pools.
type Store struct {
	basePath string
	mu       sync.RWMutex
	logger   *logrus.Entry
	pools    map[string]*TenantPool
}

// NewStore creates a store rooted at basePath (e.g. "./.caf").
func NewStore(basePath string) *Store {
	return &Store{
		basePath: basePath,
		logger:   logrus.WithField("component", "tenants.store"),
		pools:    make(map[string]*TenantPool),
	}
}

// poolsFile returns the JSON file path; path segments are fixed constants.
func (s *Store) poolsFile() string {
	return filepath.Join(s.basePath, "tenants", "v0.1", "pools.json")
}

// Load reads existing pools from disk; missing file means fresh start.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	file := s.poolsFile()
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh start
		}
		return fmt.Errorf("read tenant store %s: %w", file, err)
	}
	if err := json.Unmarshal(data, &s.pools); err != nil {
		return fmt.Errorf("parse tenant store %s: %w", file, err)
	}
	return nil
}

// Save persists all pools to disk atomically (write temp + rename).
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.poolsFile())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create tenant store dir: %w", err)
	}
	data, err := json.MarshalIndent(s.pools, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pools: %w", err)
	}
	file := s.poolsFile()
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tenant store: %w", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		return fmt.Errorf("commit tenant store: %w", err)
	}
	return nil
}
