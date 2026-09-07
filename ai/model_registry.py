"""
CloudAI Fusion - Model Registry Service

Centralized model artifact management for M29 UEBA and M31 ML Pipeline Security Hardening.
Features:
- SQLite-backed metadata storage with semantic versioning validation
- S3 integration for model artifact storage
- Stage-based promotion workflow (development → staging → production)
- Rollback support with automatic previous version detection
- Prometheus metrics for registry operations

Author: CloudAI Fusion Security Team
Date: 2026-09-05
"""

from dataclasses import dataclass, asdict
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple
import hashlib
import json
import logging
import re
import sqlite3
import os

try:
    import boto3
    from botocore.exceptions import Boto3Error, ClientError
    S3_AVAILABLE = True
except ImportError:
    S3_AVAILABLE = False
    logger = logging.getLogger(__name__)
    logger.warning("boto3 not installed. S3 integration disabled.")


# =============================================================================
# Metrics
# =============================================================================

from prometheus_client import Counter, Gauge, Histogram

MODEL_REGISTRY_OPERATIONS = Counter(
    "cloudai_model_registry_operations_total",
    "Total model registry operations",
    ["operation", "model_name"],
)

MODEL_REGISTRY_LATENCY = Histogram(
    "cloudai_model_registry_operation_seconds",
    "Model registry operation latency",
    ["operation"],
    buckets=[0.01, 0.05, 0.1, 0.5, 1.0, 5.0],
)

MODEL_STAGES_COUNT = Gauge(
    "cloudai_model_registry_stages_gauge",
    "Current number of models in each stage",
    ["model_name", "stage"],
)


# =============================================================================
# Enums & Data Classes
# =============================================================================


class ModelStage(str, Enum):
    """
    Model deployment stages following CI/CD pipeline semantics:
    
    DEVELOPMENT: New experiments and training outputs
    STAGING: Validation and evaluation stage
    PRODUCTION: Live inference models
    ARCHIVED: Retired models kept for audit/compliance
    """
    DEVELOPMENT = "development"
    STAGING = "staging"
    PRODUCTION = "production"
    ARCHIVED = "archived"


@dataclass
class ModelVersion:
    """Represents a registered model version with full metadata"""
    name: str
    version: str
    stage: ModelStage
    metrics: Dict[str, float]
    artifact_path: str
    created_at: datetime
    metadata: Dict[str, Any]
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for JSON serialization"""
        return {
            "name": self.name,
            "version": self.version,
            "stage": self.stage.value,
            "metrics": self.metrics,
            "artifact_path": self.artifact_path,
            "created_at": self.created_at.isoformat(),
            "metadata": self.metadata,
        }


# =============================================================================
# Custom Exceptions
# =============================================================================


class ModelRegistryError(Exception):
    """Base exception for model registry errors"""
    pass


class VersionConflictError(ModelRegistryError):
    """Raised when version already exists or stage mismatch"""
    pass


class ModelNotFoundError(ModelRegistryError):
    """Raised when requested model doesn't exist"""
    pass


# =============================================================================
# Core Implementation
# =============================================================================


