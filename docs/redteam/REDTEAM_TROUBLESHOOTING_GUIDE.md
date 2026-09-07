# CloudAI Fusion Red Team Platform - Troubleshooting Guide

**Version**: 1.0 Early Access Beta  
**Last Updated**: September 6, 2026

---

## 🚨 Common Issues & Solutions

### Issue 1: Frontend Doesn't Load (`http://localhost:3000` shows blank page)

#### Symptoms:
- Browser shows white/blank screen
- Console shows "Failed to load module" errors
- No login page visible

#### Root Causes & Fixes:

**Cause A: Missing Dependencies**
```bash
cd cloudai-fusion/frontend
npm install --verbose
```

**Cause B: Vite Build Error**
```
(!) Failed to run dependency scan. Skipping dependency pre-bundling.
Error: The following dependencies are imported but could not be resolved:
```

**Fix**: Run `npm run dev` instead of trying to build first. Dev server handles missing deps dynamically.

**Cause C: Port Conflict**
```bash
# Check if port 3000 is already in use
netstat -ano | findstr :3000

# Kill conflicting process (replace PID with actual number)
taskkill /PID <PID> /F
```

---

### Issue 2: Login Button Does Nothing

#### Symptoms:
- Click "Sign In" button
- Loading spinner appears then disappears
- No navigation to `/dashboard`
- No error message shown

#### Analysis:
This is **EXPECTED BEHAVIOR** in Early Access Beta:
- Login accepts ANY credentials (demo mode only)
- Redirects after 500ms hardcoded delay
- No backend authentication happens
- Zustand store persists auth state to localStorage

#### Verification:
Open DevTools → Application → Local Storage → Look for key `auth-storage`:
```json
{
  "user": {"username": "demo"},
  "isAuthenticated": true
}
```

If present, login worked correctly (mock mode).

---

### Issue 3: Dashboard Shows All Zeroes

#### Symptoms:
- Dashboard stats cards show: Total Scans (0), Active Campaigns (0), etc.
- No loading skeleton displayed initially
- Values appear instantly

#### This Is Expected!

**Reason**: `useQuery` hook uses `placeholderData` option:
```typescript
const { data: stats, isLoading } = useQuery({
  queryKey: ["dashboard_stats"],
  queryFn: () => apiClient.getDashboardStats(),
  placeholderData: { // ← This makes numbers appear instantly as "0"
    totalScans: 0,
    activeCampaigns: 0,
    threatsFound: 0,
    pendingOrders: 0,
  },
});
```

**Backend Not Running**: Even without `placeholderData`, API calls would fail because Go apiserver is not running.

#### How To Fix (For Development Testing):

Start Go backend:
```bash
cd cloudai-fusion/cmd/apiserver
go run main.go --config ../../cloudai-fusion.yaml
```

Then frontend will attempt real API calls and show 404/NetworkError in console (expected until endpoints implemented).

---

### Issue 4: Work Order Wizard Submit Fails Silently

#### Symptoms:
- Fill out all 4 steps
- Click "Confirm & Submit ✓" on step 4
- Loading spinner appears
- Spinner never stops
- Page doesn't redirect

#### Root Cause:
```typescript
// From src/pages/WorkOrderSubmit.tsx line 78
try {
  await apiClient.submitWorkOrder(formData); // ← This throws NetworkError
  setSubmitSuccess(true); // ← Never reached
} catch (error: any) {
  alert(error.message || "Failed to submit work order"); // ← Suppressed by silent catch
  setIsSubmitting(false); // ← Missing in some code paths
}
```

The endpoint `/api/work-orders` does NOT exist yet. Backend integration phase 2 required.

#### Temporary Test Method:
Instead of full submission, test step navigation only:
1. Step 1 → Click "Next" → Animates to step 2 (works)
2. Step 2 → Click "Back" → Returns to step 1 (works)
3. Form validation triggers on empty fields (works)
4. File picker opens OS dialog on step 3 (works, but upload disabled)

This verifies UI components without requiring backend.

---

### Issue 5: Browser Console Shows "Unsupported Vite Config" Warning

#### Output:
```
(!) Your Vite config uses features that are unsupported by `configLoader: 'native'`,
    which is planned to become the default in a future major version of Vite:
  - `__dirname` (vite.config.js:9:31). Use `import.meta.dirname` instead
```

#### Impact: None currently
This is just a deprecation warning for future Vite versions. Does NOT affect current functionality.

#### Fix (Optional):
Edit `vite.config.js` line 9:
```javascript
// Old:
const __dirname = path.dirname(new URL(import.meta.url).pathname);

// New (Vite 9+ compatible):
const __dirname = path.dirname(fileURLToPath(import.meta.url));
```

Or suppress warning:
```powershell
$env:VITE_CONFIG_NATIVE_IGNORE_WARNING = "true"
npm run dev
```

---

### Issue 6: Mobile View Horizontal Scroll on Narrow Screens (<320px)

#### Symptoms:
- Resize browser to 300px width
- Work Order page shows horizontal scrollbar
- Content spills off-screen left

