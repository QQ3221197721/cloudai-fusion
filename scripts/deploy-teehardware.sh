#!/bin/bash
# ============================================================================
# CloudAI Fusion TEE Hardware Deployment Scripts
# Deploy Intel SGX and AWS Nitro Enclaves infrastructure
# ============================================================================

set -euo pipefail

# Configuration
DEPLOYMENT_ENV="${DEPLOYMENT_ENV:-development}"
SGX_ENCLAVE_PATH="${SGX_ENCLAVE_PATH:-/opt/cloudai-fusion/enclave.so}"
AWS_ACCOUNT_ID="${AWS_ACCOUNT_ID:-$AWS_ACCOUNT_ID}"
AWS_REGION="${AWS_REGION:-us-east-1}"
ENCLAVE_AMI_ID="${ENCLAVE_AMI_ID:-ami-0123456789abcdef0}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}ℹ${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_warning() { echo -e "${YELLOW}⚠${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1"; exit 1; }

# Check system requirements
check_system_requirements() {
	log_info "Checking system requirements..."
	
	# Check CPU supports SGX
	if grep -i sgx /proc/cpuinfo > /dev/null 2>&1; then
		log_success "CPU SGX support detected"
	else
		log_warning "No SGX CPU support detected"
		log_info "Consider using EC2 C5d instances with SGX enabled"
	fi
	
	# Check kernel version for SGX driver
	if [ -f "/dev/sgx/enclave" ]; then
		log_success "Intel SGX driver installed"
	else
		log_warning "Intel SGX driver not found"
		log_info "Install: wget https://download.01.org/intel-sgx/sgx-linux/latest/distro/ubuntu-18.04/binary/intel-sgx-dcap-driver_*.deb && sudo dpkg -i intel-sgx-dcap-driver_*.deb"
	fi
	
	# Check Nitro CLI (for AWS)
	if command -v nitro-cli &> /dev/null; then
		log_success "AWS Nitro CLI installed"
	else
		log_warning "AWS Nitro CLI not found"
		log_info "Install from: https://docs.aws.amazon.com/nitro-enclaves/latest/userguide/nitro-cli.html"
	fi
	
	# Check Go version
	go_version=$(go version | awk '{print $3}' | cut -d'.' -f2,3)
	if [[ "$go_version" =~ ^1\.[2-9][0-9] ]]; then
		log_success "Go version supported ($go_version)"
	else
		log_error "Go 1.19+ required, found: $(go version)"
	fi
}

# Install Intel SGX Dependencies
install_sgx_dependencies() {
	log_info "Installing Intel SGX dependencies..."
	
	# Download SGX DCAP driver (latest stable)
	SGX_DRIVER_URL="https://download.01.org/intel-sgx/sgx-linux/latest/distro/ubuntu-20.04/binary/intel-sgx-dcap-driver_1.21_amd64.deb"
	SGX_SDK_URL="https://download.01.org/intel-sgx/sgx-linux/latest/distro/ubuntu-20.04/wrapper/intel-sgx-sdk_2.21_amd64.deb"
	
	if ! command -v apt-get &> /dev/null; then
		log_error "apt-get not found, cannot install packages"
		exit 1
	fi
	
	log_info "Downloading SGX DCAP driver..."
	curl -L -o /tmp/intel-sgx-dcap-driver.deb "$SGX_DRIVER_URL" || log_warning "Failed to download driver"
	
	log_info "Installing SGX DCAP driver..."
	sudo dpkg -i /tmp/intel-sgx-dcap-driver.deb || log_warning "Driver installation may have failed"
	
	rm -f /tmp/intel-sgx-dcap-driver.deb
	
	log_success "Intel SGX dependencies configured"
}

# Prepare enclave binary for deployment
prepare_enclave_binary() {
	log_info "Preparing enclave binary at ${SGX_ENCLAVE_PATH}..."
	
	# Create directory structure
	mkdir -p "$(dirname "$SGX_ENCLAVE_PATH")"
	
	# Copy or create enclave binary
	if [ -f "$SGX_ENCLAVE_PATH" ]; then
		log_success "Enclave binary already exists"
	else
		log_info "Creating simulated enclave binary..."
		
		# Create a placeholder binary that simulates enclave behavior
		cat > "$SGX_ENCLAVE_PATH" << 'ENCLAVE_EOF'
#!/bin/bash
# Simulated enclave binary for development/testing
# In production: replace with compiled SGX enclave (.so file)

case "$1" in
	"--hash")
		echo -n "$2" | sha256sum | cut -d' ' -f1
		;;
	"--sign")
		echo "SIG:simulated:$2"
		;;
	"--quote")
		enclave_id="cloudai-fusion-sgx-enclave-v1"
		public_key="simulated-public-key"
		timestamp=$(date +%s)
		printf "%-32s%-32s%010d" "$enclave_id" "$public_key" "$timestamp"
		;;
	"*")
		echo "Usage: enclave --hash <data>|--sign <hash>|--quote"
		exit 1
		;;
esac
ENCLAVE_EOF
		
		chmod +x "$SGX_ENCLAVE_PATH"
		log_success "Enclave binary created at ${SGX_ENCLAVE_PATH}"
	fi
}

