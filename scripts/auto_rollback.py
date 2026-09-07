#!/usr/bin/env python3
# ============================================================================
# Automatic Rollback Trigger for CloudAI Fusion Canary Deployments
# Monitors deployment health and triggers Kubernetes rollback if needed
# ============================================================================

import subprocess
import argparse
import logging
import json
import time
import requests
from datetime import datetime
from pathlib import Path
from typing import Optional, Tuple, Dict, Any
import os

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.StreamHandler(),
        logging.FileHandler('auto_rollback.log')
    ]
)
logger = logging.getLogger(__name__)


class AutoRollback:
    """Automatically detects unhealthy deployments and triggers rollback"""
    
    def __init__(
        self,
        namespace: str = "production",
        deployment_name: str = "cloudai-fusion-ai",
        kubectl_context: Optional[str] = None,
        check_interval: int = 60,
        consecutive_failures_threshold: int = 2,
        slack_webhook_url: Optional[str] = None,
    ):
        self.namespace = namespace
        self.deployment_name = deployment_name
        self.kubectl_context = kubectl_context
        self.check_interval = check_interval
        self.threshold = consecutive_failures_threshold
        self.slack_webhook_url = slack_webhook_url
        
        self.failure_count = 0
        self.last_success_check: Optional[datetime] = None
        self.rollback_history: list[Dict[str, Any]] = []
        
    def run_command(self, command: list[str], check: bool = True) -> Tuple[bool, str]:
        """Run shell command and return (success, output)"""
        try:
            result = subprocess.run(
                command,
                capture_output=True,
                text=True,
                timeout=30
            )
            
            if check and result.returncode != 0:
                logger.error(f"Command failed: {' '.join(command)}")
                logger.error(result.stderr)
                return False, result.stderr
            
            return True, result.stdout + result.stderr
            
        except subprocess.TimeoutExpired:
            logger.error(f"Command timed out: {' '.join(command)}")
            return False, "Timeout"
        except Exception as e:
            logger.error(f"Command error: {e}")
            return False, str(e)
    
    def get_deployment_status(self) -> dict[str, Any]:
        """Get current deployment status from Kubernetes"""
        success, output = self.run_command([
            "kubectl", "get", "deployment", self.deployment_name,
            "-n", self.namespace,
            "-o", "json"
        ])
        
        if not success:
            return {"error": "Failed to get deployment status"}
        
        try:
            status = json.loads(output)
            return {
                "ready_replicas": status.get("status", {}).get("readyReplicas", 0),
                "available_replicas": status.get("status", {}).get("availableReplicas", 0),
                "observed_generation": status.get("status", {}).get("observedGeneration", 0),
                "updated_replicas": status.get("status", {}).get("updatedReplicas", 0),
                "replicas": status.get("spec", {}).get("replicas", 0),
                "unavailable_replicas": len(status.get("status", {}).get("conditions", [])),
            }
        except json.JSONDecodeError as e:
            return {"error": f"Failed to parse JSON: {e}"}
    
    def get_rollout_status(self) -> dict[str, Any]:
        """Check rollout progress of the deployment"""
        success, output = self.run_command([
            "kubectl", "rollout", "status", "deployment", self.deployment_name,
            "-n", self.namespace,
            "--timeout=60s"
        ])
        
        # Parse rollout status
        is_complete = "successfully rolled out" in output
        details = {
            "complete": is_complete,
            "output": output.strip()
        }
        
        logger.info(f"Rollout status: {'✅ Complete' if is_complete else '⏳ In Progress'}")
        return details
    
    def trigger_rollback(self) -> tuple[bool, str]:
        """Execute Kubernetes rollback"""
        logger.warning("🚨 TRIGGERING CANARY ROLLBACK")
        
        # Execute rollback
        success, output = self.run_command([
            "kubectl", "rollout", "undo", "deployment", self.deployment_name,
            "-n", self.namespace
        ])
        
        if success:
            logger.success("✅ Rollback executed successfully")
            
            # Record rollback
            rollback_record = {
                "timestamp": datetime.utcnow().isoformat(),
                "triggered_by": "auto_rollback.py",
                "namespace": self.namespace,
                "deployment": self.deployment_name,
                "failure_count": self.failure_count
            }
            self.rollback_history.append(rollback_record)
            
            return True, output
        else:
            logger.error("❌ Rollback failed")
            return False, output
    
    def notify_slack(self, message: str, severity: str = "warning"):
        """Send Slack notification"""
        if not self.slack_webhook_url:
            logger.info("No Slack webhook configured, skipping notification")
            return
        
        color_map = {
            "info": "#36a64f",
            "warning": "#ffaa00",
            "error": "#ff0000"
        }
        
        payload = {
            "attachments": [
                {
                    "color": color_map.get(severity, "#ffaa00"),
                    "title": f"{severity.upper()} - CloudAI Fusion Rollback",
                    "text": message,
                    "footer": "CloudAI Fusion CI/CD Pipeline",
                    "ts": int(time.time())
                }
            ]
        }
        
        try:
            response = requests.post(
                self.slack_webhook_url,
                json=payload,
                timeout=10
            )
            
            if response.status_code == 200:
                logger.info("Slack notification sent")
            else:
                logger.error(f"Slack notification failed: {response.status_code}")
                
        except Exception as e:
            logger.error(f"Error sending Slack notification: {e}")
    
    def should_trigger_rollback(self) -> Tuple[bool, str]:
        """Determine if rollback should be triggered"""
        
        # Get deployment status
        status = self.get_deployment_status()
        
        if "error" in status:
            logger.error(f"Could not get deployment status: {status['error']}")
            self.failure_count += 1
            return self.failure_count >= self.threshold, status["error"]
        
        # Check for critical issues
        issues = []
        
        ready = status.get("ready_replicas", 0)
        total = status.get("replicas", 0)
        
        if ready < total:
            missing = total - ready
            issues.append(f"{missing}/{total} replicas not ready")
        
        available = status.get("available_replicas", 0)
        
        if available < total:
            issues.append(f"{total - available}/{total} replicas unavailable")
        
        updated = status.get("updated_replicas", 0)
        
        if updated != ready:
            issues.append(f"Updated replicas ({updated}) don't match ready ({ready})")
        
        # Evaluate issues
        if issues:
            logger.warning(f"Deployment issues detected: {', '.join(issues)}")
            self.failure_count += 1
        else:
            self.failure_count = 0
            self.last_success_check = datetime.utcnow()
        
        should_rollback = self.failure_count >= self.threshold
        
        if should_rollback:
            reason = f"Consecutive failures: {self.failure_count}/{self.threshold}"
            logger.warning(f"🔥 Should rollback: {reason}")
            return True, "; ".join(issues)
        
        return False, "All checks passing"
    
    def wait_and_monitor(self, max_wait_minutes: int = 30):
        """Monitor deployment continuously until healthy or timeout"""
        end_time = time.time() + (max_wait_minutes * 60)
        
        logger.info(f"👀 Starting automatic monitoring for {max_wait_minutes} minutes...")
        logger.info(f"   Failure threshold: {self.threshold}")
        logger.info(f"   Check interval: {self.check_interval}s")
        
        while time.time() < end_time:
            should_rollback, reason = self.should_trigger_rollback()
            
            if should_rollback:
                logger.warning(f"\n🚨 Triggering rollback due to: {reason}")
                success, _ = self.trigger_rollback()
                
                if success:
                    self.notify_slack(
                        f"Canary deployment failed. Auto-rollback triggered.\n\n"
                        f"*Reason:* {reason}\n"
                        f"*Failure Count:* {self.failure_count}\n"
                        f"*Deployment:* {self.deployment_name}\n"
                        f"*Namespace:* {self.namespace}",
                        severity="error"
                    )
                    
                    # Wait for rollback to complete
                    logger.info("Waiting for rollback to complete...")
                    time.sleep(60)
                    
                    rollout_status = self.get_rollout_status()
                    if rollout_status.get("complete", False):
                        logger.success("✅ Rollback completed successfully")
                    else:
                        logger.warning("⚠️  Rollout may still be in progress")
                    
                    return True
                    
            else:
                logger.info(f"✓ Deployment healthy: {reason}")
            
            remaining_seconds = int(end_time - time.time())
            if remaining_seconds > 0:
                sleep_time = min(self.check_interval, remaining_seconds)
                logger.info(f"Waiting {sleep_time}s before next check...")
                time.sleep(sleep_time)
        
        logger.error(f"❌ Timeout after {max_wait_minutes} minutes without recovery")
        return False
    
    def generate_report(self) -> dict[str, Any]:
        """Generate comprehensive rollback report"""
        return {
            "report_generated_at": datetime.utcnow().isoformat(),
            "configuration": {
                "namespace": self.namespace,
                "deployment": self.deployment_name,
                "check_interval": self.check_interval,
                "threshold": self.threshold
            },
            "metrics": {
                "total_checks_attempted": len(self.rollback_history),
                "failures_detected": sum(h.get("failure_count", 0) for h in self.rollback_history),
                "last_success_check": self.last_success_check.isoformat() if self.last_success_check else None
            },
            "rollback_history": self.rollback_history
        }


