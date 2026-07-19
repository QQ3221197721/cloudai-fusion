"""
Tests for the ModelProvenance producer (Moat B / M1, Python side).

Verifies the emitted content matches the Go signer's expected field set, hashes
the produced artifacts correctly and reproducibly, and validates its inputs -
so the Go control plane can sign it into a `model.provenance` receipt unchanged.
"""

import hashlib
import json

import pytest

from scheduler.provenance import (
    PROVENANCE_FIELDS,
    build_model_provenance,
    canonical_config_hash,
    hash_file,
    write_model_provenance,
)


def _weights(tmp_path, data: bytes = b"fake-weights-blob"):
    p = tmp_path / "weights.bin"
    p.write_bytes(data)
    return str(p)


def test_build_matches_go_field_set(tmp_path):
    wpath = _weights(tmp_path)
    prov = build_model_provenance(
        base_model_hash="qwen2.5-7b",
        dataset_manifest_hash="deadbeef",
        weights_path=wpath,
        train_config={"lr": 3e-4, "epochs": 3, "beta": 0.1},
        method="dpo",
        trainer="advanced_trainer.py",
    )
    # Exactly the content fields the Go ModelProvenance signer expects - no more, no less.
    assert set(prov.keys()) == set(PROVENANCE_FIELDS)
    assert prov["base_model_hash"] == "qwen2.5-7b"
    assert prov["dataset_manifest_hash"] == "deadbeef"
    assert prov["method"] == "dpo"
    assert prov["trainer"] == "advanced_trainer.py"
    assert prov["created_at"].endswith("Z")


def test_weights_hash_is_file_sha256(tmp_path):
    data = b"the-actual-weights"
    wpath = _weights(tmp_path, data)
    prov = build_model_provenance(
        base_model_hash="b",
        dataset_manifest_hash="m",
        weights_path=wpath,
        train_config={},
    )
    assert prov["weights_hash"] == hashlib.sha256(data).hexdigest()
    assert hash_file(wpath) == prov["weights_hash"]


def test_config_hash_is_deterministic_and_order_independent():
    a = canonical_config_hash({"lr": 0.1, "epochs": 3})
    b = canonical_config_hash({"epochs": 3, "lr": 0.1})  # different insertion order
    assert a == b  # canonical (sorted keys) -> identical
    assert a != canonical_config_hash({"lr": 0.2, "epochs": 3})  # different value -> different


def test_requires_manifest_and_base_binding(tmp_path):
    wpath = _weights(tmp_path)
    with pytest.raises(ValueError):
        build_model_provenance(base_model_hash="", dataset_manifest_hash="m", weights_path=wpath, train_config={})
    with pytest.raises(ValueError):
        build_model_provenance(base_model_hash="b", dataset_manifest_hash="", weights_path=wpath, train_config={})


def test_write_roundtrips_to_json(tmp_path):
    wpath = _weights(tmp_path)
    out = str(tmp_path / "provenance.json")
    prov = write_model_provenance(
        out,
        base_model_hash="b",
        dataset_manifest_hash="m",
        weights_path=wpath,
        train_config={"lr": 1e-4},
    )
    with open(out, "r", encoding="utf-8") as f:
        loaded = json.load(f)
    assert loaded == prov
    assert set(loaded.keys()) == set(PROVENANCE_FIELDS)
