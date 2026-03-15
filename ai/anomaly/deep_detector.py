"""
CloudAI Fusion - Deep Learning Anomaly Detection Engine
Neural network-based anomaly detection for GPU workload metrics.

Models:
  1. LSTMAutoencoder  — sequence-to-sequence reconstruction error
  2. TransformerAnomalyDetector — multi-head attention anomaly scoring
  3. DeepEnsembleDetector — unifies statistical + deep methods

Architecture: stateless per-call scoring backed by online-trainable models.
All models gracefully degrade to statistical fallback when PyTorch is unavailable.
"""

from __future__ import annotations

import math
import os
from collections import deque
from typing import Any, Dict, Optional, Tuple

import numpy as np
import structlog

logger = structlog.get_logger()

# ---------------------------------------------------------------------------
# Optional PyTorch import (graceful degradation)
# ---------------------------------------------------------------------------
try:
    import torch
    import torch.nn as nn

    _HAS_TORCH = True
except ImportError:
    _HAS_TORCH = False
    logger.warning("torch_not_available", message="Deep anomaly detectors disabled, falling back to statistical")


# =============================================================================
# 1. LSTM Autoencoder
# =============================================================================


class _LSTMEncoder(nn.Module if _HAS_TORCH else object):
    """LSTM encoder that compresses a time window into a latent vector."""

    def __init__(self, input_dim: int = 4, hidden_dim: int = 64, latent_dim: int = 16, num_layers: int = 2):
        if not _HAS_TORCH:
            return
        super().__init__()
        self.lstm = nn.LSTM(input_dim, hidden_dim, num_layers=num_layers, batch_first=True, dropout=0.1)
        self.fc = nn.Linear(hidden_dim, latent_dim)

    def forward(self, x: "torch.Tensor") -> "torch.Tensor":
        _, (h_n, _) = self.lstm(x)
        return self.fc(h_n[-1])


class _LSTMDecoder(nn.Module if _HAS_TORCH else object):
    """LSTM decoder that reconstructs the time window from a latent vector."""

    def __init__(
        self, latent_dim: int = 16, hidden_dim: int = 64, output_dim: int = 4, seq_len: int = 30, num_layers: int = 2
    ):
        if not _HAS_TORCH:
            return
        super().__init__()
        self.seq_len = seq_len
        self.fc = nn.Linear(latent_dim, hidden_dim)
        self.lstm = nn.LSTM(hidden_dim, hidden_dim, num_layers=num_layers, batch_first=True, dropout=0.1)
        self.out = nn.Linear(hidden_dim, output_dim)

    def forward(self, z: "torch.Tensor") -> "torch.Tensor":
        h = self.fc(z).unsqueeze(1).repeat(1, self.seq_len, 1)
        lstm_out, _ = self.lstm(h)
        return self.out(lstm_out)


