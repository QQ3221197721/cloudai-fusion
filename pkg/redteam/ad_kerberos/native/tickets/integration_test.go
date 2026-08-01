// Package tickets comprehensive integration tests
package tickets

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGoldenTicket_Creation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping golden ticket creation test")
	}
	
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	domainSid := []byte{0x01, 0x02, 0x03, 0x04}
	krbtgtHashMD4 := make([]byte, 16)
	binary.LittleEndian.PutUint64(krbtgtHashMD4[:8], 0xDEADBEEF)
	binary.LittleEndian.PutUint64(krbtgtHashMD4[8:], 0xFACEBOOK)
	
	creator := NewGoldenTicketCreator(logger, "TEST.LOCAL", "dc.test.local", string(domainSid), krbtgtHashMD4)
	
	options := &GoldenTicketOptions{
		TargetUser:        "admin",
		DomainSID:         domainSid,
		UserRID:           500,
		ExpirationTime:    time.Now().Add(time.Hour),
		RenewalExpiration: time.Now().Add(7 * 24 * time.Hour),
		EncryptionType:    crypto.AES256_CTS_HMAC_SHA1_96,
	}
	
	ctx := context.Background()
	ticket, err := creator.CreateGoldenTicket(ctx, options)
	
	assert.NoError(t, err)
	assert.NotNil(t, ticket)
	assert.Equal(t, "KRBTGT@TEST.LOCAL", ticket.Realm)
}

func TestSilverTicket_Creation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping silver ticket creation test")
	}
	
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	serviceName := "HOST/target.server.local"
	host := "target.server.local"
	
	creator := NewSilverTicketCreator(logger, serviceName, host)
	serviceKey := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
	}
	creator.SetUserPasswordKey(serviceKey)
	
	targetAccount := AccountName{
		NameType:   KERB_NT_PRINCIPAL_TYPE,
		NameString: []string{"SYSTEM"},
		Realm:      "TARGET.LOCAL",
	}
	
	options := &TGSOptions{
		ExpirationTime: time.Now().Add(time.Hour * 24),
		ServiceName:    serviceName,
	}
	
	ctx := context.Background()
	ticket, err := creator.CreateForgedTGS(ctx, targetAccount, options)
	
	assert.NoError(t, err)
	assert.NotNil(t, ticket)
	assert.Equal(t, "TARGET.LOCAL", ticket.Realm)
}

func TestAccountName_EncodeDecode_RoundTrip(t *testing.T) {
	an := &AccountName{
		NameType:   KERB_NT_PRINCIPAL_TYPE,
		NameString: []string{"krbtgt", "test.local"},
		Realm:      "TEST.LOCAL",
	}
	
	encoded, err := an.Encode()
	assert.NoError(t, err)
	assert.NotEmpty(t, encoded)
	
	decoded, err := DecodeAccountName(encoded)
	assert.NoError(t, err)
	
	assert.Equal(t, an.NameType, decoded.NameType)
	assert.Len(t, decoded.NameString, len(an.NameString))
	assert.Equal(t, an.Realm, decoded.Realm)
}

func TestPACBuilder_CompleteWorkflow(t *testing.T) {
	logger := logrus.New()
	domainSid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	
	builder := NewPACBuilder(logger)
	builder.SetPrimaryAccount(domainSid, 500)
	builder.AddDomainAdminGroup(domainSid)
	
	privSet := NewPrivilegeSet(logger)
	privSet.AddPrivilege("SeDebugPrivilege", true)
	privSet.AddPrivilege("SeTcbPrivilege", true)
	
	pac, err := builder.Build()
	
	assert.NoError(t, err)
	assert.NotNil(t, pac)
	assert.GreaterOrEqual(t, len(pac.Elements), 2) // PAI + PAC elements
	
	// Verify Domain Admin group was added
	assert.Contains(t, builder.pai.GroupSid, uint32(512))
}

