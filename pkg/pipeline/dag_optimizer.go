// Package pipeline — Module 18: DAG Optimizer for parallelized ML workflows.
//
// This file adds a production-grade DAG optimization layer on top of the existing FSM designer:
//   - Critical Path (Longest Path) Algorithm via topological sort + dynamic programming
//   - Pipeline Partitioning under bandwidth/memory constraints to maximize throughput
//   - Fault-Tolerance Checkpoint Placement minimizing expected replay cost
//
// All algorithms run in-memory with deterministic results on Windows test hosts.
package pipeline

import (
	"math"
	"sort"
)

// ============================================================================
// DAG Task Graph Types
// ============================================================================

// DAGTask represents an individual unit of work within a larger workflow.
// Durations are expressed as normalized units (not wall-clock seconds).
type DAGTask struct {
	ID             string
	Duration       float64 // expected execution time
	MemoryMB       float64 // peak memory consumption during execution
	BandwidthInMBPS  float64 // data ingress rate requirement
	BandwidthOutMBPS float64 // data egress rate requirement
}

// DAG represents a directed acyclic graph of tasks.
// Adjacency lists store outgoing edges (from → children).
type DAG struct {
	tasks    map[string]*DAGTask
	children map[string][]string // fromID -> [toID, ...]
	parents  map[string][]string // toID -> [fromID, ...]
	index    map[string]int      // ID -> 0-based index
	nodes    []string            // sorted list of all node IDs
}

// NewDAG builds a DAG from a list of tasks and dependencies.
// deps[i] = [parent, child] meaning parent must complete before child starts.
func NewDAG(tasks []DAGTask, deps [][2]string) *DAG {
	taskMap := make(map[string]*DAGTask, len(tasks))
	for i := range tasks {
		t := tasks[i]
		taskMap[t.ID] = &t
	}

	n := len(tasks)
	children := make(map[string][]string, n)
	parents := make(map[string][]string, n)
	index := make(map[string]int, n)
	nodes := make([]string, n)

	for i, t := range tasks {
		children[t.ID] = make([]string, 0)
		parents[t.ID] = make([]string, 0)
		index[t.ID] = i
		nodes[i] = t.ID
	}

	for _, d := range deps {
		if len(d) != 2 {
			continue
		}
		parentID, childID := d[0], d[1]
		if p, ok := taskMap[parentID]; ok {
			if c, ok := taskMap[childID]; ok {
				_ = p
				_ = c
				children[parentID] = append(children[parentID], childID)
				parents[childID] = append(parents[childID], parentID)
			}
		}
	}

	return &DAG{
		tasks: taskMap,
		children: children,
		parents: parents,
		index: index,
		nodes: nodes,
	}
}

// TopologicalSort returns nodes in dependency order. Returns (order, valid=true).
func (g *DAG) TopologicalSort() ([]string, bool) {
	inDegree := make(map[string]int, len(g.nodes))
	for id := range g.children {
		inDegree[id] = 0
	}
	for _, ch := range g.children {
		for _, target := range ch {
			inDegree[target]++
		}
	}

	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	sort.Strings(queue)

	count := 0
	result := make([]string, 0, len(g.nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, id)
		count++

		for _, ch := range g.children[id] {
			inDegree[ch]--
			if inDegree[ch] == 0 {
				queue = append(queue, ch)
			}
		}
		sort.Strings(queue)
	}

	valid := count == len(g.tasks)
	return result, valid
}

// ============================================================================
// Critical Path API
// ============================================================================

// FindCriticalPath computes longest path duration and identifies critical nodes.
// Returns: (critical_path_nodes, makespan, earliest_finish_times, late_start_times).
func (g *DAG) FindCriticalPath() ([]string, float64, map[string]float64, map[string]float64) {
	order, ok := g.TopologicalSort()
	if !ok || len(order) == 0 {
		return nil, 0, nil, nil
	}

	earliestFinish := make(map[string]float64, len(g.tasks))
	lateStart := make(map[string]float64, len(g.tasks))

	var maxEF float64
	for _, id := range order {
		task := g.tasks[id]
		if task == nil {
			continue
		}
		ef := task.Duration
		for _, p := range g.parents[id] {
			pEf := earliestFinish[p]
			candidate := pEf + task.Duration
			if candidate > ef {
				ef = candidate
			}
		}
		earliestFinish[id] = ef
		if ef > maxEF {
			maxEF = ef
		}
	}

	if maxEF <= 0 {
		return nil, 0, nil, nil
	}

	reverseOrder := make([]string, len(order))
	copy(reverseOrder, order)
	for i, j := 0, len(reverseOrder)-1; i < j; i, j = i+1, j-1 {
		reverseOrder[i], reverseOrder[j] = reverseOrder[j], reverseOrder[i]
	}

	for _, id := range reverseOrder {
		task := g.tasks[id]
		if task == nil {
			continue
		}
		if len(g.children[id]) == 0 {
			lateStart[id] = maxEF - task.Duration
		} else {
			ls := math.MaxFloat64
			for _, ch := range g.children[id] {
				lch := lateStart[ch]
				val := lch - task.Duration
				if val < ls {
					ls = val
				}
			}
			lateStart[id] = ls
		}
	}

	criticalNodes := make([]string, 0)
	for id := range g.tasks {
		task := g.tasks[id]
		if task == nil {
			continue
		}
		ef := earliestFinish[id]
		ls := lateStart[id]
		slack := (ls + task.Duration) - ef
		if slack < epsilon && slack > -epsilon {
			criticalNodes = append(criticalNodes, id)
		}
	}
	sort.Strings(criticalNodes)

	return criticalNodes, maxEF, earliestFinish, lateStart
}

