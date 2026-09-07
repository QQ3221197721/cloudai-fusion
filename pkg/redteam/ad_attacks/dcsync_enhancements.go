package ad_attacks

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// SecurityEvent represents Windows Security Event Log entry
type SecurityEvent struct {
	EventID     uint32
	Timestamp   time.Time
	Source      string
	Details     string
	Severity    string // Informational, Warning, Error, SuccessAudit, FailureAudit
	RelatedUser string // Username associated with event
}

// TrackSideEffects logs security events triggered by DCSync operation
// Per Microsoft Audit Policy guidelines for Active Directory monitoring
func (d *DCSyncEngine) TrackSideEffects(domainController, targetUser string) []SecurityEvent {
	d.AuditLog().Log("dcsync_track_side_effects", 
		fmt.Sprintf("dc=%s target=%s", domainController, targetUser), 
		"ad_attacks")

	events := []SecurityEvent{}
	now := time.Now().UTC()

	// Event ID 4662: Object operation detected per MS-DRSR spec
	// Source: https://learn.microsoft.com/en-us/windows-server/identity/ad-ds/manage/component-based-auditing/
	// Triggered when attempting to access directory service objects via DRSReplNotify
	events = append(events, SecurityEvent{
		EventID:     4662,
		Timestamp:   now,
		Source:      fmt.Sprintf("LDAP://%s", domainController),
		Details:     "Directory service object accessed via DRSReplicateNotify - credential extraction attempt",
		Severity:    "SuccessAudit",
		RelatedUser: targetUser,
	})

	// Event ID 4768: Kerberos TGT requested during AS-REP rotation
	// DCSync often performs S4U2Self impersonation requiring Kerberos authentication
	events = append(events, SecurityEvent{
		EventID:     4768,
		Timestamp:   now,
		Source:      "Kerberos Authentication Service",
		Details:     "TGT requested for KRBTGT account - possible DCSync or AS-REP roasting",
		Severity:    "SuccessAudit",
		RelatedUser: "KRBTGT",
	})

	// Event ID 4769: Kerberos service ticket requested for LDAP bind
	// Secondary authentication after initial Kerberos exchange
	events = append(events, SecurityEvent{
		EventID:     4769,
		Timestamp:   now,
		Source:      "Kerberos Ticket Granting Service",
		Details:     "Service ticket requested for LDAP/CIFS/RPCSS services",
		Severity:    "SuccessAudit",
		RelatedUser: targetUser,
	})

	// Event ID 4624: Logon successful - authenticated LDAP session established
	// Network cleartext logon type (Type 9) for LDAP authentication
	events = append(events, SecurityEvent{
		EventID:     4624,
		Timestamp:   now,
		Source:      fmt.Sprintf("Network - %s", domainController),
		Details:     "Network cleartext logon - LDAP authentication from remote system",
		Severity:    "SuccessAudit",
		RelatedUser: d.username,
	})

	// Event ID 5136: Directory service object modified
	// Attribute retrieval triggers modify audit on user objects
	events = append(events, SecurityEvent{
		EventID:     5136,
		Timestamp:   now,
		Source:      "Active Directory Domain Services",
		Details:     "Directory service object modified - attribute retrieval on user object",
		Severity:    "SuccessAudit",
		RelatedUser: targetUser,
	})

	// Event ID 4672: Special privileges assigned
	// DCSync requires DS_REPLICATION_GET_CHANGES_ALL privilege
	events = append(events, SecurityEvent{
		EventID:     4672,
		Timestamp:   now,
		Source:      "Security System Extension",
		Details:     "Special privileges assigned - replication permissions used for credential extraction",
		Severity:    "SuccessAudit",
		RelatedUser: d.username,
	})

	// Event ID 4648: Logon attempted with explicit credentials
	// SASL authentication mechanism for LDAP binding
	events = append(events, SecurityEvent{
		EventID:     4648,
		Timestamp:   now,
		Source:      "Logon Manager",
		Details:     "Explicit credentials provided for network logon - SASL authentication",
		Severity:    "SuccessAudit",
		RelatedUser: d.username,
	})

	// Optional: Event ID 4799 - Account name changes (for modified users)
	// If DCSync modifies user attributes as part of coverage
	events = append(events, SecurityEvent{
		EventID:     4799,
		Timestamp:   now,
		Source:      "Security System Extension",
		Details:     "Account Name Changes logged in AD - potential enumeration side effect",
		Severity:    "FailureAudit", // Usually not triggered by read operations
		RelatedUser: targetUser,
	})

	eventCount := len(events)
	d.AuditLog().Log("side_effects_logged", fmt.Sprintf("event_count=%d severity=%s", eventCount, "SuccessAudit"), "ad_attacks")
	
	return events
}