class LSTMAnomalyDetector:
    """
    LSTM Autoencoder for multivariate time-series anomaly detection.

    Training: online learning via sliding window reconstruction.
    Inference: high reconstruction error → anomaly.

    Input features (per timestep):
        [gpu_utilization, gpu_memory_pct, temperature, power_watts]
    """

    def __init__(
        self,
        input_dim: int = 4,
        hidden_dim: int = 64,
        latent_dim: int = 16,
        seq_len: int = 30,
        threshold_sigma: float = 3.0,
        lr: float = 1e-3,
        device: str = "cpu",
    ):
        self.input_dim = input_dim
        self.seq_len = seq_len
        self.threshold_sigma = threshold_sigma
        self.device = device
        self.enabled = _HAS_TORCH

        # Running statistics for adaptive threshold
        self._error_history: deque = deque(maxlen=2000)
        self._train_buffer: deque = deque(maxlen=500)
        self._train_step = 0
        self._online_interval = 50  # retrain every N samples

        if not self.enabled:
            return

        self.encoder = _LSTMEncoder(input_dim, hidden_dim, latent_dim).to(device)
        self.decoder = _LSTMDecoder(latent_dim, hidden_dim, input_dim, seq_len).to(device)

        params = list(self.encoder.parameters()) + list(self.decoder.parameters())
        self.optimizer = torch.optim.Adam(params, lr=lr)
        self.criterion = nn.MSELoss()

        logger.info("lstm_anomaly_detector_initialized", input_dim=input_dim, hidden_dim=hidden_dim, seq_len=seq_len)

    def _to_tensor(self, window: np.ndarray) -> "torch.Tensor":
        return torch.FloatTensor(window).unsqueeze(0).to(self.device)

    def _reconstruction_error(self, window: np.ndarray) -> float:
        """Compute per-sample reconstruction MSE."""
        x = self._to_tensor(window)
        self.encoder.eval()
        self.decoder.eval()
        with torch.no_grad():
            z = self.encoder(x)
            x_hat = self.decoder(z)
            err = torch.mean((x - x_hat) ** 2).item()
        return err

    def _online_train(self, batch: np.ndarray, epochs: int = 3):
        """Quick online training on recent normal data."""
        self.encoder.train()
        self.decoder.train()
        x = torch.FloatTensor(batch).to(self.device)
        for _ in range(epochs):
            z = self.encoder(x)
            x_hat = self.decoder(z)
            loss = self.criterion(x, x_hat)
            self.optimizer.zero_grad()
            loss.backward()
            torch.nn.utils.clip_grad_norm_(list(self.encoder.parameters()) + list(self.decoder.parameters()), 1.0)
            self.optimizer.step()

    def detect(self, window: np.ndarray) -> Dict[str, Any]:
        """
        Detect anomalies in a (seq_len, input_dim) window.

        Returns:
            {is_anomaly, score, threshold, reconstruction_error}
        """
        if not self.enabled or window.shape[0] < self.seq_len:
            return {"is_anomaly": False, "score": 0.0, "method": "lstm_autoencoder", "enabled": self.enabled}

        trimmed = window[-self.seq_len :]
        error = self._reconstruction_error(trimmed)
        self._error_history.append(error)

        # Adaptive threshold: mean + sigma * std
        errors = np.array(self._error_history)
        mean_err = np.mean(errors)
        std_err = np.std(errors) + 1e-8
        threshold = mean_err + self.threshold_sigma * std_err
        z_score = (error - mean_err) / std_err

        is_anomaly = error > threshold and len(self._error_history) > self.seq_len

        # Online learning on non-anomalous data
        if not is_anomaly:
            self._train_buffer.append(trimmed)
            self._train_step += 1
            if self._train_step % self._online_interval == 0 and len(self._train_buffer) >= 16:
                batch = np.array(list(self._train_buffer)[-32:])
                self._online_train(batch)

        return {
            "method": "lstm_autoencoder",
            "is_anomaly": is_anomaly,
            "reconstruction_error": float(error),
            "threshold": float(threshold),
            "z_score": float(z_score),
            "score": float(min(z_score / self.threshold_sigma, 1.0)) if z_score > 0 else 0.0,
        }

    def save(self, path: str):
        if not self.enabled:
            return
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        torch.save(
            {
                "encoder": self.encoder.state_dict(),
                "decoder": self.decoder.state_dict(),
                "error_history": list(self._error_history),
            },
            path,
        )
        logger.info("lstm_model_saved", path=path)

    def load(self, path: str):
        if not self.enabled or not os.path.exists(path):
            return False
        ckpt = torch.load(path, map_location=self.device, weights_only=False)
        self.encoder.load_state_dict(ckpt["encoder"])
        self.decoder.load_state_dict(ckpt["decoder"])
        self._error_history = deque(ckpt.get("error_history", []), maxlen=2000)
        logger.info("lstm_model_loaded", path=path)
        return True


# =============================================================================
# 2. Transformer Anomaly Detector
# =============================================================================


