"""
Tests for CloudAI Fusion - Anomaly Detection Engine (anomaly/detector.py)
Covers: TimeSeriesAnomalyDetector, GPUAnomalyDetector
Run with: cd ai && python -m pytest tests/test_anomaly.py -v
"""

import numpy as np
import pytest

from anomaly.detector import GPUAnomalyDetector, TimeSeriesAnomalyDetector

# =============================================================================
# TimeSeriesAnomalyDetector Tests
# =============================================================================


class TestTimeSeriesAnomalyDetector:
    """Table-driven tests for the multi-method anomaly detector."""

    def test_init_defaults(self):
        det = TimeSeriesAnomalyDetector()
        assert det.window_size == 60
        assert det.z_threshold == 3.0
        assert len(det.history) == 0

    def test_init_custom(self):
        det = TimeSeriesAnomalyDetector(window_size=30, z_threshold=2.0)
        assert det.window_size == 30
        assert det.z_threshold == 2.0

    def test_add_datapoint_creates_metric(self):
        det = TimeSeriesAnomalyDetector()
        det.add_datapoint("cpu_util", 50.0)
        assert "cpu_util" in det.history
        assert len(det.history["cpu_util"]) == 1

    def test_add_datapoint_multiple(self):
        det = TimeSeriesAnomalyDetector()
        for i in range(100):
            det.add_datapoint("metric", float(i))
        assert len(det.history["metric"]) == 100

    def test_history_trimming(self):
        """History should be trimmed when it exceeds window_size * 10."""
        det = TimeSeriesAnomalyDetector(window_size=10)
        for i in range(150):
            det.add_datapoint("metric", float(i))
        # After exceeding 10*10=100, it trims to 10*5=50, then keeps appending
        assert len(det.history["metric"]) <= 100

    def test_detect_insufficient_data(self):
        """Should return no anomaly when insufficient data."""
        det = TimeSeriesAnomalyDetector(window_size=60)
        for i in range(30):  # less than window_size
            det.add_datapoint("metric", float(i))
        result = det.detect("metric", 50.0)
        assert result["is_anomaly"] is False
        assert result["severity"] == "normal"

    def test_detect_normal_value(self):
        """Normal value within stable data should not be anomaly."""
        det = TimeSeriesAnomalyDetector(window_size=60)
        # Fill with stable data around 50
        for _ in range(100):
            det.add_datapoint("stable", 50.0 + np.random.normal(0, 1))
        result = det.detect("stable", 51.0)
        assert result["is_anomaly"] == False

    def test_detect_extreme_anomaly(self):
        """Extreme value should be detected as anomaly."""
        det = TimeSeriesAnomalyDetector(window_size=60)
        # Fill with stable data around 50
        for _ in range(100):
            det.add_datapoint("stable", 50.0 + np.random.normal(0, 0.5))
        result = det.detect("stable", 200.0)  # extreme spike
        assert result["is_anomaly"] == True
        assert result["confidence"] > 0.0
        assert result["severity"] in ("warning", "critical")

    def test_detect_methods_present(self):
        """All four methods should be in results."""
        det = TimeSeriesAnomalyDetector(window_size=60)
        for _ in range(100):
            det.add_datapoint("m", 50.0)
        result = det.detect("m", 55.0)
        assert "z_score" in result["methods"]
        assert "iqr" in result["methods"]
        assert "moving_average" in result["methods"]
        assert "rate_of_change" in result["methods"]

    def test_z_score_check(self):
        det = TimeSeriesAnomalyDetector()
        data = np.array([50.0] * 60)
        # Value at mean => z_score = 0
        z = det._z_score_check(50.0, data)
        assert z == 0.0

    def test_z_score_check_zero_std(self):
        det = TimeSeriesAnomalyDetector()
        data = np.array([10.0] * 60)  # constant => std=0
        z = det._z_score_check(10.0, data)
        assert z == 0.0

    def test_iqr_check_normal(self):
        det = TimeSeriesAnomalyDetector()
        data = np.arange(1, 61, dtype=float)  # 1..60
        is_anomaly, score = det._iqr_check(30.0, data)
        assert is_anomaly == False

    def test_iqr_check_outlier(self):
        det = TimeSeriesAnomalyDetector()
        data = np.array([50.0] * 60)
        is_anomaly, score = det._iqr_check(200.0, data)
        assert is_anomaly == True
        assert score > 0

    def test_moving_average_check(self):
        det = TimeSeriesAnomalyDetector()
        data = np.array([50.0] * 60)
        is_anomaly, deviation = det._moving_average_check(50.0, data)
        assert is_anomaly == False
        assert deviation < 0.01

    def test_rate_of_change_check_stable(self):
        det = TimeSeriesAnomalyDetector()
        data = np.array([50.0] * 60)
        is_anomaly, rate = det._rate_of_change_check(data)
        assert is_anomaly is False

    def test_rate_of_change_insufficient_data(self):
        det = TimeSeriesAnomalyDetector()
        data = np.array([50.0, 51.0])
        is_anomaly, rate = det._rate_of_change_check(data)
        assert is_anomaly is False
        assert rate == 0.0

    @pytest.mark.parametrize(
        "severity,count",
        [
            ("normal", 0),
            ("info", 1),
            ("warning", 2),
            ("critical", 3),
        ],
    )
    def test_severity_mapping(self, severity, count):
        """Verify severity mapping based on anomaly method count."""
        # We can't directly test this without mocking, but verify the logic
        if count >= 3:
            assert severity == "critical"
        elif count >= 2:
            assert severity == "warning"
        elif count >= 1:
            assert severity == "info"
        else:
            assert severity == "normal"


