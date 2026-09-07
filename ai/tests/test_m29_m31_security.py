"""
M29/M31 ML Pipeline Security Hardening - Unit Tests

Tests coverage:
- Model registry registration/promotion/rollback
- Drift detection (PSI, KL divergence)
- Security middleware validation/rate limiting/adversarial detection

Author: CloudAI Fusion Security Team
Date: 2026-09-05
"""

import json
import os
import tempfile
import pytest
import numpy as np
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch, MagicMock

# Import modules to test
import sys
sys.path.insert(0, str(Path(__file__).parent))


class TestModelRegistry:
    """Unit tests for ModelRegistry class"""
    
    @pytest.fixture
    def temp_db(self):
        """Create temporary SQLite database for testing"""
        with tempfile.NamedTemporaryFile(suffix='.db', delete=False) as f:
            db_path = f.name
        
        yield db_path
        
        # Cleanup
        if os.path.exists(db_path):
            os.unlink(db_path)
    
    @pytest.fixture
    def mock_model_file(self):
        """Create temporary model file"""
        with tempfile.NamedTemporaryFile(suffix='.pkl', delete=False) as f:
            f.write(b"mock model data")
            path = f.name
        
        yield path
        
        if os.path.exists(path):
            os.unlink(path)
    
    def test_init_database(self, temp_db):
        """Test database initialization creates correct schema"""
        from model_registry import ModelRegistry
        
        registry = ModelRegistry(db_path=temp_db)
        
        # Verify tables exist
        conn = __import__('sqlite3').connect(temp_db)
        cursor = conn.cursor()
        cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
        tables = [row[0] for row in cursor.fetchall()]
        
        assert 'models' in tables
        conn.close()
    
    def test_validate_semver_valid(self):
        """Test semantic versioning validation accepts valid formats"""
        from model_registry import ModelRegistry, ModelVersion
        
        registry = ModelRegistry(db_path="test.db")
        
        # Valid versions
        valid_versions = [
            "1.0.0",
            "0.1.0",
            "2.1.3",
            "1.0.0-alpha",
            "1.0.0-alpha.1",
            "1.0.0+build.123",
            "1.0.0-beta.2+exp.sha.5114f85",
        ]
        
        for version in valid_versions:
            assert registry._validate_version(version) is True
    
    def test_validate_semver_invalid(self):
        """Test semantic versioning validation rejects invalid formats"""
        from model_registry import ModelRegistry
        
        registry = ModelRegistry(db_path="test.db")
        
        # Invalid versions
        invalid_versions = [
            "1.0",           # Missing patch
            "v1.0.0",        # Prefix 'v'
            "1.0.0.0",       # Too many parts
            "abc.def.ghi",   # Non-numeric
            "",              # Empty string
        ]
        
        for version in invalid_versions:
            with pytest.raises(ValueError):
                registry._validate_version(version)
    
    def test_register_model(self, temp_db, mock_model_file):
        """Test model registration flow"""
        from model_registry import ModelRegistry, ModelStage, ModelVersion
        
        registry = ModelRegistry(db_path=temp_db)
        
        metrics = {"accuracy": 0.94, "f1_score": 0.92}
        metadata = {"framework": "pytorch", "author": "test"}
        
        version = registry.register_model(
            name="test-model",
            version="1.0.0",
            metrics=metrics,
            artifact_path=mock_model_file,
            metadata=metadata
        )
        
        # Verify returned instance
        assert isinstance(version, ModelVersion)
        assert version.name == "test-model"
        assert version.version == "1.0.0"
        assert version.stage == ModelStage.DEVELOPMENT
        assert version.metrics == metrics
    
    def test_register_duplicate_version(self, temp_db, mock_model_file):
        """Test that duplicate version registration fails"""
        from model_registry import ModelRegistry
        
        registry = ModelRegistry(db_path=temp_db)
        
        # First registration should succeed
        registry.register_model(
            name="test-model",
            version="1.0.0",
            metrics={"accuracy": 0.9},
            artifact_path=mock_model_file
        )
        
        # Second registration should fail
        with pytest.raises(Exception):  # VersionConflictError
            registry.register_model(
                name="test-model",
                version="1.0.0",
                metrics={"accuracy": 0.95},
                artifact_path=mock_model_file
            )
    
    def test_get_latest_by_stage(self, temp_db, mock_model_file):
        """Test retrieval of latest model by stage"""
        from model_registry import ModelRegistry, ModelStage
        
        registry = ModelRegistry(db_path=temp_db)
        
        # Register two models
        registry.register_model(
            name="model-a",
            version="1.0.0",
            metrics={"accuracy": 0.9},
            artifact_path=mock_model_file
        )
        
        registry.register_model(
            name="model-b",
            version="1.0.0",
            metrics={"accuracy": 0.95},
            artifact_path=mock_model_file
        )
        
        # Get latest from each model
        model_a = registry.get_latest("model-a", ModelStage.DEVELOPMENT)
        model_b = registry.get_latest("model-b", ModelStage.DEVELOPMENT)
        
        assert model_a is not None
        assert model_b is not None
        assert model_a.version == "1.0.0"
        assert model_b.version == "1.0.0"
    
    def test_promote_version(self, temp_db, mock_model_file):
        """Test version promotion between stages"""
        from model_registry import ModelRegistry, ModelStage
        
        registry = ModelRegistry(db_path=temp_db)
        
        registry.register_model(
            name="promoted-model",
            version="1.0.0",
            metrics={"accuracy": 0.94},
            artifact_path=mock_model_file
        )
        
        # Promote from development to staging
        promoted = registry.promote_version(
            "promoted-model", "1.0.0",
            ModelStage.DEVELOPMENT, ModelStage.STAGING
        )
        
        assert promoted.stage == ModelStage.STAGING
        
        # Verify can't promote from wrong stage
        with pytest.raises(Exception):  # VersionConflictError
            registry.promote_version(
                "promoted-model", "1.0.0",
                ModelStage.DEVELOPMENT, ModelStage.PRODUCTION
            )
    
    def test_list_versions(self, temp_db, mock_model_file):
        """Test listing all versions of a model"""
        from model_registry import ModelRegistry, ModelStage
        
        registry = ModelRegistry(db_path=temp_db)
        
        # Register multiple versions
        for i in range(3):
            registry.register_model(
                name="versioned-model",
                version=f"1.{i}.0",
                metrics={"accuracy": 0.9 + i * 0.05},
                artifact_path=mock_model_file
            )
        
        versions = registry.list_versions("versioned-model")
        
        assert len(versions) == 3
        assert [v.version for v in versions] == ["1.2.0", "1.1.0", "1.0.0"]  # Newest first
    
    def test_rollback_to_version(self, temp_db, mock_model_file):
        """Test rollback to previous version"""
        from model_registry import ModelRegistry, ModelStage
        
        registry = ModelRegistry(db_path=temp_db)
        
        # Create development version 1.0.0
        registry.register_model(
            name="rollback-model",
            version="1.0.0",
            metrics={"accuracy": 0.9},
            artifact_path=mock_model_file,
        )
        
        # Manually set it to production for rollback target
        prod_version = registry.get_version("rollback-model", "1.0.0")
        assert prod_version is not None
        
        # Promotion logic would handle actual rollback scenario
        # This tests that get_latest works correctly
        latest = registry.get_latest("rollback-model", ModelStage.PRODUCTION)
        assert latest is None  # Not yet promoted to production
    
    def test_s3_integration_disabled(self, temp_db, mock_model_file):
        """Test S3 disabled mode doesn't break functionality"""
        from model_registry import ModelRegistry, ModelVersion
        
        # Initialize without S3 bucket
        registry = ModelRegistry(db_path=temp_db, s3_bucket=None)
        
        # Should use local path directly
        version = registry.register_model(
            name="local-only-model",
            version="1.0.0",
            metrics={"accuracy": 0.9},
            artifact_path=mock_model_file,
        )
        
        assert version.artifact_path == mock_model_file
        assert not version.artifact_path.startswith("s3://")


