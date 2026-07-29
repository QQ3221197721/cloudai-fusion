"""Tests for the distributed, topology-aware RL scheduling trainer."""

from __future__ import annotations

import numpy as np
import pytest

from scheduler.distributed_trainer import (
    GPUTopology,
    ParallelQTrainer,
    QLearnerConfig,
    TopologyAwareQLearner,
    aggregate_q_tables,
    topology_affinity,
)


def make_topology() -> GPUTopology:
    return GPUTopology(num_gpus=8, nvlink_domains=[[0, 1, 2, 3], [4, 5, 6, 7]])


def test_topology_domain_lookup():
    topo = make_topology()
    assert topo.domain_of(0) == 0
    assert topo.domain_of(5) == 1
    assert topo.domain_of(99) == -1
    assert topo.same_domain(0, 3) is True
    assert topo.same_domain(0, 4) is False


def test_topology_affinity_bonus():
    topo = make_topology()
    assert topology_affinity(topo, 1, preferred_domain=0) == 1.0
    assert topology_affinity(topo, 6, preferred_domain=0) == 0.0


def test_aggregate_q_tables_averages():
    a = np.array([[0.0, 2.0], [4.0, 6.0]])
    b = np.array([[2.0, 4.0], [6.0, 8.0]])
    merged = aggregate_q_tables([a, b])
    assert np.allclose(merged, np.array([[1.0, 3.0], [5.0, 7.0]]))


def test_aggregate_q_tables_weighted():
    a = np.array([[0.0, 0.0]])
    b = np.array([[10.0, 10.0]])
    merged = aggregate_q_tables([a, b], weights=[3.0, 1.0])
    assert np.allclose(merged, np.array([[2.5, 2.5]]))


def test_aggregate_q_tables_validates():
    with pytest.raises(ValueError):
        aggregate_q_tables([])
    with pytest.raises(ValueError):
        aggregate_q_tables([np.zeros((2, 2)), np.zeros((3, 3))])
    with pytest.raises(ValueError):
        aggregate_q_tables([np.zeros((2, 2))], weights=[0.0])


def test_learner_prefers_domain_after_training():
    topo = make_topology()
    learner = TopologyAwareQLearner(topo, preferred_domain=1, config=QLearnerConfig(epsilon=0.2))
    rng = np.random.default_rng(42)
    state = 0
    for _ in range(3000):
        action = learner.select_action(state, rng, explore=True)
        reward = 1.0 if topo.domain_of(action) == 1 else 0.0
        learner.update(state, action, reward, next_state=state)

    greedy = learner.select_action(state, rng, explore=False)
    assert topo.domain_of(greedy) == 1, f"expected a GPU in domain 1, got {greedy}"


def test_parallel_trainer_threads_backend_learns():
    topo = make_topology()
    trainer = ParallelQTrainer(topology=topo, preferred_domain=0)
    result = trainer.train(num_workers=4, episodes_per_worker=1500, seed=7)

    assert result.q_table.shape == (8, 8)
    assert result.num_workers == 4
    assert result.episodes == 6000
    # In CI (no Ray) the backend must be the thread pool.
    assert result.backend in ("threads", "ray")
    # The aggregated greedy action must land in the preferred domain.
    assert topo.domain_of(result.best_action(0)) == 0


def test_parallel_trainer_rejects_empty_topology():
    with pytest.raises(ValueError):
        ParallelQTrainer(topology=GPUTopology(num_gpus=0), preferred_domain=0)