const epsilon = 1e-9

// ============================================================================
// Pipeline Partitioning API
// ============================================================================

// PartitionRequest captures resource constraints for optimal pipeline partitioning.
type PartitionRequest struct {
	TotalBandwidthMBPS float64
	TotalMemoryMB     float64
	NodeCount         int // number of parallel workers per stage
}

// PartitionPlan holds the result of optimization.
type PartitionPlan struct {
	Stages                [][]string // each stage is a parallelizable group of tasks
	Throughput            float64    // estimated ops/sec (inverse of makespan)
	Utilization           float64    // resource utilization heuristic [0,1]
	CriticalStageLength   float64    // longest stage duration
	TotalNodeDurationSum  float64    // sum of all task durations
	ParallelDepthEstimate int        // minimum number of stages
}

// OptimizePartition schedules tasks across stages respecting constraints.
// It greedily assigns tasks into minimum-length stages while honoring memory/bandwidth caps.
func OptimizePartition(tasks []DAGTask, deps [][2]string, req PartitionRequest) PartitionPlan {
	dag := NewDAG(tasks, deps)
	if dag == nil || len(tasks) == 0 {
		return PartitionPlan{}
	}

	order, ok := dag.TopologicalSort()
	if !ok {
		return PartitionPlan{}
	}

	stages := make([][]string, 0)
	currentStage := make([]string, 0)
	stageMem := 0.0
	stageBW := 0.0

	for _, id := range order {
		t := dag.tasks[id]
		if t == nil {
			continue
		}
		newMem := stageMem + t.MemoryMB
		newBW := stageBW + max(t.BandwidthInMBPS, t.BandwidthOutMBPS)

		if newMem <= req.TotalMemoryMB && newBW <= req.TotalBandwidthMBPS && len(currentStage) < req.NodeCount {
			currentStage = append(currentStage, id)
			stageMem += t.MemoryMB
			stageBW += max(t.BandwidthInMBPS, t.BandwidthOutMBPS)
		} else {
			if len(currentStage) > 0 {
				stages = append(stages, currentStage)
			}
			currentStage = []string{id}
			stageMem = t.MemoryMB
			stageBW = max(t.BandwidthInMBPS, t.BandwidthOutMBPS)
		}
	}
	if len(currentStage) > 0 {
		stages = append(stages, currentStage)
	}

	// Compute metrics
	var maxStageLen, totalNodeDur float64
	for _, s := range stages {
		stageLen := 0.0
		for _, id := range s {
			t := dag.tasks[id]
			if t != nil {
				stageLen += t.Duration
			}
		}
		totalNodeDur += stageLen
		if stageLen > maxStageLen {
			maxStageLen = stageLen
		}
	}

	util := 0.5
	if req.TotalBandwidthMBPS > 0 {
		util = min(util, stageBW/max(1.0, req.TotalBandwidthMBPS))
	}
	if req.TotalMemoryMB > 0 {
		memUtil := stageMem / req.TotalMemoryMB
		util = min(util, memUtil)
	}

	pp := PartitionPlan{
		Stages: stages, Throughput: 1.0 / max(1.0, maxStageLen), Utilization: util,
		CriticalStageLength: maxStageLen, TotalNodeDurationSum: totalNodeDur, ParallelDepthEstimate: len(stages),
	}
	return pp
}

// ============================================================================
// Fault-Tolerant Checkpoint Placement
// ============================================================================

// CheckpointConfig specifies failure rates and checkpoint overhead.
type CheckpointConfig struct {
	TaskFailureRate    float64   // probability of failure per unit time
	CheckpointOverhead float64   // cost (time) of checkpointing one stage
	RPLimit            float64   // maximum replay penalty per failure
}

// FindOptimalCheckpoints identifies which stages should have checkpoints
// to minimize total expected recovery cost under RPO constraint.
func FindOptimalCheckpoints(stages [][]string, durations []float64, cfg CheckpointConfig) []bool {
	if len(stages) == 0 || len(durations) == 0 {
		return nil
	}

	n := len(stages)
	checkpoints := make([]bool, n)
	replayCost := make([]float64, n+1)

	replayCost[0] = 0
	for i := 0; i < n; i++ {
		noCP := replayCost[i] + cfg.TaskFailureRate*durations[i] + cfg.CheckpointOverhead
		withCP := replayCost[i] + cfg.CheckpointOverhead
		if noCP <= withCP && replayCost[i] < cfg.RPLimit {
			continue
		} else {
			checkpoints[i] = true
			replayCost[i+1] = replayCost[i] + cfg.CheckpointOverhead
		}
	}

	return checkpoints
}
