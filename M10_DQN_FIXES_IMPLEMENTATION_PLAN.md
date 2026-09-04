# M10 RL Scheduler Defect Fixes - Implementation Plan

**Date:** 2026/09/03  
**Module:** M10 - Deep RL Optimizer for GPU Scheduling  
**Status:** IMPLEMENTING  

---

## Verified Defects (From Previous Audit)

### Defect #1: trainStep Double-Increment Bug ✅ FIXED IN CODE
**Location:** Line 43 comment, line 276 in Train()  
**Problem:** globalStep and trainStep both incremented causing misaligned epsilon decay  
**Status:** Code has comment but may still have logic error

### Defect #2: Hard Copy Instead of Polyak Averaging ⚠️ PARTIAL FIX  
**Location:** Line 51-52 comment references tau parameter  
**Problem:** Line 186 uses `o.qNetwork.Copy()` (hard copy) instead of soft update  
**Status:** tau defined but never used

### Defect #3: Static Epsilon-Greedy Without Adaptive Exploration 🚨 CRITICAL
**Location:** Lines 198-221 SelectAction()  
**Problem:** No UCB bonus, no confidence-based adaptation, no convergence-aware decay  
**Status:** Not addressed at all

### Defect #4: Incomplete State Representation 🚨 BLOCKER  
**Location:** Lines 95-112 State struct  
**Problem:** Missing queue_depth[], memory_pressure[], gpu_topology graph  
**Status:** Only aggregate features (CurrentLoad, AvgWaitTime) present

### Defect #5: Single-Objective Reward Function 🚨 BLOCKER  
**Location:** calculateTDError(), reward calculation missing from this file  
**Problem:** Only throughput focused, no fairness/cost/energy objectives  
**Status:** Need to locate reward function

---

## Fix Plan (Priority Order)

### Priority 1: Defect #5 - Multi-Objective Reward Function
**Why first:** Training can't converge without good rewards

```python
def multi_objective_reward(throughput, fairness_gini, cost_efficiency, energy_savings):
    return 0.4*throughput + 0.3*fairness_gini + 0.2*cost_efficiency + 0.1*energy_savings
```

**Implementation Steps:**
1. Locate existing reward calculation in deep_rl_optimizer.go
2. Add fairness_gini calculation (1 - Gini coefficient of job completion times)
3. Add cost_efficiency calculation (baseline_cost / actual_cost)
4. Add energy_savings calculation (baseline_energy / actual_energy)
5. Implement weighted combination with configurable weights

### Priority 2: Defect #4 - Enhanced State Representation
**Why second:** Rewards need complete state to be meaningful

```python
class EnhancedStateSpace:
    def __init__(self):
        self.queue_depth = MultiDiscrete([100] * num_nodes)  # Pending jobs per node
        self.memory_pressure = Box(low=0, high=1, shape=(num_nodes,))  # % used + fragmentation
        self.gpu_topology = Graph(num_nodes, edge_attr=nvlink_distances)
        self.cluster_pressure = Scalar()  # Global resource contention indicator
```

**Implementation Steps:**
1. Modify State struct to add:
   - QueueDepth []float64 (per-node pending queue depth)
   - MemoryPressure []float64 (per-node memory fragmentation ratio)
   - GPUSchedulerTopology Graph encoding (adjacency matrix flattened)
   - ClusterPressure float64 (global contention indicator)
2. Update encodeState() to include these new features
3. Increase inputDim from 50 to 100+ dimensions
4. Update neural network architecture accordingly

### Priority 3: Defect #3 - Adaptive Exploration Strategy
**Why third:** Depends on working rewards and state

```python
class AdaptiveExploration:
    def __init__(self):
        self.epsilon_start = 1.0
        self.epsilon_end = 0.05
        self.epsilon_decay = 0.9995  # Decay over episodes
        self.ucb_alpha = 0.1  # Confidence bonus factor
    
    def select_action(self, state, q_values):
        if random.random() < self.epsilon:
            return random_action()  # Exploration
        else:
            return ucb_argmax(q_values, self.ucb_alpha)  # Exploitation with confidence
```

**Implementation Steps:**
1. Modify currentEpsilon decay formula in SelectAction()
2. Add UCB exploration as fallback when epsilon expires
3. Implement confidence-based action selection:
   - Track visit counts per (state, action) pair
   - Add sqrt(ln(N) / N_sa) bonus to Q-values
4. Add convergence check: stop exploring after sustained improvement plateau

### Priority 4: Defect #2 - Soft Target Network Update
**Why fourth:** Minor optimization

**Implementation Steps:**
1. Replace `o.qNetwork.Copy()` in initNetworks() with soft update loop
2. Use Polyak averaging: theta_target = tau*theta_main + (1-tau)*theta_target
3. Update every training step or every targetUpdateFreq steps

### Priority 5: Defect #1 - Fix trainStep Global Step Alignment
**Why fifth:** Minor fix once other defects resolved

**Implementation Steps:**
1. Verify globalStep increments only on action selection
2. Verify trainStep increments only on Train() calls
3. Use globalStep for epsilon decay, trainStep for training frequency checks

---

## Validation Checklist

After fixes, validate with:

1. **Training Convergence Test:**
   ```python
   # Train 100k episodes
   for episode in range(100_000):
       optimizer.step()
       
   # Check reward curve plateaus
   assert reward_change_per_episode < 0.001  # convergence threshold
   ```

2. **Baseline Comparison:**
   ```python
   # Compare against heuristics
   dasp_makespan = train_and_measure()
   round_robin_makespan = baseline_round_robin()
   k8s_default_makespan = baseline_k8s_default()
   
   # Verify >10% improvement
   assert (round_robin_makespan - dasp_makespan) / round_robin_makespan > 0.10
   ```

3. **Failure Injection Resilience:**
   ```python
   # Inject worker crashes during training
   for crash in range(10):
       inject_worker_crash(crash_timestamp)
       optimizer.step()
       
   # Verify zero catastrophic failures
   assert catastrophic_failures == 0
   ```

---

## Files to Modify

1. `pkg/scheduler/deep_rl_optimizer.go` - Core DQN implementation
2. `pkg/scheduler/state_encoding.go` - NEW FILE: Enhanced state representation
3. `pkg/scheduler/reward_functions.go` - NEW FILE: Multi-objective rewards
4. `pkg/scheduler/exploration_strategies.go` - NEW FILE: Adaptive exploration

---

## Estimated Timeline

| Phase | Tasks | Duration |
|-------|-------|----------|
| Research & Design | Analyze existing code, design solutions | 2 days |
| Implementation | Fix defects 5→1 in priority order | 5 days |
| Validation | Run 100k episode training, compare baselines | 3 days |
| Documentation | Update comments, create validation tests | 2 days |
| **Total** | | **12 days ≈ 2 weeks** |

---

*Generated: 2026/09/03 by Qoder DQN Expert Team*
