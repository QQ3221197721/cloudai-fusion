package detect

import "testing"

// rules_coverage_test.go asserts that each Sigma rule added for the AISecOps
// L3-L8 detection layer actually fires on a realistic attack event and stays
// silent on the closest benign look-alike. Assertions target a SPECIFIC
// technique (rather than "no matches at all") so an unrelated rule firing on the
// same event cannot mask a broken rule — and so these cases stay stable as the
// rule set grows.

// assertFires fails unless some rule with wantTechnique matches the event.
func assertFires(t *testing.T, eng *Engine, category, wantTechnique string, ev map[string]any) {
	t.Helper()
	for _, m := range eng.Eval(category, ev) {
		if m.Technique == wantTechnique {
			return
		}
	}
	t.Fatalf("expected technique %s to fire for %v; got %+v", wantTechnique, ev, eng.Eval(category, ev))
}

// assertSilent fails if any rule with dontWantTechnique matches the event.
func assertSilent(t *testing.T, eng *Engine, category, dontWantTechnique string, ev map[string]any) {
	t.Helper()
	for _, m := range eng.Eval(category, ev) {
		if m.Technique == dontWantTechnique {
			t.Fatalf("technique %s must NOT fire for benign %v; matched rule %q", dontWantTechnique, ev, m.Title)
		}
	}
}

// TestRules_CredentialAccess covers LSASS dumping (T1003.001) and /etc/shadow
// theft (T1003.008).
func TestRules_CredentialAccess(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// procdump invoked against lsass → T1003.001
	assertFires(t, eng, "process_creation", "T1003.001", map[string]any{
		"Image":       `C:\tools\procdump64.exe`,
		"CommandLine": `procdump64.exe -accepteula -ma lsass.exe C:\temp\out.dmp`,
	})
	// comsvcs.dll MiniDump living-off-the-land variant → T1003.001
	assertFires(t, eng, "process_creation", "T1003.001", map[string]any{
		"Image":       `C:\Windows\System32\rundll32.exe`,
		"CommandLine": `rundll32.exe C:\windows\system32\comsvcs.dll,MiniDump 624 C:\temp\d.bin full`,
	})
	// Same dumping tool aimed at a harmless process must not fire the LSASS rule.
	assertSilent(t, eng, "process_creation", "T1003.001", map[string]any{
		"Image":       `C:\tools\procdump64.exe`,
		"CommandLine": `procdump64.exe -accepteula -ma notepad.exe C:\temp\np.dmp`,
	})

	// cat /etc/shadow → T1003.008
	assertFires(t, eng, "process_creation", "T1003.008", map[string]any{
		"Image":       `/usr/bin/cat`,
		"CommandLine": `cat /etc/shadow`,
	})
	// /etc/passwd is world-readable and not a hash source → must stay silent.
	assertSilent(t, eng, "process_creation", "T1003.008", map[string]any{
		"Image":       `/usr/bin/cat`,
		"CommandLine": `cat /etc/passwd`,
	})
}

// TestRules_LateralMovement covers WinRM lateral movement (T1021.006),
// including the benign-child filter.
func TestRules_LateralMovement(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// cmd.exe spawned by the WinRM host → remote execution → T1021.006
	assertFires(t, eng, "process_creation", "T1021.006", map[string]any{
		"ParentImage": `C:\Windows\System32\wsmprovhost.exe`,
		"Image":       `C:\Windows\System32\cmd.exe`,
		"CommandLine": `cmd.exe /c ipconfig /all`,
	})
	// conhost.exe is a normal WinRM child and is explicitly filtered.
	assertSilent(t, eng, "process_creation", "T1021.006", map[string]any{
		"ParentImage": `C:\Windows\System32\wsmprovhost.exe`,
		"Image":       `C:\Windows\System32\conhost.exe`,
	})
	// Same child with an ordinary parent is not WinRM lateral movement.
	assertSilent(t, eng, "process_creation", "T1021.006", map[string]any{
		"ParentImage": `C:\Windows\explorer.exe`,
		"Image":       `C:\Windows\System32\cmd.exe`,
	})
}

// TestRules_PrivilegeEscalation covers SYSTEM scheduled tasks (T1053.005) and
// setuid abuse/discovery (T1548.001).
func TestRules_PrivilegeEscalation(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// schtasks /create ... /ru system → T1053.005
	assertFires(t, eng, "process_creation", "T1053.005", map[string]any{
		"Image":       `C:\Windows\System32\schtasks.exe`,
		"CommandLine": `schtasks /create /tn Updater /tr C:\temp\evil.exe /sc minute /ru system`,
	})
	// Read-only enumeration of tasks must not fire.
	assertSilent(t, eng, "process_creation", "T1053.005", map[string]any{
		"Image":       `C:\Windows\System32\schtasks.exe`,
		"CommandLine": `schtasks /query /fo LIST`,
	})

	// SUID discovery sweep → T1548.001
	assertFires(t, eng, "process_creation", "T1548.001", map[string]any{
		"Image":       `/usr/bin/find`,
		"CommandLine": `find / -perm -4000 -type f 2>/dev/null`,
	})
	// Granting the setuid bit → T1548.001
	assertFires(t, eng, "process_creation", "T1548.001", map[string]any{
		"Image":       `/usr/bin/chmod`,
		"CommandLine": `chmod u+s /tmp/rootshell`,
	})
	// An ordinary find by filename must stay silent.
	assertSilent(t, eng, "process_creation", "T1548.001", map[string]any{
		"Image":       `/usr/bin/find`,
		"CommandLine": `find /var/log -name "*.log" -mtime -1`,
	})
}

// TestRules_ContainerEscape covers the three canonical escape primitives
// (T1611): host-namespace entry, cgroup release_agent abuse, and Docker socket
// access from inside a container.
func TestRules_ContainerEscape(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	assertFires(t, eng, "process_creation", "T1611", map[string]any{
		"Image":       `/usr/bin/nsenter`,
		"CommandLine": `nsenter -t 1 -m -u -i -n -p /bin/bash`,
	})
	assertFires(t, eng, "process_creation", "T1611", map[string]any{
		"Image":       `/usr/bin/bash`,
		"CommandLine": `echo /tmp/payload > /sys/fs/cgroup/rdma/release_agent`,
	})
	assertFires(t, eng, "process_creation", "T1611", map[string]any{
		"Image":       `/usr/bin/docker`,
		"CommandLine": `docker -H unix:///var/run/docker.sock run -v /:/host --privileged alpine`,
	})
	// Listing a runtime directory is not an escape.
	assertSilent(t, eng, "process_creation", "T1611", map[string]any{
		"Image":       `/usr/bin/ls`,
		"CommandLine": `ls -la /var/run/`,
	})
}
