"""
CloudAI Fusion - Anomaly Detection Engine
Statistical and ML-based anomaly detection for cloud-native AI workloads.
Detects GPU utilization anomalies, memory leaks, performance degradation,
and security-related behavioral anomalies.
"""

from typing import Any, Dict, List, Optional, Tuple

import numpy as np
import structlog

from anomaly.deep_detector import _HAS_TORCH, DeepEnsembleDetector

logger = structlog.get_logger()


class TimeSeriesAnomalyDetector:
    """
    Multi-method anomaly detection engine combining:
    1. Statistical methods (Z-score, IQR)
    2. Moving average deviation
    3. Rate-of-change detection
    4. Seasonal decomposition
    """

    def __init__(self, window_size: int = 60, z_threshold: float = 3.0):
        self.window_size = window_size
        self.z_threshold = z_threshold
        self.history: Dict[str, List[float]] = {}
        logger.info("time_series_anomaly_detector_initialized", window_size=window_size, z_threshold=z_threshold)

    def add_datapoint(self, metric_name: str, value: float, timestamp: Optional[float] = None):
        """Add a data point to the metric history."""
        if metric_name not in self.history:
            self.history[metric_name] = []
        self.history[metric_name].append(value)
        # Keep only recent window
        if len(self.history[metric_name]) > self.window_size * 10:
            self.history[metric_name] = self.history[metric_name][-self.window_size * 5 :]

    def detect(self, metric_name: str, current_value: float) -> Dict[str, Any]:
        """Run multi-method anomaly detection on a metric."""
        result = {
            "metric": metric_name,
            "value": current_value,
            "is_anomaly": False,
            "methods": {},
            "severity": "normal",
            "confidence": 0.0,
        }

        history = self.history.get(metric_name, [])
        if len(history) < self.window_size:
            return result

        recent = np.array(history[-self.window_size :])

        # Method 1: Z-Score
        z_score = self._z_score_check(current_value, recent)
        result["methods"]["z_score"] = {
            "score": float(z_score),
            "threshold": self.z_threshold,
            "is_anomaly": abs(z_score) > self.z_threshold,
        }

        # Method 2: IQR (Interquartile Range)
        iqr_anomaly, iqr_score = self._iqr_check(current_value, recent)
        result["methods"]["iqr"] = {
            "score": float(iqr_score),
            "is_anomaly": iqr_anomaly,
        }

        # Method 3: Moving Average Deviation
        ma_anomaly, ma_deviation = self._moving_average_check(current_value, recent)
        result["methods"]["moving_average"] = {
            "deviation": float(ma_deviation),
            "is_anomaly": ma_anomaly,
        }

        # Method 4: Rate of Change
        roc_anomaly, roc_value = self._rate_of_change_check(recent)
        result["methods"]["rate_of_change"] = {
            "rate": float(roc_value),
            "is_anomaly": roc_anomaly,
        }

        # Ensemble decision: anomaly if 2+ methods agree
        anomaly_count = sum(
            [
                result["methods"]["z_score"]["is_anomaly"],
                result["methods"]["iqr"]["is_anomaly"],
                result["methods"]["moving_average"]["is_anomaly"],
                result["methods"]["rate_of_change"]["is_anomaly"],
            ]
        )

        result["is_anomaly"] = anomaly_count >= 2
        result["confidence"] = anomaly_count / 4.0

        if anomaly_count >= 3:
            result["severity"] = "critical"
        elif anomaly_count >= 2:
            result["severity"] = "warning"
        elif anomaly_count >= 1:
            result["severity"] = "info"

        return result

    def _z_score_check(self, value: float, data: np.ndarray) -> float:
        """Calculate Z-score for the current value."""
        mean = np.mean(data)
        std = np.std(data)
        if std == 0:
            return 0.0
        return (value - mean) / std

    def _iqr_check(self, value: float, data: np.ndarray) -> Tuple[bool, float]:
        """Check if value falls outside IQR bounds."""
        q1 = np.percentile(data, 25)
        q3 = np.percentile(data, 75)
        iqr = q3 - q1
        lower = q1 - 1.5 * iqr
        upper = q3 + 1.5 * iqr
        score = max(0, value - upper) + max(0, lower - value)
        return value < lower or value > upper, score

    def _moving_average_check(self, value: float, data: np.ndarray) -> Tuple[bool, float]:
        """Check deviation from exponential moving average."""
        alpha = 2.0 / (len(data) + 1)
        ema = data[0]
        for d in data[1:]:
            ema = alpha * d + (1 - alpha) * ema
        deviation = abs(value - ema) / (ema + 1e-8)
        return deviation > 0.3, deviation  # 30% deviation threshold

    def _rate_of_change_check(self, data: np.ndarray) -> Tuple[bool, float]:
        """Detect sudden rate changes in recent data."""
        if len(data) < 5:
            return False, 0.0
        recent_5 = data[-5:]
        rates = np.diff(recent_5)
        max_rate = np.max(np.abs(rates))
        avg_rate = np.mean(np.abs(np.diff(data)))
        if avg_rate == 0:
            return False, 0.0
        ratio = max_rate / (avg_rate + 1e-8)
        return ratio > 5.0, ratio  # 5x sudden change