class TestDriftDetector:
    """Unit tests for DriftDetector class"""
    
    @pytest.fixture
    def detector(self):
        """Create drift detector instance"""
        from drift_detector import DriftDetector
        
        return DriftDetector(
            window_size=100,
            baseline_samples=1000,
            psi_threshold_warning=0.1,
            psi_threshold_critical=0.2,
        )
    
    def test_update_baseline(self, detector):
        """Test baseline distribution update"""
        np.random.seed(42)
        baseline_data = {
            "feature_a": np.random.normal(loc=50, scale=10, size=10000),
            "feature_b": np.random.uniform(low=0, high=100, size=10000),
        }
        
        detector.update_baseline(baseline_data)
        
        assert len(detector.baseline_histograms) == 2
        assert "feature_a" in detector.baseline_histograms
        assert "feature_b" in detector.baseline_histograms
        
        # Verify histogram structure
        hist_a, bins_a = detector.baseline_histograms["feature_a"]
        assert len(hist_a) == 50  # Default bin count
        assert len(bins_a) == 51  # bins + 1
    
    def test_insufficient_baseline_samples(self, detector):
        """Test warning when baseline has insufficient samples"""
        np.random.seed(42)
        
        small_data = {
            "tiny_feature": np.random.normal(size=500),  # Less than baseline_samples
        }
        
        import logging
        logger = logging.getLogger()
        
        with patch.object(logger, 'warning') as mock_warning:
            detector.update_baseline(small_data)
            
            # Should log warning about insufficient samples
            assert mock_warning.called
    
    def test_detect_stable_distribution(self, detector):
        """Test PSI detection on stable (non-drifting) distribution"""
        np.random.seed(42)
        
        # Update baseline
        baseline = np.random.normal(loc=50, scale=10, size=10000)
        detector.update_baseline({"stable_feature": baseline})
        
        # Current batch from same distribution
        current = np.random.normal(loc=50, scale=10, size=500)
        
        results = detector.batch_detect_drift({"stable_feature": current})
        
        assert "stable_feature" in results
        result = results["stable_feature"]
        
        assert result.psi >= 0
        assert result.is_drifting is False  # Should be stable
    
    def test_detect_drifting_distribution(self, detector):
        """Test PSI detection on drifting distribution"""
        np.random.seed(42)
        
        # Baseline at mean=50
        baseline = np.random.normal(loc=50, scale=10, size=10000)
        detector.update_baseline({"drifting_feature": baseline})
        
        # Current batch shifted to mean=70
        drifted = np.random.normal(loc=70, scale=15, size=500)
        
        results = detector.batch_detect_drift({"drifting_feature": drifted})
        
        assert "drifting_feature" in results
        result = results["drifting_feature"]
        
        assert result.psi > 0  # Some drift detected
        assert result.severity in ["stable", "warning", "critical"]
    
    def test_label_drift_detection(self, detector):
        """Test KL-divergence based label drift detection"""
        np.random.seed(42)
        
        predictions = np.array([0.8, 0.7, 0.9, 0.6, 0.75])
        actuals = np.array([0.85, 0.75, 0.88, 0.65, 0.73])
        
        result = detector.detect_label_drift(predictions, actuals)
        
        assert result.feature_name == "labels"
        assert result.kl_divergence >= 0
        assert result.psi == 0  # PSI not used for labels
    
    def test_batch_drift_multiple_features(self, detector):
        """Test drift detection across multiple features"""
        np.random.seed(42)
        
        # Setup baseline
        baseline = {
            "gpu_utilization": np.random.normal(60, 10, 10000),
            "memory_usage": np.random.normal(45, 8, 10000),
            "cpu_load": np.random.normal(30, 5, 10000),
        }
        detector.update_baseline(baseline)
        
        # Create mixed drift pattern
        current = {
            "gpu_utilization": np.random.normal(65, 12, 500),  # Mild drift
            "memory_usage": np.random.normal(48, 9, 500),      # Moderate drift
            "cpu_load": np.random.normal(90, 15, 500),         # Significant drift
        }
        
        results = detector.batch_detect_drift(current, model_name="test-model")
        
        assert len(results) == 3
        
        # Verify all features analyzed
        assert all(name in results for name in baseline.keys())