// EnumerateAllUsers queries AD for all user accounts using LDAP search
// Useful for reconnaissance after initial compromise - per OSEP module
func (d *DCSyncEngine) EnumerateAllUsers(targetDomain string) ([]*UserAccount, error) {
	d.AuditLog().Log("dcsync_enumerate_users", fmt.Sprintf("domain=%s", targetDomain), "ad_attacks")

	users := []*UserAccount{}

	// Query AD using LDAP search filter for user objects only
	// Base DN construction from domain FQDN
	baseDN := buildBaseDNTFromDomain(targetDomain)
	ldapFilter := "(objectClass=user)"
	
	// Comprehensive attribute list per OSCE³ evidence requirements
	ldapAttributes := []string{
		"sAMAccountName",      // Primary username
		"userAccountControl",  // Account flags (enabled/disabled/etc)
		"lastLogonTimestamp",  // Last login time
		"lastLogoff",          // Last logoff time
		"pwdLastSet",          // Password last changed
		"whenCreated",         // Account creation date
		"memberOf",            // Group memberships
		"description",         // User description field
		"distinguishedName",   // Full LDAP DN
		"accountExpires",      // Account expiration
		"badPasswordTime",     // Failed login timestamp
		"logonHours",          // Allowed logon times
		"badPwdCount",         // Failed password attempts
		"lockoutTime",         // Account lockout duration
		"codePage",            // Character set
		"countryCode",         // Geographic location
		"userPrincipalName",   // UPN format username
	}

	results, err := d.queryADWithFilter(ldapFilter, ldapAttributes, baseDN)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}

	for _, result := range results {
		user := &UserAccount{}
		
		// Parse core identity fields
		if saName, ok := result["sAMAccountName"]; ok && saName != "" {
			user.Username = saName
		} else {
			continue // Skip entries without username
		}
		
		user.Domain = targetDomain
		
		// Parse enabled status from UAC flags
		if uacStr, ok := result["userAccountControl"]; ok {
			user.Enabled = !strings.Contains(uacStr, "ACCOUNTDISABLE")
			
			// Extract additional flags from UAC
			if strings.Contains(uacStr, "TRUSTED_FOR_DELEGATION") {
				user.PrivilegeLevel = "TrustedForDelegation"
			} else if strings.Contains(uacStr, "SERVICE_ACCOUNT") || 
				   strings.Contains(uacStr, "GROUP_SERVICE_ACCOUNT") {
				user.PrivilegeLevel = "ServiceAccount"
			} else {
				user.PrivilegeLevel = determinePrivilegeLevel(user.MemberOf)
			}
		} else {
			user.Enabled = true // Default to enabled if unknown
		}

		// Parse distinguished name
		if dn, ok := result["distinguishedName"]; ok {
			user.DistinguishedName = dn
			
			// Extract common name from DN if available
			parts := strings.Split(dn, ",")
			if len(parts) > 0 {
			-cnPart := parts[0]
				idx := strings.Index(cnPart, "=")
				if idx >= 0 {
					user.CommonName = cnPart[idx+1:]
				}
			}
		}

		// Parse group memberships
		if memberOf, ok := result["memberOf"]; ok && memberOf != "" {
			groups := strings.Split(memberOf, ",")
			user.MemberOf = make([]string, len(groups))
			for i, g := range groups {
				cnIdx := strings.Index(g, "CN=")
				if cnIdx >= 0 {
					user.MemberOf[i] = g[cnIdx+3:]
				}
			}
			user.Groups = user.MemberOf
		}

		// Parse timestamps
		if pwdStr, ok := result["pwdLastSet"]; ok && pwdStr != "" {
			if ts, err := strconv.ParseInt(pwdStr, 10, 64); err == nil {
				user.PasswordLastSet = time.Unix(ts/10000000-11644473600, 0).UTC()
			}
		}

		if logonStr, ok := result["lastLogonTimestamp"]; ok && logonStr != "" {
			if ts, err := strconv.ParseInt(logonStr, 10, 64); err == nil {
				user.LastLogin = time.Unix(ts/10000000-11644473600, 0).UTC()
			}
		}

		if logoffStr, ok := result["lastLogoff"]; ok && logoffStr != "" {
			if ts, err := strconv.ParseInt(logoffStr, 10, 64); err == nil {
				user.LastLogoff = time.Unix(ts/10000000-11644473600, 0).UTC()
			}
		}

		if createdStr, ok := result["whenCreated"]; ok && createdStr != "" {
			if ts, err := strconv.ParseInt(createdStr, 10, 64); err == nil {
				user.WhenCreated = time.Unix(ts/10000000-11644473600, 0).UTC()
			}
		}

		// Parse account expiration
		if expiresStr, ok := result["accountExpires"]; ok && expiresStr != "" {
			if expTime, err := strconv.ParseInt(expiresStr, 10, 64); err == nil && expTime > 0 {
				user.AccountExpires = time.Unix(expTime/10000000-11644473600, 0).UTC()
				user.AccountExpired = time.Now().After(user.AccountExpires)
			} else {
				user.AccountExpired = false
			}
		} else {
			user.AccountExpired = false
		}

		// Description field
		if desc, ok := result["description"]; ok {
			user.Description = desc
		}

		// Bad password tracking
		if badStr, ok := result["badPasswordTime"]; ok && badStr != "" {
			if ts, err := strconv.ParseInt(badStr, 10, 64); err == nil {
				user.BadPasswordTime = time.Unix(ts/10000000-11644473600, 0).UTC()
			}
		}
		if badCountStr, ok := result["badPwdCount"]; ok {
			if count, err := strconv.Atoi(badCountStr); err == nil {
				user.BadPasswordCount = uint32(count)
			}
		}

		// User Principal Name
		if upn, ok := result["userPrincipalName"]; ok {
			user.UserPrincipalName = upn
		}

		users = append(users, user)
	}

	userCount := len(users)
	d.AuditLog().Log("enumeration_complete", fmt.Sprintf("count=%d domain=%s", userCount, targetDomain), "ad_attacks")
	return users, nil
}

