"""
CloudAI Fusion - Model Provenance producer (Moat B / M1, Python side).

This is the training-time producer for docs/verifiable-moat-spec.md M-B
(provenance-verifiable learning). It emits the CONTENT fields of a
cloudai-fusion ModelProvenance for a trained model; the Go control plane
(pkg/provenance) then Ed25519-SIGNS it and records a `model.provenance` receipt,
binding the weights to the exact signed, in-scope DatasetManifest they were
trained on. An auditor later runs `cafctl verify-model-provenance`.

The field names match the Go struct's JSON tags EXACTLY so the control plane can
ingest and sign without translation:

    base_model_hash, dataset_manifest_hash, train_config_hash, weights_hash,
    method, trainer, created_at

(id / key_id / signature are filled in by the signer, Go-side.) This module is
deliberately dependency-free (stdlib only) so it runs wherever training runs, with
or without torch / stable-baselines3.
"""

from __future__ import annotations

import hashlib
import json
from datetime import datetime, timezone
from typing import Any, Dict

# Hashes are hex-encoded SHA-256, matching the Go ledger's convention (raw hex, no
# prefix), so cross-language comparison is byte-exact.
_CHUNK = 1 << 16


def sha256_hex(data: bytes) -> str:
    """Hex SHA-256 of the given bytes."""
    return hashlib.sha256(data).hexdigest()


def hash_file(path: str) -> str:
    """Hex SHA-256 of a file's contents, streamed (weights may be large)."""
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(_CHUNK), b""):
            h.update(chunk)
    return h.hexdigest()


def canonical_config_hash(config: Dict[str, Any]) -> str:
    """
    Hex SHA-256 over the canonical JSON of a training config: sorted keys, compact
    separators, UTF-8, no HTML escaping - reproducible across runs and languages.
    """
    b = json.dumps(config, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    return sha256_hex(b)


# The exact key set the Go signer expects (its ModelProvenance content fields).
PROVENANCE_FIELDS = (
    "base_model_hash",
    "dataset_manifest_hash",
    "train_config_hash",
    "weights_hash",
    "method",
    "trainer",
    "created_at",
)


def build_model_provenance(
    *,
    base_model_hash: str,
    dataset_manifest_hash: str,
    weights_path: str,
    train_config: Dict[str, Any],
    method: str = "dpo",
    trainer: str = "advanced_trainer.py",
) -> Dict[str, Any]:
    """
    Build the unsigned ModelProvenance content for a completed training run.

    base_model_hash and dataset_manifest_hash are PASS-THROUGH inputs supplied by
    the control plane that started the run (the manifest hash is exactly what the
    Go DatasetManifest was committed as); weights_hash and train_config_hash are
    computed here from the produced artifacts. The result is ready to hand to the
    Go signer (provenance.SignProvenance + RecordProvenance).
    """
    if not base_model_hash:
        raise ValueError("base_model_hash is required")
    if not dataset_manifest_hash:
        raise ValueError("dataset_manifest_hash is required (bind to the signed corpus)")
    return {
        "base_model_hash": base_model_hash,
        "dataset_manifest_hash": dataset_manifest_hash,
        "train_config_hash": canonical_config_hash(train_config),
        "weights_hash": hash_file(weights_path),
        "method": method,
        "trainer": trainer,
        "created_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }


def write_model_provenance(path: str, **kwargs: Any) -> Dict[str, Any]:
    """Build the provenance content and write it as JSON to path; returns the dict."""
    prov = build_model_provenance(**kwargs)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(prov, f, indent=2, sort_keys=True)
    return prov
