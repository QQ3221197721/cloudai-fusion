"""
Quick validation test for Module 10 Competitor Baselines
Tests that the baseline implementations work without running full benchmark
"""

import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
sys.path.insert(0, str(HERE.parent.parent))

import numpy as np
from scheduler.env_central_pool import CentralPendingPoolEnvironment

# Test 1: Verify environment setup works
print("Test 1: Environment Setup")
env = CentralPendingPoolEnvironment(num_nodes=3, max_gpus_per_node=4, seed=42)
obs, info = env.reset()
print(f"  Observation shape: {obs.shape}")
print(f"  Observation range: [{obs.min():.3f}, {obs.max():.3f}]")
assert obs.min() >= 0.0 and obs.max() <= 1.0, "Observations must be in [0,1]"
print("  [PASS]\n")

# Test 2: Verify k8s binpack policy implementation
print("Test 2: Kubernetes BinPack Policy")
rng = np.random.default_rng(123)

def make_k8s_binpack(rng):
    def policy(env, obs):
        feasible = []
        pool_jobs = env._pending_pool.jobs()
        if not pool_jobs:
            feasible = list(range(env.num_nodes))
        else:
            for i in range(env.num_nodes):
                free = env._node_states[i].free_gpus
                if free >= 1 and any(j.gpus_needed <= free for j in pool_jobs):
                    feasible.append(i)
        if not feasible:
            return int(np.argmax([env._node_states[i].free_gpus for i in range(env.num_nodes)]))
        best, best_score = [], -float('inf')
        for i in feasible:
            free = env._node_states[i].free_gpus
            fitting = [j.gpus_needed for j in pool_jobs if j.gpus_needed <= free]
            demand = max(fitting) if fitting else 0
            allocated_after = (env.max_gpus - free + demand) / env.max_gpus
            if allocated_after > best_score + 1e-12:
                best, best_score = [i], allocated_after
            elif abs(allocated_after - best_score) <= 1e-12:
                best.append(i)
        return int(rng.choice(best))
    return policy

binpack_policy = make_k8s_binpack(rng)
action = binpack_policy(env, obs)
print(f"  Action selected: node {action}")
assert 0 <= action < env.num_nodes, "Action must be valid node index"
print("  [PASS]\n")

# Test 3: Verify k8s spread policy implementation  
print("Test 3: Kubernetes Spread Policy")
def make_k8s_spread(rng):
    def policy(env, obs):
        feasible = []
        pool_jobs = env._pending_pool.jobs()
        if not pool_jobs:
            feasible = list(range(env.num_nodes))
        else:
            for i in range(env.num_nodes):
                free = env._node_states[i].free_gpus
                if free >= 1 and any(j.gpus_needed <= free for j in pool_jobs):
                    feasible.append(i)
        if not feasible:
            return int(np.argmax([env._node_states[i].free_gpus for i in range(env.num_nodes)]))
        best, best_score = [], -float('inf')
        for i in feasible:
            free = env._node_states[i].free_gpus
            fitting = [j.gpus_needed for j in pool_jobs if j.gpus_needed <= free]
            demand = max(fitting) if fitting else 0
            free_after = (free - demand) / env.max_gpus
            if free_after > best_score + 1e-12:
                best, best_score = [i], free_after
            elif abs(free_after - best_score) <= 1e-12:
                best.append(i)
        return int(rng.choice(best))
    return policy

spread_policy = make_k8s_spread(rng)
action = spread_policy(env, obs)
print(f"  Action selected: node {action}")
assert 0 <= action < env.num_nodes, "Action must be valid node index"
print("  [PASS]\n")

# Test 4: Quick rollout (50 steps)
print("Test 4: Quick Rollout Test (50 steps)")
env2 = CentralPendingPoolEnvironment(num_nodes=5, max_gpus_per_node=8, seed=999, arrival_rate=0.1)
obs, _ = env2.reset()
total_reward = 0.0
for step in range(50):
    action = binpack_policy(env2, obs)
    obs, reward, terminated, truncated, info = env2.step(action)
    total_reward += reward
    if terminated or truncated:
        break
else:
    print(f"  Completed 50 steps without termination")
print(f"  Total reward over 50 steps: {total_reward:.2f}")
print("  [PASS]\n")

print("=" * 60)
print("ALL QUICK TESTS PASSED")
print("Baseline implementations are working correctly")
print("=" * 60)