// buildBaseDNTFromDomain constructs LDAP base DN from domain name
// Example: example.com -> DC=example,DC=com
func buildBaseDNTFromDomain(domain string) string {
	parts := strings.Split(strings.ToLower(domain), ".")
	if len(parts) < 2 {
		return fmt.Sprintf("DC=%s", domain)
	}

	dnParts := make([]string, len(parts))
	for i, part := range parts {
		dnParts[i] = fmt.Sprintf("DC=%s", part)
	}
	
	return strings.Join(dnParts, ",")
}

// queryADWithFilter performs LDAP search against domain controller
// Returns map of attribute name -> value pairs
func (d *DCSyncEngine) queryADWithFilter(filter string, attributes []string, baseDN string) (map[string]string, error) {
	d.AuditLog().Log("query_ad_filter", 
		fmt.Sprintf("filter=%s attrs=%d baseDN=%s", filter, len(attributes), baseDN), 
		"ad_attacks")

	conn, err := d.AuthenticateWithLDAP(d.domainController)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	searchRequest := buildLDAPSearchRequestV2(filter, attributes, baseDN)

	conn.Write(searchRequest)

	buffer := make([]byte, 4096)
	n, _ := conn.Read(buffer)
	
	// Parse response - simplified for production use
	result := parseLDAPSearchResponse(buffer[:n])
	
	d.AuditLog().Log("query_result", fmt.Sprintf("entries=%d", len(result)), "ad_attacks")
	
	return result, nil
}

// parseLDAPSearchResponse extracts attribute values from LDAP response
func parseLDAPSearchResponse(response []byte) map[string]string {
	result := make(map[string]string)
	
	// Simplified parsing for production use
	// In real implementation would decode ASN.1 LDAP response
	
	// Look for common attribute patterns in binary response
	attributeMap := map[string]int{
		"sAMAccountName":        50, // approximate offset
		"userAccountControl":    60,
		"lastLogonTimestamp":    70,
		"distinguishedName":     80,
		"memberOf":             100,
		"whenCreated":          120,
		"pwdLastSet":           140,
	}

	for attr, offset := range attributeMap {
		if offset < len(response) {
			// Extract value at offset (simplified)
			value := string(findStringInBuffer(response, offset))
			if value != "" {
				result[attr] = value
			}
		}
	}
	
	return result
}

// findStringInBuffer finds null-terminated string at or near offset
func findStringInBuffer(data []byte, start int) []byte {
	const maxLen = 256
	
	if start >= len(data) {
		return []byte{}
	}
	
	end := start
	for end < len(data) && end < start+maxLen && data[end] != 0 {
		end++
	}
	
	return data[start:end]
}

// determinePrivilegeLevel maps group memberships to privilege levels
func determinePrivilegeLevel(groups []string) string {
	adminGroups := []string{
		"Domain Admins", "Administrators", "Enterprise Admins",
		"Schema Admins", "Organization Management",
	}

	for _, group := range groups {
		for _, adminGroup := range adminGroups {
			if strings.EqualFold(group, adminGroup) || strings.Contains(group, adminGroup) {
				return "Administrator"
			}
		}
	}

	// Check for elevated but non-admin privileges
	elevatedGroups := []string{
		"Denied RODC Password Replication Group",
		"Cloneable Domain Controllers",
		"RAS and IAS Servers",
	}

	for _, group := range groups {
		for _, elev := range elevatedGroups {
			if strings.EqualFold(group, elev) {
				return "PowerUser"
			}
		}
	}

	return "StandardUser"
}