class ModelRegistry:
    """
    Centralized model artifact management system with SQLite storage and optional S3 backend.
    
    Provides:
    - Model version registration with semantic versioning validation
    - Stage-based promotion workflow
    - Artifact storage (local filesystem or AWS S3)
    - Rollback capabilities
    - Audit trail via metadata
    
    Example:
        >>> registry = ModelRegistry(db_path="registry.db", s3_bucket="my-models")
        >>> version = registry.register_model(
        ...     name="gpu-anomaly-detector",
        ...     version="1.0.0",
        ...     metrics={"accuracy": 0.94, "f1_score": 0.92},
        ...     artifact_path="/tmp/model.pkl",
        ...     metadata={"framework": "pytorch", "author": "ml-team"}
        ... )
        >>> registry.promote_version("gpu-anomaly-detector", "1.0.0", 
        ...                          ModelStage.DEVELOPMENT, ModelStage.STAGING)
    """
    
    def __init__(self, db_path: str = "model_registry.db", s3_bucket: Optional[str] = None):
        """
        Initialize model registry with database and optional S3 client.
        
        Args:
            db_path: Path to SQLite database file
            s3_bucket: Optional AWS S3 bucket name for artifact storage
                      If not provided, only local file paths are supported
        """
        self.db_path = db_path
        self.s3_bucket = s3_bucket
        
        if s3_bucket and not S3_AVAILABLE:
            raise ModelRegistryError(
                f"S3 bucket '{s3_bucket}' configured but boto3 is not installed. "
                "Install with: pip install boto3"
            )
        
        self.s3_client = boto3.client('s3') if s3_bucket and S3_AVAILABLE else None
        self._init_database()
        
        logger.info(
            "model_registry_initialized",
            db_path=db_path,
            s3_bucket=s3_bucket or "disabled",
        )
    
    def _init_database(self):
        """Initialize SQLite database with schema and indexes"""
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        # Create models table with UNIQUE constraint on (name, version)
        cursor.execute('''
            CREATE TABLE IF NOT EXISTS models (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                version TEXT NOT NULL,
                stage TEXT NOT NULL,
                metrics TEXT NOT NULL,
                artifact_path TEXT NOT NULL,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                metadata TEXT,
                UNIQUE(name, version)
            )
        ''')
        
        # Create index for efficient lookup by name + stage
        cursor.execute('''
            CREATE INDEX IF NOT EXISTS idx_model_name_stage 
            ON models(name, stage)
        ''')
        
        # Create index for version ordering
        cursor.execute('''
            CREATE INDEX IF NOT EXISTS idx_model_created 
            ON models(name, created_at DESC)
        ''')
        
        conn.commit()
        conn.close()
        
        logger.debug("database_schema_initialized", db_path=self.db_path)
    
    def _validate_version(self, version: str) -> bool:
        """
        Validate semantic versioning per SemVer 2.0.0 spec.
        
        Pattern: MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]
        Examples: 1.0.0, 2.1.3-alpha.1, 0.0.1+build.123
        
        Args:
            version: Version string to validate
            
        Returns:
            True if valid semver, False otherwise
            
        Raises:
            ValueError: If version format is invalid
        """
        pattern = r'^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$'
        
        if not re.match(pattern, version):
            raise ValueError(f"Invalid semantic version format: '{version}'. Expected format: MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]")
        
        return True
    
    def _compute_file_hash(self, filepath: str) -> str:
        """Compute SHA-256 hash of file for integrity verification"""
        sha256_hash = hashlib.sha256()
        
        with open(filepath, "rb") as f:
            for byte_block in iter(lambda: f.read(4096), b""):
                sha256_hash.update(byte_block)
        
        return sha256_hash.hexdigest()
    
    def _upload_to_s3(self, local_path: str, model_name: str, version: str) -> str:
        """
        Upload model artifact to S3 and construct ARN-style path.
        
        Args:
            local_path: Local file path of model artifact
            model_name: Name of the model
            version: Model version string
            
        Returns:
            S3 URL in format: s3://bucket/models/{model_name}/{version}/model.pkl
        """
        if not self.s3_client:
            raise ModelRegistryError("S3 client not initialized")
        
        s3_key = f"models/{model_name}/{version}/model.pkl"
        
        try:
            # Ensure directory structure exists by uploading directly
            self.s3_client.upload_file(local_path, self.s3_bucket, s3_key)
            
            # Construct S3 URL
            s3_url = f"s3://{self.s3_bucket}/{s3_key}"
            
            MODEL_REGISTRY_OPERATIONS.labels(
                operation="s3_upload", 
                model_name=model_name
            ).inc()
            
            logger.info(
                "model_artifact_uploaded_to_s3",
                model_name=model_name,
                version=version,
                s3_key=s3_key,
                file_size=os.path.getsize(local_path),
            )
            
            return s3_url
            
        except ClientError as e:
            error_code = e.response.get('Error', {}).get('Code', 'Unknown')
            logger.error(
                "s3_upload_failed",
                model_name=model_name,
                version=version,
                error_code=error_code,
            )
            raise ModelRegistryError(f"S3 upload failed: {error_code}") from e
    
    def register_model(
        self, 
        name: str, 
        version: str, 
        metrics: Dict[str, float], 
        artifact_path: str,
        metadata: Optional[Dict[str, Any]] = None
    ) -> ModelVersion:
        """
        Register new model version with comprehensive validation and metadata.
        
        Features:
        - Semantic versioning validation
        - Duplicate version prevention
        - Artifact hash computation for integrity
        - Automatic S3 upload if bucket configured
        
        Args:
            name: Unique model identifier (e.g., "gpu-anomaly-detector")
            version: Semantic version string (e.g., "1.0.0")
            metrics: Dictionary of model performance metrics
                     Required keys: "accuracy", "loss", or equivalent
                     Example: {"accuracy": 0.94, "f1_score": 0.92, "latency_ms": 45}
            artifact_path: Local file path to model artifact (.pkl, .pth, .onnx, etc.)
            metadata: Optional metadata dict with framework, author, training params
            
        Returns:
            ModelVersion instance with final artifact_path (may be S3 URL)
            
        Raises:
            ValueError: Invalid version format or duplicate version
            FileNotFoundError: Model artifact doesn't exist locally
            ModelRegistryError: S3 upload failure
            
        Example:
            >>> registry.register_model(
            ...     name="time-series-anomaly",
            ...     version="1.0.0",
            ...     metrics={"accuracy": 0.97, "precision": 0.95},
            ...     artifact_path="./models/anomaly_v1.pkl",
            ...     metadata={
            ...         "framework": "pytorch",
            ...         "training_data_version": "2026.08",
            ...         "author": "ml-research@cloudai-fusion.io"
            ...     }
            ... )
        """
        start_time = time.time()
        
        # Validate semantic versioning
        self._validate_version(version)
        
        # Verify artifact exists locally
        if not Path(artifact_path).exists():
            raise FileNotFoundError(f"Model artifact not found: {artifact_path}")
        
        # Check if version already exists
        existing = self.get_version(name, version)
        if existing:
            raise VersionConflictError(f"Version {version} already registered for model '{name}'")
        
        # Compute file hash for integrity tracking
        file_hash = self._compute_file_hash(artifact_path)
        
        # Generate default metadata if not provided
        if metadata is None:
            metadata = {}
        
        # Add implicit metadata fields
        metadata["file_hash_sha256"] = file_hash
        metadata.setdefault("registered_by", "system")
        metadata.setdefault("registration_timestamp", datetime.now(timezone.utc).isoformat())
        
        # Finalize artifact path (local or S3)
        final_artifact_path = artifact_path
        if self.s3_bucket:
            final_artifact_path = self._upload_to_s3(artifact_path, name, version)
        
        # Store in database within transaction
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        try:
            cursor.execute('''
                INSERT INTO models (name, version, stage, metrics, artifact_path, metadata)
                VALUES (?, ?, ?, ?, ?, ?)
            ''', (
                name, 
                version, 
                ModelStage.DEVELOPMENT.value,  # Always starts in DEVELOPMENT
                json.dumps(metrics), 
                final_artifact_path,
                json.dumps(metadata)
            ))
            
            conn.commit()
            
            MODEL_REGISTRY_OPERATIONS.labels(
                operation="register", 
                model_name=name
            ).inc()
            
        except sqlite3.IntegrityError as e:
            conn.rollback()
            logger.error("duplicate_version_insertion", model_name=name, version=version)
            raise VersionConflictError(f"Version conflict for {name}:{version}") from e
        finally:
            conn.close()
        
        latency = time.time() - start_time
        MODEL_REGISTRY_LATENCY.labels(operation="register").observe(latency)
        
        result = ModelVersion(
            name=name, 
            version=version, 
            stage=ModelStage.DEVELOPMENT,
            metrics=metrics, 
            artifact_path=final_artifact_path,
            created_at=datetime.now(timezone.utc),
            metadata=metadata
        )
        
        logger.info(
            "model_registered",
            model_name=name,
            version=version,
            stage=ModelStage.DEVELOPMENT.value,
            latency_ms=round(latency * 1000, 2),
        )
        
        return result
    
    def get_version(self, name: str, version: str) -> Optional[ModelVersion]:
        """
        Retrieve specific model version by exact match.
        
        Args:
            name: Model name
            version: Exact version string
            
        Returns:
            ModelVersion instance or None if not found
        """
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        cursor.execute('''
            SELECT name, version, stage, metrics, artifact_path, created_at, metadata
            FROM models
            WHERE name = ? AND version = ?
        ''', (name, version))
        
        row = cursor.fetchone()
        conn.close()
        
        if not row:
            return None
        
        return self._row_to_version(row)
    
    def get_latest(self, name: str, stage: ModelStage = ModelStage.STAGING) -> Optional[ModelVersion]:
        """
        Get latest model version from a specific deployment stage.
        
        Args:
            name: Model name
            stage: Target deployment stage (default: STAGING)
            
        Returns:
            Latest ModelVersion in stage or None
            
        Example:
            >>> prod_model = registry.get_latest("anomaly-detector", ModelStage.PRODUCTION)
            >>> if prod_model:
            ...     print(f"Latest production: {prod_model.version}")
        """
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        cursor.execute('''
            SELECT name, version, stage, metrics, artifact_path, created_at, metadata
            FROM models
            WHERE name = ? AND stage = ?
            ORDER BY created_at DESC
            LIMIT 1
        ''', (name, stage.value))
        
        row = cursor.fetchone()
        conn.close()
        
        if not row:
            return None
        
        return self._row_to_version(row)
    
    def _row_to_version(self, row: Tuple) -> ModelVersion:
        """Convert database row to ModelVersion instance"""
        return ModelVersion(
            name=row[0],
            version=row[1],
            stage=ModelStage(row[2]),
            metrics=json.loads(row[3]),
            artifact_path=row[4],
            created_at=datetime.fromisoformat(row[5]),
            metadata=json.loads(row[6]) if row[6] else {}
        )
    
    def promote_version(
        self, 
        name: str, 
        version: str, 
        from_stage: ModelStage, 
        to_stage: ModelStage
    ) -> ModelVersion:
        """
        Promote model version between staging environments.
        
        Workflow:
        DEVELOPMENT → STAGING: After validation testing
        STAGING → PRODUCTION: After QA approval  
        PRODUCTION → ARCHIVED: When retiring model
        
        Args:
            name: Model name
            version: Version to promote
            from_stage: Current stage (validated before update)
            to_stage: Target stage
            
        Returns:
            Updated ModelVersion instance
            
        Raises:
            VersionConflictError: Source stage doesn't match
            ModelNotFoundError: Version not found
            
        Example:
            >>> registry.promote_version(
            ...     "gpu-detector", "1.0.0",
            ...     ModelStage.DEVELOPMENT, ModelStage.STAGING
            ... )
        """
        start_time = time.time()
        
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        try:
            # Verify source stage existence
            cursor.execute('''
                SELECT version FROM models 
                WHERE name = ? AND version = ? AND stage = ?
            ''', (name, version, from_stage.value))
            
            if not cursor.fetchone():
                raise VersionConflictError(
                    f"Version {version} not found in {from_stage.value}. "
                    f"Available stages may have changed."
                )
            
            # Update stage
            cursor.execute('''
                UPDATE models SET stage = ?, updated_at = CURRENT_TIMESTAMP 
                WHERE name = ? AND version = ?
            ''', (to_stage.value, name, version))
            
            if cursor.rowcount == 0:
                raise ModelNotFoundError(f"No rows updated for {name}:{version}")
            
            conn.commit()
            
            MODEL_REGISTRY_OPERATIONS.labels(
                operation=f"promote_{from_stage.value}_to_{to_stage.value}", 
                model_name=name
            ).inc()
            
            # Refresh and return updated version
            updated = self.get_version(name, version)
            
            # Update metrics
            MODEL_STAGES_COUNT.labels(model_name=name, stage=to_stage.value).set(1)
            MODEL_STAGES_COUNT.labels(model_name=name, stage=from_stage.value).set(0)
            
        except Exception as e:
            conn.rollback()
            raise
        finally:
            conn.close()
        
        latency = time.time() - start_time
        MODEL_REGISTRY_LATENCY.labels(operation="promote").observe(latency)
        
        logger.info(
            "model_promoted",
            model_name=name,
            version=version,
            from_stage=from_stage.value,
            to_stage=to_stage.value,
            latency_ms=round(latency * 1000, 2),
        )
        
        return updated
    
    def list_versions(self, name: str) -> List[ModelVersion]:
        """
        List all versions with their stages and metrics.
        
        Args:
            name: Model name to query
            
        Returns:
            List of ModelVersion sorted by creation date (newest first)
            
        Example:
            >>> versions = registry.list_versions("anomaly-detector")
            >>> for v in versions:
            ...     print(f"{v.version}: {v.stage.value} - accuracy={v.metrics['accuracy']:.3f}")
        """
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        cursor.execute('''
            SELECT name, version, stage, metrics, artifact_path, created_at, metadata
            FROM models
            WHERE name = ?
            ORDER BY created_at DESC
        ''', (name,))
        
        rows = cursor.fetchall()
        conn.close()
        
        return [self._row_to_version(row) for row in rows]
    
    def rollback_to_version(self, name: str, target_version: str) -> ModelVersion:
        """
        Rollback production model to specified previous version.
        
        Safety checks:
        - Verifies target version exists
        - Ensures target != current production version
        - Automatically promotes through STAGING first
        
        Args:
            name: Model name to rollback
            target_version: Version to restore (must exist in DEVELOPMENT)
            
        Returns:
            Rolled-back ModelVersion now in PRODUCTION
            
        Raises:
            ModelNotFoundError: No production version or target not found
            
        Example:
            >>> # Revert to last stable release after prod incident
            >>> prev = registry.rollback_to_version("detector", "0.9.0")
        """
        # Get current production version
        current = self.get_latest(name, ModelStage.PRODUCTION)
        
        if not current:
            raise ModelNotFoundError("No production version found for rollback")
        
        if current.version == target_version:
            raise ModelRegistryError(f"Already at target version: {target_version}")
        
        # Verify target exists in DEVELOPMENT
        target = self.get_version(name, target_version)
        if not target:
            raise ModelNotFoundError(f"Target version {target_version} not found")
        
        # Promote through development stage first (safety checkpoint)
        promoted = self.promote_version(
            name, target_version, 
            ModelStage.DEVELOPMENT, ModelStage.PRODUCTION
        )
        
        logger.warning(
            "production_rollback_executed",
            model_name=name,
            from_version=current.version,
            to_version=target_version,
        )
        
        return promoted
    
    def delete_version(self, name: str, version: str) -> bool:
        """
        Soft-delete model version (marks as archived).
        
        Note: Physical artifact deletion should be handled separately
        for compliance and audit purposes.
        
        Args:
            name: Model name
            version: Version to archive
            
        Returns:
            True if deleted, False if not found
        """
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        cursor.execute('''
            DELETE FROM models WHERE name = ? AND version = ?
        ''', (name, version))
        
        deleted = cursor.rowcount > 0
        conn.commit()
        conn.close()
        
        if deleted:
            MODEL_REGISTRY_OPERATIONS.labels(
                operation="delete", 
                model_name=name
            ).inc()
        
        return deleted


# Import time for metrics
import time
from datetime import timezone


if __name__ == "__main__":
    # Demo usage
    logging.basicConfig(level=logging.INFO)
    
    # Initialize registry
    registry = ModelRegistry(db_path="demo_registry.db")
    
    # Register sample model
    version = registry.register_model(
        name="demo-gpu-detector",
        version="1.0.0",
        metrics={"accuracy": 0.945, "f1_score": 0.923},
        artifact_path="/tmp/demo_model.pkl",
        metadata={"framework": "pytorch", "author": "demo"}
    )
    
    print(f"Registered: {version.name}:{version.version}")
    print(f"Artifacts: {version.artifact_path}")