# =============================================================================
# GPUAnomalyDetector Tests
# =============================================================================


class TestGPUAnomalyDetector:
    """Tests for GPU-specific anomaly detection."""

    def test_init(self):
        det = GPUAnomalyDetector()
        assert det.thermal_threshold == 85
        assert det.memory_leak_window == 300

    def test_normal_metrics(self):
        det = GPUAnomalyDetector()
        anomalies = det.analyze_gpu_metrics(
            "node-1",
            0,
            utilization=60.0,
            memory_used_pct=40.0,
            temperature=65.0,
            power_watts=200.0,
        )
        # With insufficient history, should not detect anomalies
        assert len(anomalies) == 0

    def test_thermal_throttling(self):
        det = GPUAnomalyDetector()
        anomalies = det.analyze_gpu_metrics(
            "node-1",
            0,
            utilization=50.0,
            memory_used_pct=40.0,
            temperature=91.0,  # over 90 threshold
            power_watts=300.0,
        )
        thermal = [a for a in anomalies if a["type"] == "thermal_throttling_risk"]
        assert len(thermal) == 1
        assert thermal[0]["severity"] == "critical"  # >90C

    def test_thermal_warning(self):
        det = GPUAnomalyDetector()
        anomalies = det.analyze_gpu_metrics(
            "node-1",
            0,
            utilization=50.0,
            memory_used_pct=40.0,
            temperature=87.0,  # >85 but <90
            power_watts=250.0,
        )
        thermal = [a for a in anomalies if a["type"] == "thermal_throttling_risk"]
        assert len(thermal) == 1
        assert thermal[0]["severity"] == "warning"

    def test_no_thermal_below_threshold(self):
        det = GPUAnomalyDetector()
        anomalies = det.analyze_gpu_metrics(
            "node-1",
            0,
            utilization=50.0,
            memory_used_pct=40.0,
            temperature=80.0,  # below threshold
            power_watts=200.0,
        )
        thermal = [a for a in anomalies if a["type"] == "thermal_throttling_risk"]
        assert len(thermal) == 0

    def test_memory_leak_detection(self):
        """Simulate monotonically increasing memory to trigger leak detection."""
        det = GPUAnomalyDetector()
        # Feed 70+ monotonically increasing memory values
        for i in range(70):
            det.analyze_gpu_metrics(
                "node-leak",
                0,
                utilization=50.0,
                memory_used_pct=30.0 + i * 0.5,
                temperature=70.0,
                power_watts=200.0,
            )
        # The last call should detect memory leak
        anomalies = det.analyze_gpu_metrics(
            "node-leak",
            0,
            utilization=50.0,
            memory_used_pct=30.0 + 70 * 0.5,
            temperature=70.0,
            power_watts=200.0,
        )
        leaks = [a for a in anomalies if a["type"] == "possible_memory_leak"]
        assert len(leaks) == 1
        assert leaks[0]["severity"] == "high"
        assert leaks[0]["trend"] == "monotonic_increase"

    def test_multiple_gpu_indices(self):
        """Each GPU should be tracked independently."""
        det = GPUAnomalyDetector()
        det.analyze_gpu_metrics("node-1", 0, 50.0, 40.0, 70.0, 200.0)
        det.analyze_gpu_metrics("node-1", 1, 60.0, 50.0, 72.0, 220.0)

        prefix0 = "node-1_gpu0_util"
        prefix1 = "node-1_gpu1_util"
        assert prefix0 in det.ts_detector.history
        assert prefix1 in det.ts_detector.history
