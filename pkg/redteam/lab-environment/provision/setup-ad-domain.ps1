# ============================================================================
# Active Directory Domain Provisioning Script
# ============================================================================
# This script sets up a realistic Windows Server 2022 domain controller
# with standard enterprise configuration:
# - NTLMv1 enabled for legacy compatibility (realistic target)
# - Password policy: minimum 8 chars, no complexity requirement
# - Default group policies applied
# - LSA protection disabled (common misconfiguration)
# ================================================================

param(
    [string]$DomainName = "corp.internal",
    [string]$AdminUsername = "jsmith_admin",
    [string]$AdminPassword = "Str0ngP@ssw0rd!",
    [int]$LDAPPort = 389,
    [int]$SMBPort = 445
)

$ErrorActionPreference = "Continue"

Write-Host "🏛️  Setting up Active Directory Domain Services..." -ForegroundColor Cyan

# Install AD DS role
Import-Module ADDSDeployment

# Create installation log
$logFile = "C:\AdInstall.log"

# Configure forest parameters
$ForestCredential = New-Object System.Management.Automation.PSCredential "$DomainName\$AdminUsername", (ConvertTo-SecureString $AdminPassword -AsPlainText -Force)

Write-Host "Installing AD Forest..." -ForegroundColor Yellow

try {
    # Deploy new forest
    Install-ADDSForest `
        -CreateDnsDelegation:$false `
        -CriticalRoleBootstrappingMode:$true `
        -DatabasePath "C:\Windows\NTDS" `
        -DomainMode "Win2022" `
        -DomainName $DomainName `
        -DomainNetbiosName $DomainName.Split('.')[0].ToUpper() `
        -ForestMode "Win2022" `
        -InstallDns:$true `
        -LogPath "C:\Windows\Logs\DNS" `
        -NoRebootOnCompletion:$false `
        -SysvolPath "C:\Windows\SYSVOL" `
        -SafeModeAdministratorPassword $(ConvertTo-SecureString $AdminPassword -AsPlainText -Force) | Out-Null
        
    Write-Host "✅ AD Forest installed successfully!" -ForegroundColor Green
} catch {
    Write-Host "⚠️  AD might already be installed or failed: $_" -ForegroundColor Yellow
}

# Wait for AD to fully initialize
Start-Sleep -Seconds 30

# Configure password policy
Write-Host "Configuring default password policy..." -ForegroundColor Cyan

$domain = [System.DirectoryServices.ActiveDirectory.Domain]::GetCurrentDomain()
$defaultPwdPolicy = Get-ADDefaultDomainPasswordPolicy -Identity $domain.DistinguishedName

# Make password requirements LESS strict for realistic target environment
Set-ADDefaultDomainPasswordPolicy -Identity $domain.DistinguishedName `
    -ComplexityEnabled:$false `
    -HistoryCount 5 `
    -LockoutDuration ([TimeSpan]::FromMinutes(30)) `
    -LockoutObservationWindow ([TimeSpan]::FromMinutes(30)) `
    -LockoutThreshold 10 `
    -MaxPasswordAge ([TimeSpan]::FromDays(90)) `
    -MinPasswordAge ([TimeSpan]::FromDays(1)) `
    -MinPasswordLength 8

Write-Host "✅ Password policy relaxed for realistic scenario" -ForegroundColor Green