func TestPACChecksum_Verification(t *testing.T) {
	logger := logrus.New()
	domainSid := []byte{0x01, 0x02}
	
	builder := NewPACBuilder(logger)
	builder.SetPrimaryAccount(domainSid, 500)
	
	pac1, _ := builder.Build()
	
	// Modify the PAC data
	if len(pac1.Elements) > 0 {
		pac1.Elements[0].Data[0] ^= 0xFF
	}
	
	pac2, _ := builder.Build()
	
	// Checksums should be different
	assert.NotEqual(t, pac1.Checksum, pac2.Checksum)
}

func TestEncodeFlags_CustomCombination(t *testing.T) {
	flags := EncodeFlags([]string{"Forwardable", "Renewable", "Mutable"})
	
	assert.NotEqual(t, uint32(0), flags)
	assert.True(t, flags&TicketFlagForwardable != 0)
	assert.True(t, flags&TicketFlagRenewable != 0)
	assert.True(t, flags&TicketFlagMutable != 0)
}

func TestEncodeCredentialClaims_Multiple(t *testing.T) {
	claims := []CredentialClaim{
		{Name: "group", Value: "Domain Admins", Attr: 0x60000000},
		{Name: "rights", Value: "RemoteDesktop", Attr: 0x00000002},
		{Name: "privileges", Value: "SeDebugPrivilege", Attr: 0x00000002},
	}
	
	result := EncodeCredentialClaims(claims)
	
	assert.NotEmpty(t, result)
	assert.Greater(t, len(result), 20)
}

func TestSIDBuilder_BuildComplete(t *testing.T) {
	sb := NewSIDBuilder("NT Authority")
	sb.AddSubAuthority(19)
	sb.AddSubAuthority(544)
	sb.AddSubAuthority(550)
	
	sid, err := sb.Build()
	
	assert.NoError(t, err)
	assert.Greater(t, len(sid), 8)
	
	// First byte should be revision 0x01
	assert.Equal(t, byte(0x01), sid[0])
	
	// Second byte should be number of sub-authorities (3)
	assert.Equal(t, byte(3), sid[1])
}

func TestTicketPermissions_ApplyToTicket_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping full integration test")
	}
	
	logger := logrus.New()
	domainSid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x08}
	
	permissions := &TicketPermissions{
		DomainSid:        domainSid,
		AdminGroupId:     512,
		UserRid:          500,
		GroupIds:         []uint32{513, 520, 544},
		Privileges:       NewPrivilegeSet(logger),
		Rights:           NewRightsManager(),
		ExpirationTime:   time.Now().AddDate(0, 0, 7),
	}
	
	builder := NewPACBuilder(logger)
	ticket := &Ticket{}
	
	err := permissions.ApplyToTicket(ticket, builder)
	
	assert.NoError(t, err)
	assert.NotEmpty(t, builder.pai.GroupSid)
	assert.Greater(t, len(builder.pai.GroupSid), 2) // At least RID + Domain Users + DA
}

func TestEncryptTicket_SupportedAlgorithms(t *testing.T) {
	tkt := &Ticket{
		TicketName: AccountName{
			NameType:   KERB_NT_PRINCIPAL_TYPE,
			NameString: []string{"test"},
		},
		Realm:        "TEST.LOCAL",
		Key:          EncryptionKey{Algorithm: crypto.AES256_CTS_HMAC_SHA1_96, Key: make([]byte, 32)},
		Expiration:   time.Now().Add(time.Hour),
		RenewTill:    time.Now().Add(7*24*time.Hour),
		Credentials:  nil,
	}
	
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	
	encrypted, err := EncryptTicket(tkt, key, crypto.AES256_CTS_HMAC_SHA1_96)
	
	assert.NoError(t, err)
	assert.NotEmpty(t, encrypted)
	assert.Greater(t, len(encrypted), len(make([]byte, 32)))
}

func TestKDCOptions_DefaultValues(t *testing.T) {
	opts := DefaultKDCOptions()
	
	assert.Equal(t, time.Hour*10, opts.ExpirationDuration)
	assert.Equal(t, time.Hour*24*7, opts.RenewalDuration)
	assert.True(t, opts.IsForwardable)
	assert.False(t, opts.IsMutable)
	assert.Empty(t.opts.EncryptionKey)
}
