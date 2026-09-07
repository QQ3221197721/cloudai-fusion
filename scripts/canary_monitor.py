#!/usr/bin/env python3
# ============================================================================
# Canary Deployment Monitor with Automatic Rollback Capability
# Monitors canary deployment health and triggers rollbacks if needed
# ============================================================================

import requests
import time
import json
import argparse
import logging
import sys
from typing import Optional, Tuple, Dict, Any
from dataclasses import dataclass, asdict
from datetime import datetime
from pathlib import Path
import os

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.StreamHandler(sys.stdout),
        # File handler for detailed logs
        logging.FileHandler('canary_monitor.log')
    ]
)
logger = logging.getLogger(__name__)


@dataclass
class HealthMetrics:
    """Health metrics data structure"""
    error_rate: float
    latency_p99_ms: int
    latency_p50_ms: int
    requests_per_second: float
    success_rate: float
    timestamp: str


@dataclass  
class CanaryStatus:
    """Canary deployment status"""
    is_healthy: bool
    status: str
    reasons: list[str]
    metrics: Optional[HealthMetrics] = None


class CanaryMonitor:
    """
    Monitors canary deployment health across multiple dimensions:
    - Error rate from Prometheus
    - Latency percentiles (p50, p99)
    - Request throughput
    - HTTP endpoint health
    """
    
    def __init__(
        self,
        endpoint: str,
        prometheus_url: Optional[str] = None,
        threshold: float = 0.01,
        timeout_threshold_ms: int = 500,
        latency_p99_threshold_ms: int = 1000,
    ):
        self.endpoint = endpoint.rstrip('/')
        self.prometheus_url = prometheus_url or "http://localhost:9090"
        self.health_endpoint = f"{self.endpoint}/health"
        
        self.error_threshold = threshold
        self.latency_timeout_threshold_ms = timeout_threshold_ms
        self.latency_p99_threshold_ms = latency_p99_threshold_ms
        
        self.metrics_history: list[HealthMetrics] = []
        self.consecutive_failures = 0
        self.max_consecutive_failures = 3
        
    def get_error_rate(self) -> Tuple[float, bool]:
        """
        Query Prometheus for HTTP error rate over last 5 minutes.
        
        Returns:
            Tuple of (error_rate, success)
        """
        try:
            query = "sum(rate(http_requests_total{status=~\"5..\"}[5m])) / sum(rate(http_requests_total[5m]))"
            
            response = requests.get(
                f"{self.prometheus_url}/api/v1/query",
                params={"query": query},
                timeout=10
            )
            
            if response.status_code != 200:
                logger.warning(f"Prometheus query failed (status {response.status_code}): {response.text[:200]}")
                return 0.0, False
            
            data = response.json().get("data", {})
            result = data.get("result", [])
            
            if not result:
                logger.info("No error rate metrics found in Prometheus")
                return 0.0, True
                
            error_rate = float(result[0]["value"][1])
            return error_rate, True
            
        except Exception as e:
            logger.error(f"Error fetching error rate: {e}")
            return 0.0, False
    
    def get_latency_percentile(self, percentile: float = 0.99) -> Tuple[int, bool]:
        """
        Query Prometheus for latency percentile (p50, p90, p99).
        
        Args:
            percentile: Percentile to query (0.5, 0.9, 0.99)
            
        Returns:
            Tuple of (latency_ms, success)
        """
        try:
            # Try to use request duration histogram if available
            query = f"histogram_quantile({percentile:.2f}, rate(http_request_duration_seconds_bucket[5m]))"
            
            response = requests.get(
                f"{self.prometheus_url}/api/v1/query",
                params={"query": query},
                timeout=10
            )
            
            if response.status_code != 200:
                logger.warning(f"Latency query failed: {response.text[:200]}")
                return 0, False
            
            data = response.json().get("data", {}).get("result", [])
            
            if not data:
                # Fallback to tracing span duration
                fallback_query = f"histogram_quantile({percentile:.2f}, rate(tracing_span_duration_seconds_bucket[5m]))"
                
                response = requests.get(
                    f"{self.prometheus_url}/api/v1/query",
                    params={"query": fallback_query},
                    timeout=10
                )
                
                if response.status_code == 200:
                    data = response.json().get("data", {}).get("result", [])
                    if data:
                        latency_sec = float(data[0]["value"][1])
                        return int(latency_sec * 1000), True
                
                logger.info("No latency metrics found")
                return 0, True
                
            latency_sec = float(data[0]["value"][1])
            return int(latency_sec * 1000),  # Convert to ms
            
        except Exception as e:
            logger.error(f"Error fetching latency: {e}")
            return 0, False
    
    def get_requests_per_second(self) -> Tuple[float, bool]:
        """Query Prometheus for request throughput"""
        try:
            query = "sum(rate(http_requests_total[5m]))"
            
            response = requests.get(
                f"{self.prometheus_url}/api/v1/query",
                params={"query": query},
                timeout=10
            )
            
            if response.status_code != 200:
                return 0.0, False
            
            data = response.json().get("data", {}).get("result", [])
            
            if not data:
                return 0.0, True
                
            rps = float(data[0]["value"][1])
            return rps, True
            
        except Exception as e:
            logger.error(f"Error fetching RPS: {e}")
            return 0.0, False
    
    def check_health_endpoint(self) -> Tuple[bool, str]:
        """Check the application health endpoint directly"""
        try:
            response = requests.get(
                self.health_endpoint,
                timeout=10
            )
            
            if response.status_code == 200:
                try:
                    health_data = response.json()
                    logger.debug(f"Health endpoint response: {json.dumps(health_data, indent=2)}")
                    return True, "OK"
                except json.JSONDecodeError:
                    return True, "OK"
            else:
                reason = f"HTTP status code: {response.status_code}"
                logger.warning(f"Health check failed: {reason}")
                return False, reason
                
        except requests.exceptions.RequestException as e:
            logger.error(f"Health endpoint error: {e}")
            return False, str(e)
    
    def check_memory_usage(self) -> Tuple[float, bool]:
        """Query Prometheus for memory usage percentage"""
        try:
            query = "(container_memory_usage_bytes{pod=~\".*canary.*\"}) / (container_spec_memory_limit_bytes{pod=~\".*canary.*\"}) * 100"
            
            response = requests.get(
                f"{self.prometheus_url}/api/v1/query",
                params={"query": query},
                timeout=10
            )
            
            if response.status_code != 200:
                return 0.0, False
            
            data = response.json().get("data", {}).get("result", [])
            
            if not data:
                return 0.0, True
                
            memory_pct = float(data[0]["value"][1])
            return memory_pct, True
            
        except Exception as e:
            logger.error(f"Error fetching memory: {e}")
            return 0.0, False
    
    def check_all_metrics(self) -> HealthMetrics:
        """Collect all health metrics"""
        error_rate, _ = self.get_error_rate()
        latency_p50, _ = self.get_latency_percentile(0.50)
        latency_p99, _ = self.get_latency_percentile(0.99)
        rps, _ = self.get_requests_per_second()
        
        metrics = HealthMetrics(
            error_rate=error_rate,
            latency_p99_ms=latency_p99,
            latency_p50_ms=latency_p50,
            requests_per_second=rps,
            success_rate=1.0 - error_rate,
            timestamp=datetime.utcnow().isoformat()
        )
        
        self.metrics_history.append(metrics)
        return metrics
    
    def evaluate_health(self, metrics: HealthMetrics) -> CanaryStatus:
        """
        Evaluate current metrics against thresholds.
        
        Returns:
            CanaryStatus with evaluation results
        """
        issues = []
        warnings = []
        
        # Check error rate
        if metrics.error_rate > self.error_threshold:
            issues.append(
                f"❌ Error rate {metrics.error_rate:.4f} exceeds threshold {self.error_threshold:.4f}"
            )
            logger.error(issues[-1])
        elif metrics.error_rate > self.error_threshold * 0.8:
            warnings.append(
                f"⚠️  Error rate approaching threshold: {metrics.error_rate:.4f}"
            )
        
        # Check P99 latency
        if metrics.latency_p99_ms > self.latency_p99_threshold_ms:
            issues.append(
                f"❌ P99 latency {metrics.latency_p99_ms}ms exceeds threshold {self.latency_p99_threshold_ms}ms"
            )
            logger.error(issues[-1])
        elif metrics.latency_p99_ms > self.latency_p99_threshold_ms * 0.7:
            warnings.append(
                f"⚠️  P99 latency trending high: {metrics.latency_p99_ms}ms"
            )
        
        # Check P50 latency
        if metrics.latency_p50_ms > self.latency_timeout_threshold_ms:
            issues.append(
                f"❌ P50 latency {metrics.latency_p50_ms}ms exceeds threshold {self.latency_timeout_threshold_ms}ms"
            )
            logger.warning(issues[-1])
        
        # Determine overall status
        is_healthy = len(issues) == 0
        status = "healthy" if is_healthy else "unhealthy"
        
        return CanaryStatus(
            is_healthy=is_healthy,
            status=status,
            reasons=[*issues, *warnings],
            metrics=metrics
        )
    
    def check_overall_health(self) -> CanaryStatus:
        """Check all health dimensions and return comprehensive status"""
        logger.info("🔍 Collecting health metrics...")
        
        # Get metrics
        metrics = self.check_all_metrics()
        logger.info(f"📊 Metrics:")
        logger.info(f"   • Error Rate: {metrics.error_rate:.6f} ({metrics.error_rate * 100:.4f}%)")
        logger.info(f"   • P50 Latency: {metrics.latency_p50_ms}ms")
        logger.info(f"   • P99 Latency: {metrics.latency_p99_ms}ms")
        logger.info(f"   • Requests/sec: {metrics.requests_per_second:.2f}")
        
        # Evaluate
        status = self.evaluate_health(metrics)
        
        # Check health endpoint
        endpoint_ok, endpoint_msg = self.check_health_endpoint()
        if not endpoint_ok:
            status.reasons.append(f"❌ Endpoint unhealthy: {endpoint_msg}")
            status.is_healthy = False
            status.status = "unhealthy"
        
        # Log final status
        if status.is_healthy:
            logger.info(f"✅ Canary healthy: All checks passed")
        else:
            logger.warning(f"❌ Canary unhealthy:")
            for reason in status.reasons:
                if reason.startswith("❌"):
                    logger.warning(reason)
        
        return status
    
    def should_rollback(self, consecutive_failures: int = None) -> tuple[bool, str]:
        """
        Determine if rollback should be triggered based on health status.
        
        Returns:
            Tuple of (should_rollback, reason)
        """
        if consecutive_failures is None:
            consecutive_failures = self.max_consecutive_failures
        
        status = self.check_overall_health()
        
        if status.is_healthy:
            self.consecutive_failures = 0
            return False, "Healthy"
        
        self.consecutive_failures += 1
        
        if self.consecutive_failures >= consecutive_failures:
            reason = f"Consecutive failures: {self.consecutive_failures}/{consecutive_failures}"
            return True, reason
        
        logger.info(f"Waiting for recovery... ({self.consecutive_failures}/{consecutive_failures} failures)")
        return False, "Not enough failures yet"
    
    def wait_until_healthy(
        self, 
        timeout_minutes: int = 10,
        check_interval: int = 30
    ) -> tuple[bool, HealthMetrics | None]:
        """
        Poll until healthy or timeout reached.
        
        Args:
            timeout_minutes: Maximum monitoring duration
            check_interval: Seconds between checks
            
        Returns:
            Tuple of (success, last_metrics)
        """
        end_time = time.time() + (timeout_minutes * 60)
        logger.info(f"👀 Monitoring canary for {timeout_minutes} minutes...")
        logger.info(f"   Check interval: {check_interval}s")
        logger.info(f"   Error threshold: {self.error_threshold:.4f}")
        logger.info(f"   Latency threshold: {self.latency_p99_threshold_ms}ms")
        
        while time.time() < end_time:
            status = self.check_overall_health()
            
            if status.is_healthy:
                logger.info(f"✅ Canary is healthy! All metrics within thresholds.")
                return True, status.metrics
                
            logger.warning(f"⏳ Waiting for recovery... ({check_interval}s)")
            time.sleep(check_interval)
        
        logger.error(f"❌ Timeout after {timeout_minutes} minutes without recovery")
        return False, self.metrics_history[-1] if self.metrics_history else None
    
    def generate_report(self) -> dict[str, Any]:
        """Generate comprehensive monitoring report"""
        if not self.metrics_history:
            return {"error": "No metrics collected"}
        
        latest = self.metrics_history[-1]
        
        report = {
            "status": "healthy" if latest.success_rate > (1 - self.error_threshold) else "unhealthy",
            "generated_at": datetime.utcnow().isoformat(),
            "total_checks": len(self.metrics_history),
            "current_metrics": {
                "error_rate": latest.error_rate,
                "error_rate_percentage": round(latest.error_rate * 100, 4),
                "latency_p50_ms": latest.latency_p50_ms,
                "latency_p99_ms": latest.latency_p99_ms,
                "requests_per_second": round(latest.requests_per_second, 2),
                "success_rate": round(latest.success_rate * 100, 4)
            },
            "thresholds": {
                "error_rate_max": self.error_threshold,
                "latency_p99_ms_max": self.latency_p99_threshold_ms,
                "latency_p50_ms_max": self.latency_timeout_threshold_ms
            },
            "consecutive_failures": self.consecutive_failures
        }
        
        return report
    
    def save_report(self, output_path: Optional[str] = None):
        """Save monitoring report to JSON file"""
        report = self.generate_report()
        
        if output_path is None:
            output_path = "canary-health-report.json"
        
        with open(output_path, 'w') as f:
            json.dump(report, f, indent=2)
        
        logger.info(f"💾 Report saved to: {output_path}")
        return output_path


