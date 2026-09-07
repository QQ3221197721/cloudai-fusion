"""
CloudAI Fusion - Concept Drift Detection Engine

Implements statistical methods for detecting distribution shift in incoming data streams.
Core techniques:
- PSI (Population Stability Index) for feature distributions
- KL Divergence for label/prediction shifts
- Rolling window analysis with configurable thresholds
- Multi-feature batch processing

Author: CloudAI Fusion Security Team
Date: 2026-09-05
"""

import logging
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Any, Dict, List, Optional, Tuple
import numpy as np
from scipy import stats
from prometheus_client import Counter, Gauge, Histogram

logger = logging.getLogger(__name__)


# =============================================================================
# Metrics
# =============================================================================

DRIFT_DETECTIONS = Counter(
    "cloudai_drift_detections_total",
    "Total drift detections by type and severity",
    ["feature_name", "drift_type", "severity"],
)

DRIFT_LATENCY = Histogram(
    "cloudai_drift_detection_seconds",
    "Drift detection operation latency",
    buckets=[0.01, 0.05, 0.1, 0.5, 1.0, 5.0],
)

DRIFT_SEVERITY_GAUGE = Gauge(
    "cloudai_drift_severity_gauge",
    "Current maximum drift severity (0=stable, 1=warning, 2=critical)",
    ["model_name"],
)

BASELINE_SAMPLE_COUNT = Gauge(
    "cloudai_drift_baseline_samples",
    "Number of samples in baseline distribution",
    ["feature_name"],
)


# =============================================================================
# Enums & Data Models
# =============================================================================


class DriftSeverity(str, Enum):
    """Drift severity classification based on statistical thresholds"""
    STABLE = "stable"        # PSI < 0.1 - No action needed
    WARNING = "warning"      # 0.1 <= PSI < 0.2 - Monitor closely
    CRITICAL = "critical"    # PSI >= 0.2 - Model retraining recommended