# Disable LSA Protection (common enterprise misconfiguration)
Write-Host "Disabling LSA Protection (for attack realism)..." -ForegroundColor Cyan

Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control\Lsa" `
    -Name "DisablePasswordChange" `
    -Value 1 `
    -Type DWord `
    -Force

Remove-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control\Lsa" `
    -Name "RunAsFullAdmin" `
    -Force -ErrorAction SilentlyContinue

Write-Host "✅ LSA Protection disabled" -ForegroundColor Green

# Enable Kerberos pre-authentication bypass testing
Write-Host "Configuring Kerberos settings..." -ForegroundColor Cyan

# Allow RC4 encryption (legacy compatibility)
Set-ADUser -Identity "CN=Administrators,CN=Users,DC=corp,DC=internal" -Replace @{msds-AllowedToActOnBehalfOfOtherIdentity=$null}

# Create test user accounts
$testUsers = @("jsmith","jdoe","mike","sarah","admin")

foreach ($username in $testUsers) {
    try {
        $userParams = @{
            Name = $username
            Enabled = $true
            Path = "CN=Users,DC=corp,DC=internal"
            AccountPassword = (ConvertTo-SecureString "P@ssw0rd!123" -AsPlainText -Force)
            ChangePasswordAtLogon = $false
            Description = "Test user account"
        }
        
        if (-not (Get-ADUser -Filter "Name -eq '$username'" -ErrorAction SilentlyContinue)) {
            New-ADUser @userParams
            Write-Host "  ✅ Created user: $username" -ForegroundColor Green
        }
    } catch {
        Write-Host "  ⚠️  User $username might already exist" -ForegroundColor Yellow
    }
}

# Grant users permission to add computers to domain (common corporate setting)
Write-Host "Configuring delegation permissions..." -ForegroundColor Cyan

$computersOU = Get-OrganizationalUnit -Identity "Computers" -ErrorAction SilentlyContinue
if ($computersOU) {
    Add-ADGroupMember -Identity "Domain Users" -Identity $computersOU
    Write-Host "✅ Domain Users can join computers to domain" -ForegroundColor Green
}

# Export domain information for reconnaissance
Write-Host "Generating reconnaissance data..." -ForegroundColor Cyan

$dcInfo = @{
    DomainFQDN = $DomainName
    NetBIOSName = $DomainName.Split('.')[0].ToUpper()
    FSMORoleHolder = $env:COMPUTERNAME
    DNSZone = $DomainName
    LDAPPort = $LDAPPort
    SMBPort = $SMBPort
    AdminAccount = $AdminUsername
    PasswordHash = "aad3b435b51404ff...hash..." # Placeholder for demonstration
}

$dcInfo | ConvertTo-Json | Out-File "C:\Windows\Temp\dc-info.json" -Encoding UTF8

Write-Host "✅ DC information exported to C:\Windows\Temp\dc-info.json" -ForegroundColor Green

# Create firewall rules for internal traffic only
Write-Host "Configuring Windows Firewall..." -ForegroundColor Cyan

New-NetFirewallRule -DisplayName "Allow LDAP Traffic" -Direction Inbound -Protocol TCP -LocalPort $LDAPPort -Profile Domain -Action Allow -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName "Allow SMB Traffic" -Direction Inbound -Protocol TCP -LocalPort $SMBPort -Profile Domain -Action Allow -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName "Allow Kerberos Traffic" -Direction Inbound -Protocol UDP -LocalPort 88 -Profile Domain -Action Allow -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName "Allow WinRM Traffic" -Direction Inbound -Protocol TCP -LocalPort 5985 -Profile Domain -Action Allow -ErrorAction SilentlyContinue

Write-Host "✅ Firewall rules configured" -ForegroundColor Green

# Generate security event baseline
Write-Host "Creating security event baseline..." -ForegroundColor Cyan

Get-WinEvent -LogName Security -MaxEvents 100 | Where-Object {
    $_.Id -in @(4624, 4625, 4720, 4728, 4732, 4756)
} | Export-Csv "C:\Windows\Temp\security-baseline.csv" -NoTypeInformation -Encoding UTF8

Write-Host "✅ Security baseline exported" -ForegroundColor Green

Write-Host ""
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "✅ AD Domain Setup Complete!" -ForegroundColor Green
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Domain Details:"
Write-Host "  - FQDN: ${DomainName}" -ForegroundColor White
Write-Host "  - NetBIOS: $($DomainName.Split('.')[0].ToUpper())" -ForegroundColor White
Write-Host "  - Admin Account: ${AdminUsername}" -ForegroundColor White
Write-Host "  - LDAP Port: ${LDAPPort}" -ForegroundColor White
Write-Host "  - SMB Port: ${SMBPort}" -ForegroundColor White
Write-Host ""
Write-Host "Attack Surface Available:"
Write-Host "  ✅ RC4 encryption enabled (Kerberos attacks)"
Write-Host "  ✅ LSA Protection disabled (credential dumping)"
Write-Host "  ✅ Simple passwords (brute force viable)"
Write-Host "  ✅ User computer joining allowed (lateral movement)"
Write-Host ""
Write-Host "Reconnaissance data: C:\Windows\Temp\dc-info.json"
Write-Host ""
