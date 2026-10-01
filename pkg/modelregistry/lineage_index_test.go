// Package modelregistry implements Module 13 — the AI/ML model registry.
// This test file adds performance-focused optimizations: an in-memory lineage
// adjacency index for O(1) traversal, plus storage metrics tracking for deduplication ratio.

package modelregistry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// lineageIndex provides an in-memory adjacency index for fast ancestor/descendant
// traversal without reading JSON files from disk on every query. It is lazily
// loaded when the first Lineage() call happens and refreshed on Register/Rollback.
//
// Thread-safety: immutable snapshot semantics via RWMutex and copy-on-write.
type lineageIndex struct {
	mu            sync.RWMutex
	nodes         map[string]*modelNode     // keyed by "name:version"
	ancestors     map[string][]string       // parent pointers (one per node)
	descendants   map[string][]string       // children pointers (may be multi-valued for branching)
}

// modelNode holds a cached version record reference plus topology hints.
type modelNode struct {
	Name      string
	Version   string
	SHA256    string
	CreatedAt int64 // unix timestamp copy
	ParentRef string // "name:parent-version" or empty
}

// newLineageIndex creates an empty adjacency structure.
func newLineageIndex() *lineageIndex {
	return &lineageIndex{
		nodes:       make(map[string]*modelNode),
		ancestors:   make(map[string][]string),
		descendants: make(map[string][]string),
	}
}

// refresh loads or rebuilds the entire index from on-disk records. Callers must
// hold r.mu for safety since Index() may return different snapshots over time.
func (idx *lineageIndex) refresh(ctx context.Context, reg *FSRegistry, models []string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Reallocate fresh maps for true refresh.
	newIdx := newLineageIndex()

	for _, name := range models {
		arts, err := reg.listModel(name)
		if err != nil || len(arts) == 0 {
			continue
		}
		for _, art := range arts {
			refStr := ref(art.Name, art.Version)
			node := &modelNode{
				Name:      art.Name,
				Version:   art.Version,
				SHA256:    art.SHA256,
				CreatedAt: art.CreatedAt.Unix(),
				ParentRef: func() string {
					if art.Lineage.ParentVersion == "" {
						return ""
					}
					return ref(art.Name, art.Lineage.ParentVersion)
				}(),
			}
			newIdx.nodes[refStr] = node

			if node.ParentRef != "" {
				newIdx.ancestors[refStr] = []string{node.ParentRef}
				newIdx.descendants[node.ParentRef] = append(newIdx.descendants[node.ParentRef], refStr)
			} else {
				newIdx.ancestors[refStr] = nil
			}
		}
	}

	idx.nodes = newIdx.nodes
	idx.ancestors = newIdx.ancestors
	idx.descendants = newIdx.descendants
}

// getAncestors returns the direct parent(s) of ref in O(1). Empty slice if root.
func (idx *lineageIndex) getAncestors(ref string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if ancestors, ok := idx.ancestors[ref]; ok {
		return append([]string(nil), ancestors...)
	}
	return nil
}

// getDescendants returns all immediate child versions that claim `ref` as parent.
func (idx *lineageIndex) getDescendants(ref string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if descs, ok := idx.descendants[ref]; ok {
		return append([]string(nil), descs...)
	}
	return nil
}

// walkAncestorsFast walks up the parent chain using the in-memory index.
// Returns nodes newest->oldest, same order as original lineageLocked().
func (idx *lineageIndex) walkAncestorsFast(ctx context.Context, reg *FSRegistry, startRef string) ([]*ModelArtifact, error) {
	idx.mu.RLock()
	var path []*modelNode
	current := startRef
	visited := map[string]bool{}
	for current != "" {
		if visited[current] {
			idx.mu.RUnlock()
			return nil, cycleError(startRef)
		}
		visited[current] = true

		node, ok := idx.nodes[current]
		if !ok {
			idx.mu.RUnlock()
			// Fallback to disk read for missing node
			return idx.walkAncestorsDisk(ctx, reg, startRef)
		}
		path = append(path, node)
		parentRefs := idx.ancestors[current]
		if len(parentRefs) == 0 {
			break
		}
		current = parentRefs[0] // single parent assumption
	}
	idx.mu.RUnlock()

	// Now materialize full ModelArtifact records only for touched nodes.
	result := make([]*ModelArtifact, len(path))
	for i, node := range path {
		artifact, err := reg.getLocked(ctx, node.Name, node.Version)
		if err != nil {
			return nil, err
		}
		result[i] = artifact
	}
	return result, nil
}

// walkAncestorsDisk is the fallback implementation when index misses.
func (idx *lineageIndex) walkAncestorsDisk(ctx context.Context, reg *FSRegistry, startRef string) ([]*ModelArtifact, error) {
	name, ver := parseRef(startRef)
	root, err := reg.getLocked(ctx, name, ver)
	if err != nil {
		return nil, err
	}

	result := make([]*ModelArtifact, 0, 32)
	var walk func(art *ModelArtifact) error
	walk = func(art *ModelArtifact) error {
		result = append(result, art)
		if art.Lineage.ParentVersion == "" {
			return nil
		}
		parent, err := reg.getLocked(ctx, art.Name, art.Lineage.ParentVersion)
		if err != nil {
			return err
		}
		return walk(parent)
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return result, nil
}

// getBlobStats returns the number of unique blobs stored (content-addressed).
func (idx *lineageIndex) getBlobCount(reg *FSRegistry) (int, error) {
	blobsDir := filepath.Join(reg.root, blobsDir)
	entries, err := os.ReadDir(blobsDir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count, nil
}

// cycleError returns a lineage cycle detection error message.
func cycleError(startRef string) error {
	return fmt.Errorf("lineage cycle detected at %s", startRef)
}

// parseRef splits "name:version" into components. Assumes format is always valid.
func parseRef(refStr string) (name, version string) {
	i := strings.LastIndexByte(refStr, ':')
	if i <= 0 {
		return refStr, "latest"
	}
	return refStr[:i], refStr[i+1:]
}