@dataclass
class DriftResult:
    """
    Comprehensive drift detection result with multiple metrics
    
    Attributes:
        feature_name: Name of feature or label being analyzed
        psi: Population Stability Index (0 = identical distributions)
        kl_divergence: KL Divergence between prediction/actual distributions
        is_drifting: Boolean flag for critical drift
        severity: Categorized severity level
        timestamp: When detection was performed
        details: Additional diagnostic information
    """
    feature_name: str
    psi: float
    kl_divergence: float
    is_drifting: bool
    severity: DriftSeverity
    timestamp: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    details: Dict[str, Any] = None
    
    def __post_init__(self):
        if self.details is None:
            self.details = {}
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for JSON serialization"""
        return {
            "feature_name": self.feature_name,
            "psi": round(self.psi, 6),
            "kl_divergence": round(self.kl_divergence, 6),
            "is_drifting": self.is_drifting,
            "severity": self.severity.value,
            "timestamp": self.timestamp.isoformat(),
            "details": self.details or {},
        }


# =============================================================================
# Core Drift Detection Engine
# =============================================================================


class DriftDetector:
    """
    Statistical drift detection engine for ML pipeline monitoring.
    
    Detects:
    - Feature distribution shift via PSI (Population Stability Index)
    - Label/prediction distribution shift via KL Divergence
    - Batch-level drift across multiple features simultaneously
    
    Configuration:
    - window_size: Size of rolling window for current batch analysis
    - baseline_samples: Required historical samples for baseline establishment
    - thresholds: Custom PSI/KL thresholds (optional)
    
    Example:
        >>> detector = DriftDetector(window_size=1000, baseline_samples=10000)
        >>> 
        >>> # Initialize baseline from historical data
        >>> detector.update_baseline({
        ...     "gpu_utilization": historical_gpu_data,
        ...     "memory_usage": historical_mem_data
        ... })
        >>> 
        >>> # Detect drift in new batch
        >>> results = detector.batch_detect_drift(current_batch)
        >>> for name, result in results.items():
        ...     if result.is_drifting:
        ...         logger.warning(f"Drift detected: {result}")
    """
    
    def __init__(
        self,
        window_size: int = 1000,
        baseline_samples: int = 10000,
        psi_threshold_warning: float = 0.1,
        psi_threshold_critical: float = 0.2,
        kl_threshold_warning: float = 0.1,
        kl_threshold_critical: float = 0.5,
    ):
        """
        Initialize drift detector with configurable parameters.
        
        Args:
            window_size: Sample count for current batch analysis window
            baseline_samples: Minimum historical samples for baseline histogram
            psi_threshold_warning: PSI threshold for warning severity
            psi_threshold_critical: PSI threshold for critical severity
            kl_threshold_warning: KL divergence threshold for warning
            kl_threshold_critical: KL divergence threshold for critical
        """
        self.window_size = window_size
        self.baseline_samples = baseline_samples
        self.psi_threshold_warning = psi_threshold_warning
        self.psi_threshold_critical = psi_threshold_critical
        self.kl_threshold_warning = kl_threshold_warning
        self.kl_threshold_critical = kl_threshold_critical
        
        # Baseline storage: feature_name -> (histogram, bins)
        self.baseline_histograms: Dict[str, Tuple[np.ndarray, np.ndarray]] = {}
        
        # Tracking state
        self._detection_count = 0
        self._last_baseline_update: Optional[datetime] = None
        
        logger.info(
            "drift_detector_initialized",
            window_size=window_size,
            baseline_samples=baseline_samples,
        )
    
    def update_baseline(self, feature_data: Dict[str, np.ndarray]):
        """
        Update baseline distribution from historical reference data.
        
        Computes histograms for each feature using consistent binning strategy.
        Only updates features with sufficient sample count.
        
        Args:
            feature_data: Dict mapping feature names to 1D numpy arrays
                         Example: {"gpu_utilization": array([45.2, 67.8, ...])}
        
        Raises:
            ValueError: Insufficient samples for any feature
            
        Example:
            >>> # Load historical data from database or CSV
            >>> historical = {
            ...     "gpu_utilization": load_historical("gpu_data.csv"),
            ...     "request_latency": load_historical("latency.csv")
            ... }
            >>> detector.update_baseline(historical)
        """
        start_time = time.time()
        
        updated_features = []
        
        for name, data in feature_data.items():
            if not isinstance(data, np.ndarray):
                data = np.array(data)
            
            if len(data) < self.baseline_samples:
                logger.warning(
                    "insufficient_baseline_samples",
                    feature=name,
                    available=len(data),
                    required=self.baseline_samples,
                )
                continue
            
            # Compute histogram with fixed bin count for consistency
            hist, bins = np.histogram(data, bins=50, density=True)
            
            # Store normalized histogram for comparison
            self.baseline_histograms[name] = (hist, bins)
            
            BASELINE_SAMPLE_COUNT.labels(feature_name=name).set(len(data))
            updated_features.append(name)
            
            logger.debug(
                "baseline_updated",
                feature=name,
                sample_count=len(data),
                num_bins=50,
            )
        
        self._last_baseline_update = datetime.now(timezone.utc)
        self._detection_count = 0
        
        elapsed = time.time() - start_time
        logger.info(
            "baseline_update_complete",
            features_updated=len(updated_features),
            elapsed_seconds=round(elapsed, 3),
        )
        
        DRIFT_SEVERITY_GAUGE.labels(model_name="baseline").set(0)
    
    def detect_feature_drift(
        self,
        current_data: np.ndarray,
        baseline_hist: Tuple[np.ndarray, np.ndarray]
    ) -> DriftResult:
        """
        Calculate PSI (Population Stability Index) for single feature.
        
        PSI measures stability of distribution between two time periods.
        Interpretation:
        - PSI < 0.1: Stable (negligible change)
        - 0.1 ≤ PSI < 0.2: Moderate change (monitor)
        - PSI ≥ 0.2: Significant drift (investigate)
        
        Args:
            current_data: Current batch data (1D numpy array)
            baseline_hist: Tuple of (histogram, bins) from baseline
            
        Returns:
            DriftResult with PSI value and severity classification
        """
        start_time = time.time()
        
        hist, bins = baseline_hist
        
        # Compute current histogram using same bins
        current_hist, _ = np.histogram(current_data, bins=bins, density=True)
        
        # Calculate midpoints for integration
        mid_points = (bins[:-1] + bins[1:]) / 2
        
        # Convert to probabilities (area under histogram)
        expected = hist * np.diff(bins)
        actual = current_hist * np.diff(bins)
        
        # Add small epsilon for numerical stability (avoid division by zero)
        epsilon = 1e-10
        
        # Standard PSI formula: Σ((expected - actual) * ln(expected/actual))
        psi = np.sum((actual - expected) * np.log((expected + epsilon) / (actual + epsilon)))
        
        # Take absolute value for symmetric interpretation
        psi = abs(psi)
        
        # Determine severity based on thresholds
        if psi < self.psi_threshold_warning:
            severity = DriftSeverity.STABLE
        elif psi < self.psi_threshold_critical:
            severity = DriftSeverity.WARNING
        else:
            severity = DriftSeverity.CRITICAL
        
        # Prepare additional diagnostics
        details = {
            "sample_count": len(current_data),
            "mean_diff": float(np.mean(current_data) - np.mean(hist * np.diff(bins))),
            "std_diff": float(np.std(current_data) - np.std(hist * np.diff(bins))),
        }
        
        result = DriftResult(
            feature_name="",
            psi=float(psi),
            kl_divergence=0.0,
            is_drifting=severity == DriftSeverity.CRITICAL,
            severity=severity,
            details=details,
        )
        
        # Update metrics
        DRIFT_DETECTIONS.labels(
            feature_name="",
            drift_type="psi",
            severity=severity.value,
        ).inc()
        
        DRIFT_LATENCY.observe(time.time() - start_time)
        
        return result
    
    def detect_label_drift(
        self,
        predictions: np.ndarray,
        actuals: np.ndarray
    ) -> DriftResult:
        """
        KL-divergence based label distribution shift detection.
        
        Compares distribution of model predictions against ground truth labels.
        High KL divergence indicates model degradation in calibration.
        
        Args:
            predictions: Model output probabilities or class predictions
            actuals: Ground truth labels
            
        Returns:
            DriftResult with KL divergence metric
        """
        # Bin both distributions
        pred_bins = np.histogram(predictions, bins=20, density=True)[0]
        act_bins = np.histogram(actuals, bins=20, density=True)[0]
        
        # Normalize to probability distributions
        epsilon = 1e-10
        pred_norm = pred_bins / (pred_bins.sum() + epsilon)
        act_norm = act_bins / (act_bins.sum() + epsilon)
        
        # Symmetric KL divergence
        kl_div = np.sum(act_norm * np.log((act_norm + epsilon) / (pred_norm + epsilon)))
        
        # Ensure non-negative
        kl_div = max(0.0, kl_div)
        
        # Determine severity
        if kl_div < self.kl_threshold_warning:
            severity = DriftSeverity.STABLE
        elif kl_div < self.kl_threshold_critical:
            severity = DriftSeverity.WARNING
        else:
            severity = DriftSeverity.CRITICAL
        
        result = DriftResult(
            feature_name="labels",
            psi=0.0,
            kl_divergence=float(kl_div),
            is_drifting=severity == DriftSeverity.CRITICAL,
            severity=severity,
            details={
                "prediction_mean": float(np.mean(predictions)),
                "actual_mean": float(np.mean(actuals)),
                "sample_count": len(predictions),
            },
        )
        
        DRIFT_DETECTIONS.labels(
            feature_name="labels",
            drift_type="kl_divergence",
            severity=severity.value,
        ).inc()
        
        logger.info(
            "label_drift_detected",
            kl_divergence=result.kl_divergence,
            severity=severity.value,
        )
        
        return result
    
    def batch_detect_drift(
        self,
        current_batch: Dict[str, np.ndarray],
        model_name: str = "unknown"
    ) -> Dict[str, DriftResult]:
        """
        Detect drift across multiple features in a single batch.
        
        Features compared against baseline only if registered.
        All drifts are logged and aggregated.
        
        Args:
            current_batch: Dict of feature_name → data array
            model_name: Optional model identifier for metrics
            
        Returns:
            Dict mapping feature_name → DriftResult
            
        Example:
            >>> batch = {
            ...     "gpu_utilization": get_current_metrics("gpu"),
            ...     "cpu_load": get_current_metrics("cpu"),
            ...     "memory_free": get_current_metrics("mem")
            ... }
            >>> results = detector.batch_detect_drift(batch, "gpu-scheduler")
            >>> 
            >>> critical_drifts = [
            ...     (name, r) for name, r in results.items() if r.is_drifting
            ... ]
            >>> if critical_drifts:
            ...     logger.critical(f"{len(critical_drifts)} critical drifts detected!")
        """
        start_time = time.time()
        results = {}
        
        for feature_name, data in current_batch.items():
            if not isinstance(data, np.ndarray):
                data = np.array(data)
            
            if len(data) < self.window_size:
                logger.warning(
                    "insufficient_batch_samples",
                    feature=feature_name,
                    available=len(data),
                    required=self.window_size,
                )
                continue
            
            if feature_name in self.baseline_histograms:
                result = self.detect_feature_drift(data, self.baseline_histograms[feature_name])
                result.feature_name = feature_name
                
                results[feature_name] = result
                
                logger.info(
                    "drift_detected",
                    feature=feature_name,
                    psi=round(result.psi, 6),
                    severity=result.severity.value,
                )
                
                # Update gauge for this feature
                DRIFT_SEVERITY_GAUGE.labels(model_name=feature_name).set(
                    2 if result.severity == DriftSeverity.CRITICAL else
                    1 if result.severity == DriftSeverity.WARNING else 0
                )
        
        self._detection_count += 1
        
        total_time = time.time() - start_time
        logger.info(
            "batch_drift_detection_complete",
            model_name=model_name,
            features_analyzed=len(results),
            drifts_detected=sum(1 for r in results.values() if r.is_drifting),
            elapsed_ms=round(total_time * 1000, 2),
        )
        
        DRIFT_LATENCY.labels(operation="batch").observe(total_time)
        
        return results
    
    def get_drift_summary(self) -> Dict[str, Any]:
        """
        Generate summary statistics of recent drift detections.
        
        Returns:
            Summary dict with counts, latest severe drifts, last baseline time
        """
        stable_count = sum(
            1 for g in DRIFT_SEVERITY_GAUGE.collect()
            for m in g.samples if m.value == 0
        )
        
        warning_count = sum(
            1 for g in DRIFT_SEVERITY_GAUGE.collect()
            for m in g.samples if m.value == 1
        )
        
        critical_count = sum(
            1 for g in DRIFT_SEVERITY_GAUGE.collect()
            for m in g.samples if m.value == 2
        )
        
        return {
            "total_detections": self._detection_count,
            "baseline_sample_count": len(self.baseline_histograms),
            "features_monitored": list(self.baseline_histograms.keys()),
            "latest_baseline_update": self._last_baseline_update.isoformat() if self._last_baseline_update else None,
            "severity_summary": {
                "stable": stable_count,
                "warning": warning_count,
                "critical": critical_count,
            },
        }


if __name__ == "__main__":
    # Demo usage with synthetic data
    import numpy as np
    
    logging.basicConfig(level=logging.INFO)
    
    # Initialize detector
    detector = DriftDetector(window_size=500, baseline_samples=5000)
    
    # Create baseline from normal distribution
    np.random.seed(42)
    baseline_data = {
        "feature_a": np.random.normal(loc=50, scale=10, size=10000),
        "feature_b": np.random.uniform(low=0, high=100, size=10000),
    }
    
    print("=== Updating baseline ===")
    detector.update_baseline(baseline_data)
    
    # Test 1: Stable batch (same distribution)
    print("\n=== Test 1: Stable batch ===")
    stable_batch = {
        "feature_a": np.random.normal(loc=50, scale=10, size=500),
        "feature_b": np.random.uniform(low=0, high=100, size=500),
    }
    results = detector.batch_detect_drift(stable_batch)
    for name, result in results.items():
        print(f"{name}: PSI={result.psi:.4f}, Severity={result.severity.value}")
    
    # Test 2: Drifting batch (shifted distribution)
    print("\n=== Test 2: Drifting batch ===")
    drift_batch = {
        "feature_a": np.random.normal(loc=70, scale=15, size=500),  # Mean shifted
        "feature_b": np.random.uniform(low=50, high=100, size=500),  # Range shifted
    }
    results = detector.batch_detect_drift(drift_batch)
    for name, result in results.items():
        print(f"{name}: PSI={result.psi:.4f}, Severity={result.severity.value}, Drifting={result.is_drifting}")
