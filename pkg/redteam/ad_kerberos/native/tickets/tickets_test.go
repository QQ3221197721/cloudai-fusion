// Package tickets unit tests for complete Kerberos ticket functionality
package tickets

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAccountName_Encode(t *testing.T) {
	an := &AccountName{
		NameType:   0x0002,
		NameString: []string{"krbtgt"},
		Realm:      "CLOUDAI.FUSION",
	}
	
	data, err := an.Encode()
	assert.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Greater(t, len(data), 10)
}

func TestAccountName_Decode(t *testing.T) {
	input := []byte{
		0x30, 0x13, // SEQUENCE length 19
		0x02, 0x01, 0x02, // INTEGER type
		0x16, 0x08, 'k', 'r', 'b', 't', 'g', 't', // UTF8 string
	}
	
	an, err := DecodeAccountName(input)
	assert.NoError(t, err)
	assert.NotNil(t, an)
}

func TestEncodeFlags_ValidNames(t *testing.T) {
	flags := EncodeFlags([]string{"Forwardable", "Renewable"})
	
	assert.NotEqual(t, uint32(0), flags)
	assert.Equal(t, true, flags&TicketFlagForwardable == TicketFlagForwardable)
	assert.Equal(t, true, flags&TicketFlagRenewable == TicketFlagRenewable)
}

func TestEncodeFlags_EmptyList(t *testing.T) {
	flags := EncodeFlags([]string{})
	
	assert.Equal(t, uint32(0), flags)
}

func TestTicketTimes_Encode(t *testing.T) {
	startTime := time.Date(2026, 8, 5, 14, 0, 0, 0, time.UTC)
	endTime := startTime.Add(10 * time.Hour)
	renewal := startTime.Add(7*24*time.Hour + 10*time.Hour)
	
	times := &TicketTimes{
		TktStartTime: startTime,
		TktEndTime:   endTime,
		RenewTill:    renewal,
	}
	
	encoded := times.Encode()
	assert.Len(t, encoded, 3)
	
	// Each should be UTCTime format (13 bytes)
	for i := 0; i < 3; i++ {
		assert.Len(t, encoded[i], 13)
	}
}

func TestEncodeCredentialClaims_Empty(t *testing.T) {
	result := EncodeCredentialClaims(nil)
	
	assert.Nil(t, result)
}

func TestEncodeCredentialClaims_WithClaims(t *testing.T) {
	claims := []CredentialClaim{
		{Name: "group", Value: "domain admins", Attr: 0x60000000},
	}
	
	result := EncodeCredentialClaims(claims)
	
	assert.NotEmpty(t, result)
	assert.Greater(t, len(result), 0)
}

func TestNewPACBuilder(t *testing.T) {
	logger := logrus.New()
	builder := NewPACBuilder(logger)
	
	assert.NotNil(t, builder)
	assert.NotNil(t, builder.pai)
	assert.Empty(t, builder.resourceGroups)
	assert.Empty(t, builder.customClaims)
}

func TestSetPrimaryAccount_SetsCorrectValues(t *testing.T) {
	logger := logrus.New()
	userSid := []byte{0x01, 0x02, 0x03}
	builder := NewPACBuilder(logger)
	
	builder.SetPrimaryAccount(userSid, 500)
	
	assert.Equal(t, userSid, builder.pai.UserSid)
	assert.Equal(t, uint32(500), builder.pai.RID)
	assert.NotEmpty(t, builder.pai.GroupSid) // Default Domain Users RID
}

func TestAddDomainAdminGroup_AddsToPAI(t *testing.T) {
	logger := logrus.New()
	domainSid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	builder := NewPACBuilder(logger)
	
	builder.SetPrimaryAccount(domainSid, 500)
	builder.AddDomainAdminGroup(domainSid)
	
	assert.Contains(t, builder.pai.GroupSid, uint32(512)) // Domain Admins RID
	assert.Len(t, builder.resourceGroups, 1)
}

func TestBuild_PACStructure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping PAC build test")
	}
	
	logger := logrus.New()
	domainSid := []byte{0x01, 0x02, 0x03}
	builder := NewPACBuilder(logger)
	
	builder.SetPrimaryAccount(domainSid, 500)
	
	pac, err := builder.Build()
	assert.NoError(t, err)
	assert.NotNil(t, pac)
	assert.Greater(t, len(pac.Elements), 0)
}

func TestNewPrivilegeSet(t *testing.T) {
	logger := logrus.New()
	ps := NewPrivilegeSet(logger)
	
	assert.NotNil(t, ps)
	assert.NotNil(t, ps.privileges)
}

func TestPrivilegeSet_AddPrivilege(t *testing.T) {
	logger := logrus.New()
	ps := NewPrivilegeSet(logger)
	
	ps.AddPrivilege("SeDebugPrivilege", true)
	ps.AddPrivilege("SeTcbPrivilege", false)
	
	assert.Greater(t, len(ps.privileges), 0)
	
	val, ok := ps.privileges["SeDebugPrivilege"]
	assert.True(t, ok)
	assert.Equal(t, uint64(2), val) // Enabled
	
	val2, ok2 := ps.privileges["SeTcbPrivilege"]
	assert.True(t, ok2)
	assert.Equal(t, uint64(0), val2) // Disabled
}

func TestRightsManager_GrantAndDeny(t *testing.T) {
	rm := NewRightsManager()
	
	rm.GrantRight("AccessReadSecurityEvents")
	rm.DenyRight("LockWorkstation")
	rm.AddGroup("Domain Users")
	
	assert.Equal(t, 1, len(rm.rights))
	assert.Equal(t, 1, len(rm.denyList))
	assert.Equal(t, 1, len(rm.groups))
}

func TestEncodeResourceGroups(t *testing.T) {
	groups := []ResourceGroup{
		{
			DomainSid:    []byte{0x01, 0x02},
			Name:         "test group",
			Sid:          1000,
			Attributes:   0x20000000,
		},
	}
	
	result := EncodeResourceGroups(groups)
	
	assert.NotEmpty(t, result)
	assert.Greater(t, len(result), 0)
}

func TestTicketPermissions_ApplyToTicket(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}
	
	logger := logrus.New()
	domainSid := []byte{0x01, 0x02, 0x03}
	
	permissions := &TicketPermissions{
		DomainSid:        domainSid,
		AdminGroupId:     512,
		UserRid:          500,
		GroupIds:         []uint32{513, 520},
		Privileges:       NewPrivilegeSet(logger),
		Rights:           NewRightsManager(),
		ExpirationTime:   time.Now().Add(time.Hour),
	}
	
	builder := NewPACBuilder(logger)
	ticket := &Ticket{}
	
	err := permissions.ApplyToTicket(ticket, builder)
	
	assert.NoError(t, err)
	assert.NotEmpty(t, builder.pai.GroupSid)
}

func TestSIDBuilder_Basic(t *testing.T) {
	sb := NewSIDBuilder("NT Authority")
	sb.AddSubAuthority(500)
	sb.AddSubAuthority(513)
	
	sid, err := sb.Build()
	assert.NoError(t, err)
	assert.Greater(t, len(sid), 8)
}
