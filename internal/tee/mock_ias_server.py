#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Mock Intel IAS Server for Local Development and Testing
===========================================================

This mock server simulates the Intel Attestation Service (IAS) API endpoint,
enabling developers to test TEE quote verification without requiring real API credentials.

Features:
- Simulates all IAS /inspect endpoint responses (VALID/FAIL/REVOKED/WARNING)
- Configurable response modes via query parameters
- Realistic latency injection for performance testing
- Health check endpoint (/healthz)

Usage:
    python mock_ias_server.py --port 8080 --mode random
    
Test with curl:
    curl -X POST http://localhost:8080/ias/v2/inspect \
      -H "Content-Type: application/json" \
      -d '{"qplIn": "'$(echo -n "fake-quote-data-that-is-long-enough-for-testing-purposes-only" | base64)>'"}'

Security Notes:
- This is ONLY for local development/testing
- NEVER use in production environments
- Always validate against real IAS before deployment
"""

import argparse
import base64
import json
import logging
import random
import sys
from datetime import datetime
from flask import Flask, request, jsonify

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - [%(levelname)s] - %(message)s',
    handlers=[logging.StreamHandler(sys.stdout)]
)
logger = logging.getLogger(__name__)

app = Flask(__name__)

# ============================================================
# Test Mode Constants
# ============================================================
MODES = {
    'success': 'VALID',
    'fail': 'FAIL',
    'revoked': 'REVOKED',
    'warn': 'VALID_WITH_WARNINGS',
}

DEFAULT_MODE = 'success'
DEFAULT_PORT = 8080


# ============================================================
# Helper Functions
# ============================================================
def generate_test_response(mode: str) -> dict:
    """Generate a realistic IAS response based on mode"""
    
    if mode == 'success':
        return {
            "quoteStatus": "VALID",
            "pseID": "1234567890ABCDEF",
            "tcbEvaluationStatus": "FULLY_UPDATED",
            "additionalInfo": [
                "PCOL=105268, OV=14, CPUID=0xA0F06",
                "SGX TCB is evaluated according to PCH Type 8"
            ]
        }
    
    elif mode == 'fail':
        return {
            "quoteStatus": "FAIL",
            "quoteErrorMessage": "TCB Level 0 is out of date - security vulnerabilities detected",
            "additionalInfo": [
                "CVE-2024-XXXXX affects this enclave version",
                "Please update to latest SGX SDK"
            ]
        }
    
    elif mode == 'revoked':
        return {
            "quoteStatus": "REVOKED",
            "quoteErrorMessage": "Quote revoked due to hardware vulnerability mitigation",
            "additionalInfo": [
                "Intel Security Advisory INTEL-SA-XXXXX",
                "Hardware fix required for full protection"
            ]
        }
    
    elif mode == 'warn':
        return {
            "quoteStatus": "VALID",
            "pseID": "ABCDEF1234567890",
            "tcbEvaluationStatus": "NOT_EVALUATED",
            "quoteErrorMessage": "Warning: Latest security patches may not be installed",
            "additionalInfo": [
                "TCB evaluation pending - check Intel Security Advisories"
            ]
        }
    
    else:
        # Random mode for chaos testing
        return random.choice(list(MODES.values()))


def validate_quote_format(quote_base64: str) -> tuple[bool, str]:
    """Validate that the quote is properly formatted"""
    try:
        quote_bytes = base64.b64decode(quote_base64)
        
        # SGX quotes are typically 300-500 bytes
        if len(quote_bytes) < 256:
            return False, f"Quote too short ({len(quote_bytes)} bytes)"
        
        if len(quote_bytes) > 1024:
            return False, f"Quote too long ({len(quote_bytes)} bytes)"
        
        return True, "Valid"
        
    except Exception as e:
        return False, f"Base64 decoding failed: {str(e)}"


def simulate_network_latency() -> float:
    """Simulate realistic network latency (50-300ms)"""
    return random.uniform(0.05, 0.3)


# ============================================================
# API Endpoints
# ============================================================
@app.route('/ias/v2/inspect', methods=['POST'])
def inspect_quote():
    """
    Simulate Intel IAS /inspect endpoint
    
    Expected Request:
        POST /ias/v2/inspect
        Content-Type: application/json
        X-Api-Key: any-value
        
        {
            "qplIn": "<base64-encoded-sgx-quote>"
        }
    
    Response:
        {
            "quoteStatus": "VALID|FAIL|REVOKED",
            "pseID": "...",
            "tcbEvaluationStatus": "...",
            ...
        }
    """
    
    # Parse query parameters for test modes
    mode = request.args.get('mode', DEFAULT_MODE)
    
    # Inject simulated latency
    latency = simulate_network_latency()
    logger.info(f"Processing quote request (mode={mode}, latency={latency:.3f}s)")
    
    # Extract base64-encoded quote from request body
    try:
        data = request.get_json(force=True)
        if 'qplIn' not in data:
            return jsonify({
                "error": "Missing qplIn parameter",
                "details": "Request must contain base64-encoded SGX quote"
            }), 400
            
        quote_base64 = data['qplIn']
        if not quote_base64:
            return jsonify({"error": "Empty qplIn parameter"}), 400
            
    except json.JSONDecodeError as e:
        return jsonify({"error": "Invalid JSON", "details": str(e)}), 400
    except Exception as e:
        return jsonify({"error": "Failed to parse request", "details": str(e)}), 400
    
    # Validate quote format
    is_valid, msg = validate_quote_format(quote_base64)
    if not is_valid:
        logger.warning(f"Invalid quote format: {msg}")
        return jsonify({
            "quoteStatus": "FAIL",
            "quoteErrorMessage": msg
        }), 400
    
    # Generate appropriate response based on mode
    # For 'random' mode, randomly select one of VALID/FAIL/REVOKED
    if mode == 'random':
        actual_mode = random.choice(['success', 'fail', 'revoked', 'warn'])
        response_data = generate_test_response(actual_mode)
        logger.info(f"Random mode selected: {actual_mode}")
    else:
        response_data = generate_test_response(mode)
    
    # Log successful request
    logger.info(f"Quote verified - status: {response_data['quoteStatus']}")
    
    return jsonify(response_data), 200


@app.route('/healthz', methods=['GET'])
def health_check():
    """Health check endpoint for load balancer verification"""
    return jsonify({
        "status": "healthy",
        "timestamp": datetime.utcnow().isoformat(),
        "version": "1.0.0-mock",
        "description": "Mock Intel IAS Server - NOT FOR PRODUCTION USE"
    }), 200


@app.route('/', methods=['GET'])
def index():
    """Service information endpoint"""
    return jsonify({
        "name": "Mock Intel IAS Server",
        "version": "1.0.0",
        "description": "Local development/testing only - simulates Intel Attestation Service",
        "endpoints": [
            "/ias/v2/inspect",
            "/healthz"
        ],
        "usage": "python mock_ias_server.py --mode <success|fail|revoked|random>",
        "warning": "⚠️ DO NOT USE IN PRODUCTION - This is a MOCK server"
    })


# ============================================================
# Main Entry Point
# ============================================================
def main():
    parser = argparse.ArgumentParser(
        description='Mock Intel IAS Server for Local Testing',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Run with success mode (default):
  python mock_ias_server.py
  
  # Run with custom port:
  python mock_ias_server.py --port 9000
  
  # Run with fail mode (simulate quote failure):
  python mock_ias_server.py --mode fail
  
  # Run with random mode (chaos testing):
  python mock_ias_server.py --mode random
  
  # Test with curl:
  curl -X POST http://localhost:8080/ias/v2/inspect \\
    -H "Content-Type: application/json" \\
    -d '{"qplIn": "'$(echo -n "fake-quote" | base64)'"}'
        """
    )
    
    parser.add_argument(
        '--port', '-p',
        type=int,
        default=DEFAULT_PORT,
        help=f'Port to listen on (default: {DEFAULT_PORT})'
    )
    
    parser.add_argument(
        '--host', '-h',
        type=str,
        default='127.0.0.1',
        help='Host to bind to (default: 127.0.0.1)'
    )
    
    parser.add_argument(
        '--mode', '-m',
        type=str,
        choices=['success', 'fail', 'revoked', 'warn', 'random'],
        default=DEFAULT_MODE,
        help='Default response mode (default: success)'
    )
    
    parser.add_argument(
        '--debug', '-d',
        action='store_true',
        help='Enable Flask debug mode (auto-reload, verbose logging)'
    )
    
    args = parser.parse_args()
    
    # Print startup banner
    print("""
╔═══════════════════════════════════════════════════════════╗
║                                                           ║
║   Mock Intel IAS Server                                  ║
║   ⚠️  NOT FOR PRODUCTION USE ⚠️                          ║
║                                                           ║
╚═══════════════════════════════════════════════════════════╝

Running in {} mode on http://{}:{}
Press Ctrl+C to stop

""".format(args.mode.upper(), args.host, args.port))
    
    # Start Flask server
    app.run(
        host=args.host,
        port=args.port,
        debug=args.debug,
        threaded=True
    )


if __name__ == '__main__':
    main()