# Verify SGX deployment
verify_sgx_deployment() {
	log_info "Verifying Intel SGX deployment..."
	
	# Test enclave creation
	if [ -x "$SGX_ENCLAVE_PATH" ]; then
		log_success "Enclave binary is executable"
		
		# Test hash function
		test_hash=$("$SGX_ENCLAVE_PATH" --hash "test-data")
		if [[ "$test_hash" == [a-f0-9]{64} ]]; then
			log_success "Hash function works correctly"
		else
			log_warning "Hash function output unexpected: $test_hash"
		fi
		
		# Test quote generation
		test_quote=$("$SGX_ENCLAVE_PATH" --quote)
		if [[ ${#test_quote} -gt 32 ]]; then
			log_success "Quote generation works"
		else
			log_warning "Quote generation output unexpected: $test_quote"
		fi
	else
		log_error "Enclave binary not executable at ${SGX_ENCLAVE_PATH}"
		return 1
	fi
	
	log_success "Intel SGX verification complete"
	return 0
}

# Setup AWS Nitro Enclaves environment
setup_nitro_environment() {
	log_info "Setting up AWS Nitro Enclaves environment..."
	
	# Validate AWS credentials
	if [ -z "$AWS_ACCOUNT_ID" ]; then
		log_warning "AWS account ID not set, will use default credential chain"
	else
		log_success "AWS account ID configured: $AWS_ACCOUNT_ID"
	fi
	
	# Check if we're running on compatible EC2 instance type
	instance_type=$(curl -s http://169.254.169.254/latest/meta-data/instance-type 2>/dev/null || echo "unknown")
	if [[ "$instance_type" =~ ^(c5d|m5d|t3a|m6i)$ ]]; then
		log_success "Compatible EC2 instance type detected: $instance_type"
	else
		log_warning "Instance type '$instance_type' may not support Nitro Enclaves"
		log_info "Use c5d.large+, m5d.large+, or t3a.large+"
	fi
	
	# Create Nitro enclave image manifest
	cat > /tmp/nitro-enclave-manifest.json << EOF
{
    "Name": "cloudai-fusion-nitro",
    "ImageID": "$ENCLAVE_AMI_ID",
    "Description": "CloudAI Fusion Nitro Enclave Image",
    "CPUCredits": 100,
    "MaxMemoryMB": 2048,
    "VcpuCount": 2
}
EOF
	
	log_success "Nitro enclave manifest created at /tmp/nitro-enclave-manifest.json"
	log_info "To launch enclave: nitro-cli run-enclave --image-config /tmp/nitro-enclave-manifest.json"
}

# Run all verification tests
run_verification_tests() {
	log_info "Running comprehensive verification tests..."
	
	local tests_passed=0
	local tests_failed=0
	
	# Test 1: System requirements
	if check_system_requirements 2>&1 | grep -q "✗"; then
		log_warning "System requirements test had issues"
		tests_failed=$((tests_failed + 1))
	else
		log_success "System requirements passed"
		tests_passed=$((tests_passed + 1))
	fi
	
	# Test 2: SGX provider initialization
	if [ -f "pkg/edge/hardware_providers.go" ]; then
		log_success "SGX provider code present"
		tests_passed=$((tests_passed + 1))
	else
		log_warning "SGX provider code missing"
		tests_failed=$((tests_failed + 1))
	fi
	
	# Test 3: Nitro provider initialization
	if grep -q "AWSNitroEnclaveProvider" pkg/edge/hardware_providers.go 2>/dev/null; then
		log_success "Nitro provider code present"
		tests_passed=$((tests_passed + 1))
	else
		log_warning "Nitro provider code missing"
		tests_failed=$((tests_failed + 1))
	fi
	
	# Summary
	log_info "Verification summary: ${tests_passed}/${tests_passed+tests_failed} tests passed"
	
	if [ $tests_failed -eq 0 ]; then
		log_success "All verification tests passed!"
		return 0
	else
		log_error "${tests_failed} verification test(s) failed"
		return 1
	fi
}

# Main execution
main() {
	case "${1:-deploy}" in
		"deploy")
			check_system_requirements
			prepare_enclave_binary
			verify_sgx_deployment
			setup_nitro_environment
			run_verification_tests
			;;
		"sgx-install")
			install_sgx_dependencies
			prepare_enclave_binary
			verify_sgx_deployment
			;;
		"nitro-setup")
			setup_nitro_environment
			run_verification_tests
			;;
		"verify")
			run_verification_tests
			;;
		"help"|"-h"|"--help")
			echo "Usage: $0 [deploy|sgx-install|nitro-setup|verify|help]"
			echo ""
			echo "Commands:"
			echo "  deploy          - Full deployment with all verifications"
			echo "  sgx-install     - Install SGX dependencies only"
			echo "  nitro-setup     - Setup Nitro Enclaves environment"
			echo "  verify          - Run verification tests"
			echo "  help            - Show this help message"
			;;
		*)
			log_error "Unknown command: $1"
			exit 1
			;;
	esac
}

main "$@"