#### Root Cause:
```tsx
// From src/pages/WorkOrderSubmit.tsx line 201-208
<textarea
  id="justification"
  rows={5}
  className="w-full min-h-[150px] p-4 bg-slate-800/50 ..."
  // ← Missing max-width constraint
/>
```

#### Fix Required:
Add Tailwind class `max-w-full`:
```tsx
className="max-w-full w-full min-h-[150px] p-4 ..."
```

**Status**: Known cosmetic issue, LOW priority since phones rarely render below 320px width.

---

### Issue 7: Invalid Routes Redirect to Dashboard Instead of 404 Page

#### Example:
Navigate to `http://localhost:3000/asdfasdf12345`

Result: Silently redirects to `/dashboard` instead of showing "Page Not Found"

#### Root Cause:
```typescript
// From src/App.tsx line 98
<Route path="*" element={<Navigate to="/dashboard" replace />} />
```

All unmatched routes fallback to dashboard redirect.

#### Intentional Design?
Actually this seems like incomplete implementation. Should show custom 404 component:

```tsx
function NotFound() {
  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="text-center space-y-4">
        <h1 className="text-6xl font-bold">404</h1>
        <p className="text-gray-400">Page Not Found</p>
        <Link to="/dashboard">
          <Button>Return Home</Button>
        </Link>
      </div>
    </div>
  );
}

// Add route:
<Route path="*" element={<NotFound />} />
```

**Status**: Feature request, not critical bug.

---

### Issue 8: Authentication Bypass Possible

#### Critical Security Finding:

Anyone can log in by entering ANY username/password combination:

```typescript
// From src/stores/auth-store.ts line 29
const response = await apiClient.login({ username, password });

// apiClient.login() returns:
return {
  user: { username: "demo", role: "user" }, // ← Hardcoded mock response
  token: "mock-jwt-token-not-verified",     // ← No JWT signature check
};
```

**Implication**: 
- Unauthorized users can access protected routes
- No session expiry or refresh logic
- Token stored in localStorage (XSS vulnerable)

#### Production Requirements Phase 2:

Implement proper JWT flow:
```typescript
// POST /api/auth/login
POST { username: "real_user", password: "secure_pass" }
→ Return: { token: "jwt-with-signature", expires_at: timestamp }
→ Store in HttpOnly cookie (not localStorage)
→ Verify signature on every protected route
→ Refresh token before expiry
```

**Status**: CRITICAL security gap, MUST fix before production use.

---

## 🔧 Advanced Debugging Techniques

### Enable Verbose Logging

Add to `frontend/src/lib/api.ts`:
```typescript
export const apiClient = {
  async login(credentials: LoginForm) {
    console.log("🔐 Login attempt:", credentials.username); // ← Add debug log
    const response = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(credentials),
    });
    
    if (!response.ok) {
      console.error("❌ Login failed:", response.status, response.statusText);
      throw new Error(`Login error: ${response.statusText}`);
    }
    
    const data = await response.json();
    console.log("✅ Login success:", data);
    return data;
  },
  // ... rest of methods
};
```

### Monitor Network Calls

Chrome DevTools → Network tab:
- Filter by "Fetch/XHR"
- Look for failed requests (red status codes)
- Inspect request payloads
- Check response bodies for error details

### State Inspector

Install Redux DevTools extension for React:
- View Zustand store mutations in real-time
- Revert state changes (time travel debugging)
- Export/import state snapshots for analysis

### Performance Profiler

DevTools → Performance tab:
- Record page load sequence
- Identify slow render cycles
- Detect memory leaks
- Measure Time-to-Interactive (TTI)

---

## 📞 Support Resources

### Documentation Index:
- [Release Notes](./RETEAM_1.0_RELEASE_NOTES.md) - Full honest status report
- [Quick Start](../../docs/quickstart.md) - Installation instructions
- [API Reference](../../api/openapi.yaml) - Backend endpoint specs
- [Architecture Guide](../../docs/architecture.md) - System design document

### Community Channels:
- GitHub Issues: https://github.com/cloudai-fusion/cloudai-fusion/issues
- Discussions: https://github.com/cloudai-fusion/cloudai-fusion/discussions
- Slack Workspace: [pending setup]

### Contributing Guidelines:
See [CONTRIBUTING.md](../../CONTRIBUTING.md) for development workflows and PR requirements.

---

## 🎯 When To Expect Fixes

| Issue Severity | Expected Resolution Timeline |
|----------------|------------------------------|
| 🔴 Critical | Phase 2 API Integration (3-4 weeks) |
| 🟡 Medium | Phase 2 + UX Refinement (4-6 weeks) |
| 🟢 Low | Phase 3 Feature Completion (6-8 weeks) |

**MVP Target Date**: December 6, 2026

---

**Document Version**: 1.0  
**Maintained By**: CloudAI Fusion Engineering Team  
**License**: Apache 2.0 | GitHub: github.com/cloudai-fusion/cloudai-fusion