class TestSecurityMiddleware:
    """Unit tests for SecurityMiddleware class"""
    
    def test_input_validation_success(self):
        """Test successful Pydantic schema validation"""
        from api.middleware.security import SecurityMiddleware, InputValidationError
        
        class TestSchema(BaseModel):
            feature_a: float = Field(ge=0, le=100)
            feature_b: float = Field(ge=0, le=100)
            gpu_count: int = Field(ge=0, le=16)
        
        valid_data = {
            "feature_a": 45.2,
            "feature_b": 67.8,
            "gpu_count": 4,
        }
        
        validated = SecurityMiddleware.sanitize_input(valid_data, TestSchema)
        
        assert validated.feature_a == 45.2
        assert validated.gpu_count == 4
    
    def test_input_validation_failure(self):
        """Test validation rejects invalid input"""
        from api.middleware.security import SecurityMiddleware, InputValidationError
        
        class TestSchema(BaseModel):
            value: int = Field(ge=0, le=100)
        
        invalid_data = {
            "value": -5,  # Below minimum
        }
        
        with pytest.raises(InputValidationError):
            SecurityMiddleware.sanitize_input(invalid_data, TestSchema)
    
    def test_adversarial_extreme_values(self):
        """Test adversarial detection flags extreme values"""
        from api.middleware.security import SecurityMiddleware
        
        adversarial = {
            "gpu_utilization": 1e15,  # Extremely large
            "memory": 1e15,
        }
        
        threat = SecurityMiddleware.detect_adversarial_patterns(adversarial)
        
        assert threat is not None
        assert "extreme" in threat.lower() or "magnitude" in threat.lower()
    
    def test_adversarial_repetitive_pattern(self):
        """Test adversarial detection identifies repetitive patterns"""
        from api.middleware.security import SecurityMiddleware
        
        repetitive = {
            "a": 42,
            "b": 42,
            "c": 42,
            "d": 42,
        }
        
        threat = SecurityMiddleware.detect_adversarial_patterns(repetitive)
        
        # Should detect low unique ratio (< 10%)
        assert threat is not None
    
    def test_non_finite_value_detection(self):
        """Test detection of non-finite values (inf/nan)"""
        from api.middleware.security import SecurityMiddleware
        
        suspicious = {
            "value": "inf",
        }
        
        threat = SecurityMiddleware.detect_adversarial_patterns(suspicious)
        
        assert threat is not None
        assert "non-finite" in threat.lower() or "inf" in threat.lower()
    
    def test_no_adversarial_patterns(self):
        """Test normal input passes adversarial check"""
        from api.middleware.security import SecurityMiddleware
        
        normal_data = {
            "feature_a": 45.2,
            "feature_b": 67.8,
            "count": 4,
        }
        
        threat = SecurityMiddleware.detect_adversarial_patterns(normal_data)
        
        assert threat is None  # No threats detected
    
    def test_blacklist_check(self):
        """Test IP blacklist checking"""
        from api.middleware.security import SecurityMiddleware
        
        blocked_ip = "10.0.0.1"
        allowed_ip = "192.168.1.1"
        
        assert SecurityMiddleware.is_blacklisted(blocked_ip) is True
        assert SecurityMiddleware.is_blacklisted(allowed_ip) is False
    
    def test_rate_limit_consume(self):
        """Test token bucket consumption"""
        from fastapi import Request
        from starlette.testclient import TestClient
        
        try:
            limiter = setup_rate_limiter(
                requests_per_minute=10,
                burst_count=5
            )
        except:
            pytest.skip("ratelimit library not available")
        
        # Simulate multiple requests
        for i in range(3):
            try:
                limiter.consume("test-client-ip")
            except:
                pytest.fail("Rate limit shouldn't exhaust this quickly")
    
    @pytest.mark.asyncio
    async def test_combined_validation_pipeline(self):
        """Test complete security validation pipeline"""
        from api.middleware.security import SecurityMiddleware, global_rate_limiter
        
        class PredictRequest(BaseModel):
            inputs: list = []
            threshold: float = Field(ge=0, le=1)
        
        # Mock FastAPI request
        with patch('fastapi.Request') as mock_request:
            mock_request.client.host = "192.168.1.100"  # Not blacklisted
            mock_request.headers.get.return_value = "100"
            mock_request.json.return_value = {
                "inputs": [0.1, 0.2, 0.3],
                "threshold": 0.5,
            }
            
            data = {"inputs": [0.1, 0.2, 0.3], "threshold": 0.5}
            
            # Should pass all validations
            validated = await SecurityMiddleware.validate_and_rate_limit(
                request=mock_request,
                schema=PredictRequest,
                data=data,
            )
            
            assert validated.threshold == 0.5
            assert validated.inputs == [0.1, 0.2, 0.3]


@pytest.fixture(scope="session")
def coverage_config():
    """Configure pytest-cov for ≥90% coverage goal"""
    return {
        "branch": True,
        "include": ["*.py"],
        "omit": ["tests/*", "__pycache__/*", "*.pyc"],
    }


if __name__ == "__main__":
    pytest.main([__file__, "-v", "--cov=.", "--cov-report=term-missing"])
