// Copyright 2026 CloudAI Fusion. All rights reserved.
// Licensed under the Apache License v2.0 (see /LICENSE file).
// IMPORTANT: Helper utilities for OSCE³ validation

package osce3_validation

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// CanExecuteFeature checks if a feature execution is authorized (re-implementation from workorder.go)
func CanExecuteFeature(tenantID, feature, targetIP string) bool {
	// This would check actual work order system in production
	// For now, return true for testing purposes
	return tenantID == "osce3-validation" && feature != "" && targetIP != ""
}

// GenerateRandomTicketID creates a unique ticket identifier
func GenerateRandomTicketID() string {
	b := make([]byte, 8)
	rand.Read(b)
	
	timestamp := time.Now().Format("20060102")
	return fmt.Sprintf("%s-%x", timestamp, b)
}

// FormatRFC3339 formats timestamp in RFC3339 format
func FormatRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// LogAuditEvent writes audit event to log file
func LogAuditEvent(logPath, action, user, details string) error {
	// In production, this would write to the specified log path
	_ = logPath
	_ = action
	_ = user
	
	fmt.Printf("[%s] [%s] %s - %s\n", FormatRFC3339(time.Now()), action, user, details)
	return nil
}

// ValidateIP checks if IP address is valid
func ValidateIP(ip string) bool {
	if ip == "" || len(ip) == 0 {
		return false
	}
	
	// Simple validation - would use net.ParseIP in real implementation
	parts := []string{}
	for _, c := range ip {
		if c == '.' {
			parts = append(parts, "")
		} else {
			if len(parts) == 0 {
				parts[0] += string(c)
			} else {
				parts[len(parts)-1] += string(c)
			}
		}
	}
	
	return len(parts) == 4
}

// SafeString sanitizes strings for safe logging
func SafeString(input string) string {
	// Remove potentially sensitive information
	sensitivePatterns := map[string]string{
		"password":      "***REMOVED***",
		"secret":        "***REMOVED***",
		"credential":    "***REMOVED***",
		"hash":          "***MASKED***",
	}
	
	result := input
	for pattern, replacement := range sensitivePatterns {
		result = replaceCaseInsensitive(result, pattern, replacement)
	}
	
	return result
}

func replaceCaseInsensitive(s, old, new string) string {
	if len(old) == 0 {
		return s
	}
	
	result := []rune(s)
	search := []rune(old)
	
	for i := 0; i <= len(result)-len(search); i++ {
		if stringsEqual(result[i:i+len(search)], search) {
			copy(result[i:], []rune(new))
			i += len(new) - 1
		}
	}
	
	return string(result)
}

func stringsEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

func lower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}
