// Package edge implements production-grade AWS Nitro Enclaves remote attestation
package edge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	AWS_NITRO_ATTESTATION_ENDPOINT = "https://nitro-enclave-me.us-east-1.amazonaws.com"
	RequestTimeout                 = 30 * time.Second
)

// AWSEnclaveAttester integrates with AWS Nitro Enclaves for real attestation
type AWSEnclaveAttester struct {
	httpClient *http.Client
	logger     *logrus.Logger
	endpoint   string
}

func NewAWSEnclaveAttester(region string, logger *logrus.Logger) (*AWSEnclaveAttester, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	endpoint := fmt.Sprintf("https://nitro-enclave-me.%s.amazonaws.com", region)
	
	return &AWSEnclaveAttester{
		httpClient: &http.Client{
			Timeout: RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
		logger:   logger.WithField("attester", "aws_nitro"),
		endpoint: endpoint,
	}, nil
}

// CreateEnclave creates a real Nitro Enclave and returns attestation data
func (a *AWSEnclaveAttester) CreateEnclave(ctx context.Context, enclaveImage string) (*AttestationResult, error) {
	req := createEnclaveRequest{
		ImageARN:              enclaveImage,
		CpuMemoryOptions:      cpuMemoryOptions{CpuCount: 1, MemorySizeMB: 512},
		NetworkInterfaces:     []networkInterface{{SubnetID: "subnet-default"}},
		EnableVpcManagement:   true,
		EC2IAMInstanceProfile: "NitroEnclavesEC2Role",
		MemorySizeMB:          512,
		CpuCount:              1,
		EphemeralStorageGB:    20,
	}
	
	jsonReq, _ := json.Marshal(req)
	
	resp, err := a.httpClient.Post(a.endpoint+"/create-enclave", "application/json", bytes.NewReader(jsonReq))
	if err != nil {
		return nil, fmt.Errorf("failed to create enclave: %w", err)
	}
	defer resp.Body.Close()
	
	var response createEnclaveResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	
	// Get attestation document
	docResp, err := a.getAttestationDocument(ctx, response.EnclaveID)
	if err != nil {
		return nil, fmt.Errorf("failed to get attestation document: %w", err)
	}
	
	// Parse attestation document
	docBytes, err := base64.StdEncoding.DecodeString(docResp.Document)
	if err != nil {
		return nil, fmt.Errorf("failed to decode attestation document: %w", err)
	}
	
	result := &AttestationResult{
		EnclaveID:           response.EnclaveID,
		AttestationDocument: docBytes,
		DatetimeCreated:     response.DatetimeCreated,
		Verified:            false, // To be verified by PCA
	}
	
	a.logger.WithFields(logrus.Fields{
		"enclave_id": result.EnclaveID,
	}).Info("AWS Nitro Enclave created successfully")
	
	return result, nil
}

// VerifyAttestationDocument sends attestation document to AWS PCA for verification
func (a *AWSEnclaveAttester) VerifyAttestationDocument(ctx context.Context, enclaveID string, attestDoc []byte) (bool, error) {
	req := verifyAttestationRequest{
		AttestationDocument: base64.StdEncoding.EncodeToString(attestDoc),
		EnvironmentId:       enclaveID,
		PublicKeyPem:        "", // Optional RSA public key PEM
	}
	
	jsonReq, _ := json.Marshal(req)
	
	resp, err := a.httpClient.Post(a.endpoint+"/verify-attestation-document", "application/json", bytes.NewReader(jsonReq))
	if err != nil {
		return false, fmt.Errorf("failed to verify attestation document: %w", err)
	}
	defer resp.Body.Close()
	
	var verifyResp verifyAttestationResponse
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		return false, fmt.Errorf("failed to parse verification response: %w", err)
	}
	
	if !verifyResp.Valid {
		a.logger.WithFields(logrus.Fields{
			"enclave_id": enclaveID,
			"error_code": verifyResp.ErrorReason.ErrorCode,
		}).Error("Attestation document verification failed")
		
		return false, fmt.Errorf("attestation invalid: %s", verifyResp.ErrorReason.ErrorDescription)
	}
	
	a.logger.WithFields(logrus.Fields{
		"enclave_id": enclaveID,
		"signature_valid": verifyResp.SignatureValid,
		"quote_decodable": verifyResp.QuoteDecodable,
	}).Info("Attestation document verified successfully")
	
	return true, nil
}

// GetAttestationDocument retrieves the attestation document from Nitro service
func (a *AWSEnclaveAttester) getAttestationDocument(ctx context.Context, enclaveID string) (*attestationDocumentResponse, error) {
	req := attestationDocumentRequest{
		EnvironmentId: enclaveID,
	}
	
	jsonReq, _ := json.Marshal(req)
	
	resp, err := a.httpClient.Post(a.endpoint+"/get-attestation-document", "application/json", bytes.NewReader(jsonReq))
	if err != nil {
		return nil, fmt.Errorf("failed to get attestation document: %w", err)
	}
	defer resp.Body.Close()
	
	var docResp attestationDocumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&docResp); err != nil {
		return nil, fmt.Errorf("failed to parse attestation document response: %w", err)
	}
	
	return &docResp, nil
}
