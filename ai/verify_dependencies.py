#!/usr/bin/env python3
# ============================================================================
# CloudAI Fusion - M29/M31 Security Dependencies Verification
# ============================================================================
# Purpose: Verify all required dependencies are installed and working
# Usage: python verify_dependencies.py
# ============================================================================

import sys
from importlib.metadata import version, PackageNotFoundError

def check_import(name, module=None):
    """Try to import module and return success status."""
    if module is None:
        module = name
    
    try:
        __import__(module)
        try:
            v = version(module)
            print(f"[OK] {name}={v}")
        except PackageNotFoundError:
            print(f"[OK] {name}")
        return True
    except ImportError as e:
        print(f"[FAIL] {name}: {e}")
        return False

def main():
    """Check all critical dependencies."""
    
    # Critical runtime dependencies (required for M29/M31 security tests)
    core = {
        "numpy": "numpy",
        "scipy": "scipy",
        "structlog": "structlog",  # CRITICAL: Was missing in Thomas report
        "httpx": "httpx",          # CRITICAL: Replaces deprecated httpx2
        "pytest": "pytest",
        "pytest_asyncio": "pytest_asyncio",  # CRITICAL: Was missing in Thomas report
        "prometheus_client": "prometheus_client",
        "pydantic": "pydantic",
        "ratelimit": "ratelimit",
        "boto3": "boto3",
        "scikit_learn": "sklearn",
        "fastapi": "fastapi",
        "torch": "torch",
    }
    
    failed = []
    
    print("=" * 70)
    print("CloudAI Fusion AI Engine Dependencies Verification")
    print("=" * 70)
    print("\n[CORE DEPENDENCIES - Required for M29/M31 Tests]")
    print("-" * 70)
    
    for name, module in core.items():
        if not check_import(name, module):
            failed.append(name)
    
    print("\n[VERIFICATION SUMMARY]")
    print("-" * 70)
    
    if failed:
        print(f"\n[FAILED] {len(failed)} dependency(ies) missing!")
        for name in failed:
            print(f"   - {name}")
        print("\n[Installation Command]:")
        print(f"   pip install {' '.join(failed)}")
        print("\nOr run complete installation:")
        print(f"   pip install -r requirements-complete.txt")
        sys.exit(1)
    else:
        print(f"\n[SUCCESS] All {len(core)} core dependencies verified!")
        
        try:
            np_ver = version("numpy")
            sp_ver = version("scipy")
            sl_ver = version("structlog")
            
            print(f"\n[Key Version Check]:")
            print(f"   numpy=={np_ver}")
            print(f"   scipy=={sp_ver}")
            print(f"   structlog=={sl_ver}")
            
            print(f"\n[Ready to run test suite]:")
            print(f"   pytest tests/test_m29_m31_security.py -v --cov=. --tb=short")
            
        except Exception as e:
            print(f"\n[Could not retrieve versions] {e}")
        
        sys.exit(0)

if __name__ == "__main__":
    main()