class GPUAnomalyDetector:
    """Specialized anomaly detector for GPU workloads.

    Combines statistical methods (Z-score, IQR, EMA, RoC) with deep learning
    models (LSTM Autoencoder, Transformer) via DeepEnsembleDetector.
    Deep models are used when PyTorch is available; otherwise falls back
    to statistical-only detection.
    """

    INPUT_DIM = 4  # [utilization, memory_pct, temperature, power]
    DEEP_SEQ_LEN = 30

    def __init__(self, device: str = "cpu"):
        self.ts_detector = TimeSeriesAnomalyDetector(window_size=120)
        self.thermal_threshold = 85  # Celsius
        self.memory_leak_window = 300  # seconds

        # Deep learning ensemble (gracefully degrades when torch unavailable)
        self.deep_ensemble = DeepEnsembleDetector(
            input_dim=self.INPUT_DIM,
            seq_len=self.DEEP_SEQ_LEN,
            device=device,
        )
        # Multivariate window buffer: key → deque of [util, mem, temp, power]
        self._mv_windows: Dict[str, List[List[float]]] = {}

        logger.info("gpu_anomaly_detector_initialized", deep_learning_enabled=_HAS_TORCH, device=device)

    def analyze_gpu_metrics(
        self,
        node_name: str,
        gpu_index: int,
        utilization: float,
        memory_used_pct: float,
        temperature: float,
        power_watts: float,
    ) -> List[Dict[str, Any]]:
        """Analyze GPU metrics using statistical + deep learning ensemble."""
        anomalies = []
        prefix = f"{node_name}_gpu{gpu_index}"

        # Track metrics in statistical detector
        self.ts_detector.add_datapoint(f"{prefix}_util", utilization)
        self.ts_detector.add_datapoint(f"{prefix}_mem", memory_used_pct)
        self.ts_detector.add_datapoint(f"{prefix}_temp", temperature)
        self.ts_detector.add_datapoint(f"{prefix}_power", power_watts)

        # --- Statistical utilization anomaly ---
        stat_result = self.ts_detector.detect(f"{prefix}_util", utilization)

        # --- Deep learning ensemble ---
        deep_result = self._run_deep_ensemble(
            prefix, utilization, memory_used_pct, temperature, power_watts, stat_result
        )

        # Decide anomaly from ensemble
        if deep_result and deep_result.get("is_anomaly"):
            anomalies.append(
                {
                    "type": "gpu_anomaly_ensemble",
                    "node": node_name,
                    "gpu_index": gpu_index,
                    "value": utilization,
                    "severity": deep_result["severity"],
                    "confidence": deep_result["ensemble_score"],
                    "detection_method": "deep_ensemble",
                    "method_agreement": deep_result["method_agreement"],
                    "per_method_scores": deep_result.get("scores", {}),
                }
            )
        elif stat_result["is_anomaly"]:
            # Fallback: pure statistical (when deep models not warmed up)
            anomalies.append(
                {
                    "type": "gpu_utilization_anomaly",
                    "node": node_name,
                    "gpu_index": gpu_index,
                    "value": utilization,
                    "severity": stat_result["severity"],
                    "confidence": stat_result["confidence"],
                    "detection_method": "statistical",
                    "details": stat_result["methods"],
                }
            )

        # --- Thermal throttling risk ---
        if temperature > self.thermal_threshold:
            anomalies.append(
                {
                    "type": "thermal_throttling_risk",
                    "node": node_name,
                    "gpu_index": gpu_index,
                    "value": temperature,
                    "severity": "critical" if temperature > 90 else "warning",
                    "confidence": 0.95,
                }
            )

        # --- Memory leak detection (monotonic increase) ---
        mem_history = self.ts_detector.history.get(f"{prefix}_mem", [])
        if len(mem_history) > 60:
            recent = mem_history[-60:]
            if all(recent[i] <= recent[i + 1] for i in range(len(recent) - 1)):
                anomalies.append(
                    {
                        "type": "possible_memory_leak",
                        "node": node_name,
                        "gpu_index": gpu_index,
                        "value": memory_used_pct,
                        "severity": "high",
                        "confidence": 0.80,
                        "trend": "monotonic_increase",
                    }
                )

        return anomalies

    def _run_deep_ensemble(
        self,
        prefix: str,
        utilization: float,
        memory_pct: float,
        temperature: float,
        power_watts: float,
        stat_result: Dict[str, Any],
    ) -> Optional[Dict[str, Any]]:
        """Build multivariate window and run deep ensemble detection."""
        # Normalize features to [0, 1] range for neural networks
        feature_vec = [
            utilization / 100.0,
            memory_pct / 100.0,
            min(temperature / 100.0, 1.5),  # allow slight overshoot
            min(power_watts / 500.0, 1.5),
        ]

        if prefix not in self._mv_windows:
            self._mv_windows[prefix] = []
        self._mv_windows[prefix].append(feature_vec)

        # Keep bounded
        max_keep = self.DEEP_SEQ_LEN * 5
        if len(self._mv_windows[prefix]) > max_keep:
            self._mv_windows[prefix] = self._mv_windows[prefix][-max_keep:]

        # Need enough history for deep models
        if len(self._mv_windows[prefix]) < self.DEEP_SEQ_LEN:
            return None

        window = np.array(self._mv_windows[prefix], dtype=np.float32)
        return self.deep_ensemble.detect(window, statistical_result=stat_result)


# Singleton instance for use by the agent server
gpu_anomaly_detector = GPUAnomalyDetector()