def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(
        description="CloudAI Fusion Automatic Rollback System",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python auto_rollback.py --namespace production --deployment cloudai-fusion-ai
  python auto_rollback.py --slack-webhook-url $SLACK_WEBHOOK_URL --check-interval 30
        """
    )
    
    parser.add_argument(
        "--namespace",
        default="production",
        help="Kubernetes namespace (default: production)"
    )
    
    parser.add_argument(
        "--deployment",
        default="cloudai-fusion-ai",
        help="Deployment name (default: cloudai-fusion-ai)"
    )
    
    parser.add_argument(
        "--kubectl-context",
        help="Kubectl context name (optional)"
    )
    
    parser.add_argument(
        "--check-interval",
        type=int,
        default=60,
        help="Check interval in seconds (default: 60)"
    )
    
    parser.add_argument(
        "--threshold",
        type=int,
        default=2,
        help="Consecutive failures before rollback (default: 2)"
    )
    
    parser.add_argument(
        "--duration",
        type=int,
        default=30,
        help="Monitoring duration in minutes (default: 30)"
    )
    
    parser.add_argument(
        "--slack-webhook-url",
        help="Slack webhook URL for notifications"
    )
    
    args = parser.parse_args()
    
    logger.info("=" * 70)
    logger.info("CloudAI Fusion Auto-Rollback System")
    logger.info("=" * 70)
    logger.info(f"Configuration:")
    logger.info(f"  • Namespace: {args.namespace}")
    logger.info(f"  • Deployment: {args.deployment}")
    logger.info(f"  • Check interval: {args.check_interval}s")
    logger.info(f"  • Failure threshold: {args.threshold}")
    logger.info(f"  • Monitoring duration: {args.duration} minutes")
    logger.info("=" * 70)
    
    rollback_system = AutoRollback(
        namespace=args.namespace,
        deployment_name=args.deployment,
        kubectl_context=args.kubectl_context,
        check_interval=args.check_interval,
        consecutive_failures_threshold=args.threshold,
        slack_webhook_url=args.slack_webhook_url
    )
    
    start_time = time.time()
    success = rollback_system.wait_and_monitor(max_wait_minutes=args.duration)
    elapsed = time.time() - start_time
    
    # Generate report
    report = rollback_system.generate_report()
    
    with open("auto_rollback_report.json", 'w') as f:
        json.dump(report, f, indent=2)
    
    logger.info("=" * 70)
    if success:
        logger.success("✅ Auto-rollback completed successfully")
    else:
        logger.warning("⚠️  Rollback did not complete within timeout")
    logger.info(f"Total monitoring time: {elapsed:.1f}s")
    logger.info(f"Report saved to: auto_rollback_report.json")
    logger.info("=" * 70)
    
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    import sys
    main()