def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(
        description="CloudAI Fusion Canary Deployment Monitor",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python canary_monitor.py --endpoint http://my-app:8000 --duration 600
  python canary_monitor.py --prometheus-url http://prometheus:9090 --threshold 0.01
  python canary_monitor.py --duration 300 --output custom-report.json
        """
    )
    
    parser.add_argument(
        "--endpoint",
        default="http://localhost:8000",
        help="Application health endpoint (default: http://localhost:8000)"
    )
    
    parser.add_argument(
        "--prometheus-url",
        default="http://localhost:9090",
        help="Prometheus URL for metrics (default: http://localhost:9090)"
    )
    
    parser.add_argument(
        "--threshold",
        type=float,
        default=0.01,
        help="Maximum acceptable error rate (default: 0.01 = 1%%)"
    )
    
    parser.add_argument(
        "--duration",
        type=int,
        default=600,
        help="Monitoring duration in seconds (default: 600 = 10 minutes)"
    )
    
    parser.add_argument(
        "--interval",
        type=int,
        default=30,
        help="Check interval in seconds (default: 30)"
    )
    
    parser.add_argument(
        "--output",
        help="Output JSON report file path"
    )
    
    parser.add_argument(
        "--verbose", "-v",
        action="store_true",
        help="Enable verbose logging"
    )
    
    args = parser.parse_args()
    
    if args.verbose:
        logging.getLogger().setLevel(logging.DEBUG)
    
    logger.info("=" * 70)
    logger.info("CloudAI Fusion Canary Deployment Monitor")
    logger.info("=" * 70)
    logger.info(f"Configuration:")
    logger.info(f"  • Endpoint: {args.endpoint}")
    logger.info(f"  • Prometheus: {args.prometheus_url}")
    logger.info(f"  • Duration: {args.duration}s ({args.duration // 60} minutes)")
    logger.info(f"  • Threshold: {args.threshold:.4f} ({args.threshold * 100:.2f}%)")
    logger.info("=" * 70)
    
    monitor = CanaryMonitor(
        endpoint=args.endpoint,
        prometheus_url=args.prometheus_url,
        threshold=args.threshold,
        latency_p99_threshold_ms=1000,  # 1 second P99 latency
    )
    
    start_time = time.time()
    successful, last_metrics = monitor.wait_until_healthy(
        timeout_minutes=args.duration // 60,
        check_interval=args.interval
    )
    
    elapsed = time.time() - start_time
    
    # Generate and save report
    report_path = monitor.save_report(args.output)
    
    logger.info("=" * 70)
    if successful:
        logger.info("✅ CANARY DEPLOYMENT HEALTHY")
        logger.info(f"   Total monitoring time: {elapsed:.1f}s")
        exit_code = 0
    else:
        logger.warning("❌ CANARY DEPLOYMENT UNHEALTHY")
        if last_metrics:
            logger.warning(f"   Final error rate: {last_metrics.error_rate:.6f}")
            logger.warning(f"   Final P99 latency: {last_metrics.latency_p99_ms}ms")
        logger.warning(f"   Total monitoring time: {elapsed:.1f}s")
        logger.warning("Rollback required!")
        exit_code = 1
    
    logger.info("=" * 70)
    
    sys.exit(exit_code)


if __name__ == "__main__":
    main()