class _PositionalEncoding(nn.Module if _HAS_TORCH else object):
    """Sinusoidal positional encoding for transformer input."""

    def __init__(self, d_model: int, max_len: int = 512):
        if not _HAS_TORCH:
            return
        super().__init__()
        pe = torch.zeros(max_len, d_model)
        position = torch.arange(0, max_len, dtype=torch.float).unsqueeze(1)
        div_term = torch.exp(torch.arange(0, d_model, 2).float() * (-math.log(10000.0) / d_model))
        pe[:, 0::2] = torch.sin(position * div_term)
        if d_model > 1:
            pe[:, 1::2] = torch.cos(position * div_term[: d_model // 2])
        self.register_buffer("pe", pe.unsqueeze(0))

    def forward(self, x: "torch.Tensor") -> "torch.Tensor":
        return x + self.pe[:, : x.size(1)]


class _TransformerAutoencoder(nn.Module if _HAS_TORCH else object):
    """Transformer encoder with reconstruction head for anomaly detection."""

    def __init__(
        self,
        input_dim: int = 4,
        d_model: int = 64,
        nhead: int = 4,
        num_layers: int = 2,
        dim_feedforward: int = 128,
        dropout: float = 0.1,
    ):
        if not _HAS_TORCH:
            return
        super().__init__()
        self.input_proj = nn.Linear(input_dim, d_model)
        self.pos_enc = _PositionalEncoding(d_model)
        encoder_layer = nn.TransformerEncoderLayer(
            d_model=d_model,
            nhead=nhead,
            dim_feedforward=dim_feedforward,
            dropout=dropout,
            batch_first=True,
        )
        self.transformer = nn.TransformerEncoder(encoder_layer, num_layers=num_layers)
        self.output_proj = nn.Linear(d_model, input_dim)
        # Anomaly score head: uses CLS-like pooling → scalar
        self.anomaly_head = nn.Sequential(
            nn.Linear(d_model, d_model // 2),
            nn.ReLU(),
            nn.Linear(d_model // 2, 1),
            nn.Sigmoid(),
        )

    def forward(self, x: "torch.Tensor") -> Tuple["torch.Tensor", "torch.Tensor"]:
        h = self.pos_enc(self.input_proj(x))
        encoded = self.transformer(h)
        reconstruction = self.output_proj(encoded)
        # Pool over time dimension for anomaly score
        pooled = encoded.mean(dim=1)
        anomaly_score = self.anomaly_head(pooled).squeeze(-1)
        return reconstruction, anomaly_score


class TransformerAnomalyDetector:
    """
    Transformer-based anomaly detection with reconstruction + attention scoring.

    Advantages over LSTM:
    - Better at capturing long-range dependencies
    - Attention weights provide interpretability (which timestep is anomalous)
    - Parallelizable training
    """

    def __init__(
        self,
        input_dim: int = 4,
        d_model: int = 64,
        nhead: int = 4,
        num_layers: int = 2,
        seq_len: int = 30,
        threshold_sigma: float = 2.5,
        lr: float = 5e-4,
        device: str = "cpu",
    ):
        self.input_dim = input_dim
        self.seq_len = seq_len
        self.threshold_sigma = threshold_sigma
        self.device = device
        self.enabled = _HAS_TORCH

        self._error_history: deque = deque(maxlen=2000)
        self._train_buffer: deque = deque(maxlen=500)
        self._train_step = 0
        self._online_interval = 50

        if not self.enabled:
            return

        self.model = _TransformerAutoencoder(
            input_dim,
            d_model,
            nhead,
            num_layers,
        ).to(device)
        self.optimizer = torch.optim.AdamW(self.model.parameters(), lr=lr, weight_decay=1e-5)
        self.criterion = nn.MSELoss()

        logger.info("transformer_anomaly_detector_initialized", d_model=d_model, nhead=nhead, num_layers=num_layers)

    def _compute_scores(self, window: np.ndarray) -> Tuple[float, float]:
        """Compute reconstruction error and attention-based anomaly score."""
        x = torch.FloatTensor(window).unsqueeze(0).to(self.device)
        self.model.eval()
        with torch.no_grad():
            reconstruction, anomaly_score = self.model(x)
            recon_error = torch.mean((x - reconstruction) ** 2).item()
            attn_score = anomaly_score.item()
        return recon_error, attn_score

    def _online_train(self, batch: np.ndarray, epochs: int = 3):
        self.model.train()
        x = torch.FloatTensor(batch).to(self.device)
        # Generate labels: all normal (0.0) for online training
        labels = torch.zeros(x.size(0)).to(self.device)
        for _ in range(epochs):
            reconstruction, anomaly_score = self.model(x)
            recon_loss = self.criterion(x, reconstruction)
            score_loss = nn.functional.binary_cross_entropy(anomaly_score, labels)
            loss = recon_loss + 0.1 * score_loss
            self.optimizer.zero_grad()
            loss.backward()
            torch.nn.utils.clip_grad_norm_(self.model.parameters(), 1.0)
            self.optimizer.step()

    def detect(self, window: np.ndarray) -> Dict[str, Any]:
        """Detect anomalies using transformer reconstruction + attention score."""
        if not self.enabled or window.shape[0] < self.seq_len:
            return {"is_anomaly": False, "score": 0.0, "method": "transformer", "enabled": self.enabled}

        trimmed = window[-self.seq_len :]
        recon_error, attn_score = self._compute_scores(trimmed)
        self._error_history.append(recon_error)

        errors = np.array(self._error_history)
        mean_err = np.mean(errors)
        std_err = np.std(errors) + 1e-8
        threshold = mean_err + self.threshold_sigma * std_err
        z_score = (recon_error - mean_err) / std_err

        # Dual criteria: reconstruction error OR attention score
        recon_anomaly = recon_error > threshold and len(self._error_history) > self.seq_len
        attn_anomaly = attn_score > 0.7
        is_anomaly = recon_anomaly or attn_anomaly

        # Combined score
        combined_score = 0.5 * min(z_score / self.threshold_sigma, 1.0) + 0.5 * attn_score
        combined_score = max(0.0, min(1.0, combined_score))

        # Online learning on non-anomalous data
        if not is_anomaly:
            self._train_buffer.append(trimmed)
            self._train_step += 1
            if self._train_step % self._online_interval == 0 and len(self._train_buffer) >= 16:
                batch = np.array(list(self._train_buffer)[-32:])
                self._online_train(batch)

        return {
            "method": "transformer",
            "is_anomaly": is_anomaly,
            "reconstruction_error": float(recon_error),
            "attention_anomaly_score": float(attn_score),
            "combined_score": float(combined_score),
            "threshold": float(threshold),
            "z_score": float(z_score),
        }

    def save(self, path: str):
        if not self.enabled:
            return
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        torch.save(
            {
                "model": self.model.state_dict(),
                "error_history": list(self._error_history),
            },
            path,
        )

    def load(self, path: str):
        if not self.enabled or not os.path.exists(path):
            return False
        ckpt = torch.load(path, map_location=self.device, weights_only=False)
        self.model.load_state_dict(ckpt["model"])
        self._error_history = deque(ckpt.get("error_history", []), maxlen=2000)
        return True


# =============================================================================
# 3. Deep Ensemble Detector (unifies statistical + deep)
# =============================================================================


class DeepEnsembleDetector:
    """
    Production-grade ensemble combining statistical and deep learning methods.

    Voting strategy:
        - Each method produces a score in [0, 1]
        - Weighted average with configurable weights
        - Anomaly if combined score > threshold

    Methods:
        1. Statistical (Z-score + IQR + EMA + RoC) — from detector.py
        2. LSTM Autoencoder — reconstruction error
        3. Transformer — reconstruction + attention scoring
    """

    def __init__(
        self,
        input_dim: int = 4,
        seq_len: int = 30,
        weights: Optional[Dict[str, float]] = None,
        anomaly_threshold: float = 0.6,
        device: str = "cpu",
    ):
        self.seq_len = seq_len
        self.anomaly_threshold = anomaly_threshold

        # Method weights (statistical gets higher weight until DL models warm up)
        self.weights = weights or {
            "statistical": 0.40,
            "lstm": 0.30,
            "transformer": 0.30,
        }

        # Deep detectors
        self.lstm_detector = LSTMAnomalyDetector(
            input_dim=input_dim,
            seq_len=seq_len,
            device=device,
        )
        self.transformer_detector = TransformerAnomalyDetector(
            input_dim=input_dim,
            seq_len=seq_len,
            device=device,
        )

        # Adaptive weight adjustment
        self._correct_counts = {"statistical": 1, "lstm": 1, "transformer": 1}
        self._total_counts = {"statistical": 1, "lstm": 1, "transformer": 1}

        logger.info(
            "deep_ensemble_detector_initialized",
            weights=self.weights,
            threshold=anomaly_threshold,
            torch_available=_HAS_TORCH,
        )

    def detect(
        self,
        window: np.ndarray,
        statistical_result: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """
        Run ensemble anomaly detection.

        Args:
            window: (seq_len, input_dim) array of recent metrics.
            statistical_result: pre-computed result from TimeSeriesAnomalyDetector.

        Returns:
            Ensemble result with per-method breakdown.
        """
        method_results: Dict[str, Dict[str, Any]] = {}
        scores: Dict[str, float] = {}

        # 1. Statistical score (from caller or default 0)
        if statistical_result:
            stat_score = statistical_result.get("confidence", 0.0)
            method_results["statistical"] = statistical_result
            scores["statistical"] = stat_score
        else:
            scores["statistical"] = 0.0

        # 2. LSTM Autoencoder
        lstm_result = self.lstm_detector.detect(window)
        method_results["lstm"] = lstm_result
        scores["lstm"] = lstm_result.get("score", 0.0) if lstm_result.get("enabled", False) else 0.0

        # 3. Transformer
        tf_result = self.transformer_detector.detect(window)
        method_results["transformer"] = tf_result
        scores["transformer"] = tf_result.get("combined_score", 0.0) if tf_result.get("enabled", False) else 0.0

        # Weighted ensemble score
        total_weight = 0.0
        ensemble_score = 0.0
        for method, weight in self.weights.items():
            s = scores.get(method, 0.0)
            # Skip methods that are disabled (score stays 0, weight redistributed)
            if method != "statistical" and not _HAS_TORCH:
                continue
            ensemble_score += weight * s
            total_weight += weight

        if total_weight > 0:
            ensemble_score /= total_weight
        else:
            ensemble_score = scores.get("statistical", 0.0)

        is_anomaly = ensemble_score > self.anomaly_threshold

        # Determine severity
        if ensemble_score > 0.85:
            severity = "critical"
        elif ensemble_score > 0.65:
            severity = "high"
        elif ensemble_score > self.anomaly_threshold:
            severity = "warning"
        elif ensemble_score > 0.3:
            severity = "info"
        else:
            severity = "normal"

        # Agreement count
        method_votes = sum(1 for m, r in method_results.items() if r.get("is_anomaly", False))

        return {
            "is_anomaly": is_anomaly,
            "ensemble_score": float(ensemble_score),
            "severity": severity,
            "method_agreement": method_votes,
            "total_methods": len(method_results),
            "weights": self.weights,
            "per_method": method_results,
            "scores": scores,
            "torch_available": _HAS_TORCH,
        }

    def update_weights(self, method: str, was_correct: bool):
        """Adaptive weight adjustment based on feedback."""
        if method not in self._total_counts:
            return
        self._total_counts[method] += 1
        if was_correct:
            self._correct_counts[method] += 1

        # Recalculate weights based on accuracy
        accuracies = {}
        for m in self.weights:
            acc = self._correct_counts[m] / self._total_counts[m]
            accuracies[m] = acc

        total_acc = sum(accuracies.values())
        if total_acc > 0:
            for m in self.weights:
                self.weights[m] = accuracies[m] / total_acc

    def save(self, directory: str):
        os.makedirs(directory, exist_ok=True)
        self.lstm_detector.save(os.path.join(directory, "lstm_anomaly.pt"))
        self.transformer_detector.save(os.path.join(directory, "transformer_anomaly.pt"))
        logger.info("ensemble_models_saved", directory=directory)

    def load(self, directory: str):
        self.lstm_detector.load(os.path.join(directory, "lstm_anomaly.pt"))
        self.transformer_detector.load(os.path.join(directory, "transformer_anomaly.pt"))
        logger.info("ensemble_models_loaded", directory=directory)
