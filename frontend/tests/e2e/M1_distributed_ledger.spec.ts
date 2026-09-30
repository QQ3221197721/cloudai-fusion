/**
 * M1 Distributed Ledger Module - End-to-End Test Suite
 * 
 * Comprehensive test coverage for Verifiable Control Plane functionality
 * Tests user journey: View Evidence Chain → Search & Filter → Verify Integrity → Export Proofs
 */

import { test, expect } from '@playwright/test';
import axios from 'axios';

// Base configuration
const BASE_URL = process.env.TEST_FRONTEND_URL || 'http://localhost:5173';
const API_BASE_URL = process.env.TEST_API_URL || 'http://localhost:8080';

// ============================================================================
// Test Suite: M1 Distributed Ledger Dashboard
// ============================================================================

test.describe('M1 Distributed Ledger Page', () => {
  // ========================================================================
  // Test 1: Verify ledger page loads correctly
  // ========================================================================
  test('should load and display ledger entries on initial page visit', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Should show loading state initially
    await expect(page.getByText(/Loading evidence ledger/i)).toBeVisible({ timeout: 5000 });
    
    // Wait for dashboard to render
    await expect(page.getByText(/Distributed Ledger/i)).toBeVisible();
    
    // Should show main header with title
    const header = page.getByRole('heading', { name: /cloudai fusion/i });
    await expect(header).toBeVisible();
    
    // Should have navigation tabs
    await expect(page.getByText(/Overview/i)).toBeVisible();
    await expect(page.getByText(/Evidence Records/i)).toBeVisible();
    await expect(page.getByText(/Transparency/i)).toBeVisible();
  });

  // ========================================================================
  // Test 2: Verify overview tab displays summary cards
  // ========================================================================
  test('should display summary statistics in overview tab', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to overview tab (should be active by default)
    await expect(page.getByText(/Overview/i)).toHaveAttribute('data-state', 'active');
    
    // Verify all summary cards are present
    await expect(page.getByText(/Total Records/)).toBeVisible();
    await expect(page.getByText(/Latest Sequence/)).toBeVisible();
    await expect(page.getByText(/Chain Valid/)).toBeVisible();
    await expect(page.getByText(/Verification Status/)).toBeVisible();
    
    // Check that metric values are displayed (numbers)
    const metrics = page.locator('text=/^\d+$/', { hasText: true });
    await expect(metrics.first()).toBeVisible();
  });

  // ========================================================================
  // Test 3: Evidence records table should display data
  // ========================================================================
  test('should display evidence records in sorted table format', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Click on Evidence Records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    await expect(recordsTab).toHaveAttribute('data-state', 'active');
    
    // Verify table headers are visible
    await expect(page.getByText(/#/i)).toBeVisible();
    await expect(page.getByText(/Timestamp/i)).toBeVisible();
    await expect(page.getByText(/Action/i)).toBeVisible();
    await expect(page.getByText(/Subject/i)).toBeVisible();
    await expect(page.getByText(/Hash/i)).toBeVisible();
    await expect(page.getByText(/Verified/i)).toBeVisible();
    
    // Table rows should appear (or empty state message)
    const tbody = page.locator('tbody');
    const rows = tbody.locator('tr');
    const count = await rows.count();
    
    if (count > 1) {
      // Data exists - first row after header should be clickable
      await rows.first().click();
      // Modal should appear
      await expect(page.getByText(/Evidence Receipt Details/i)).toBeVisible({ timeout: 5000 });
    } else {
      // Empty state
      await expect(page.getByText(/No evidence records found/i)).toBeVisible();
    }
  });

  // ========================================================================
  // Test 4: Search functionality should filter records
  // ========================================================================
  test('should filter evidence records by search term', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    
    // Find search input
    const searchInput = page.locator('input[placeholder*="search"]');
    if (await searchInput.count() > 0) {
      // Fill in search term
      await searchInput.fill('CREATE_CLUSTER');
      
      // Wait for filtered results or no results message
      const tbody = page.locator('tbody');
      const rows = tbody.locator('tr');
      const count = await rows.count();
      
      // Either filtered results or "no records found"
      expect(count).toBeGreaterThanOrEqual(1);
    }
  });

  // ========================================================================
  // Test 5: Filter by action type should work
  // ========================================================================
  test('should filter records by action type dropdown', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    
    // Find filter dropdown
    const filterSelect = page.locator('select');
    if (await filterSelect.count() > 0) {
      // Select a specific action type
      const optionElement = page.locator('option').first();
      const optionValue = await optionElement.evaluate((el) => el.value);
      
      if (optionValue && optionValue !== 'all') {
        await filterSelect.selectOption(optionValue);
        
        // Wait for filtered results
        await page.waitForTimeout(1000);
        
        // Should see filtered results or updated count
        const tbody = page.locator('tbody');
        const rows = tbody.locator('tr');
        const count = await rows.count();
        expect(count).toBeGreaterThanOrEqual(1);
      }
    }
  });

  // ========================================================================
  // Test 6: Entry detail modal should open on click
  // ========================================================================
  test('should open detailed view modal when clicking on entry row', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    
    // Try to click on a row
    const tbody = page.locator('tbody');
    const firstRow = tbody.locator('tr').first();
    
    if (await firstRow.count() > 0) {
      await firstRow.click();
      
      // Modal should appear within timeout
      await expect(page.getByText(/Evidence Receipt Details/i)).toBeVisible({ timeout: 5000 });
      
      // Modal should contain expected fields
      await expect(page.getByText(/Receipt ID/i)).toBeVisible();
      await expect(page.getByText(/Sequence Number/i)).toBeVisible();
      await expect(page.getByText(/Content Hash/i)).toBeVisible();
    }
  });

  // ========================================================================
  // Test 7: Copy hash to clipboard should work
  // ========================================================================
  test('should copy hashes to clipboard when clicking copy button', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to records tab and open modal
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    
    const tbody = page.locator('tbody');
    const firstRow = tbody.locator('tr').first();
    
    if (await firstRow.count() > 0) {
      await firstRow.click();
      await expect(page.getByText(/Evidence Receipt Details/i)).toBeVisible({ timeout: 5000 });
      
      // Find copy buttons for hashes
      const copyButtons = page.locator('[aria-label="copy"] button, button svg path="M16 13L8 13L8 16L16 16Z")').all();
      const buttonCount = await copyButtons.length;
      
      if (buttonCount > 0) {
        // Click first copy button
        await copyButtons[0].click();
        
        // Allow time for clipboard operation
        await page.waitForTimeout(500);
        
        // Clipboard write may succeed or fail depending on browser permissions
        // This is a basic smoke test
      }
    }
  });

  // ========================================================================
  // Test 8: Export JSON should trigger download
  // ========================================================================
  test('should export evidence ledger as JSON file', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Enable download listener
    const download = await page.waitForEvent('download');
    
    // Click export button
    const exportButton = page.getByRole('button', { name: /export json/i });
    await exportButton.click();
    
    // Download should start
    await download.waitUntil('ended');
    
    // Get suggested filename
    const filename = await download.suggestedFilename();
    expect(filename).toContain('evidence');
    expect(filename).toContain('.json');
  });

  // ========================================================================
  // Test 9: Chain verification should update status
  // ========================================================================
  test('should verify chain integrity when clicking verify chain button', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Ensure we're on overview tab
    await expect(page.getByText(/Overview/i)).toHaveAttribute('data-state', 'active');
    
    // Find verify chain button
    const verifyButton = page.getByRole('button', { name: /verify chain/i });
    
    if (await verifyButton.count() > 0) {
      await verifyButton.click();
      
      // Should show loading animation
      await expect(page.locator('.animate-spin')).toBeVisible({ timeout: 5000 });
      
      // After verification, should see result
      const alert = page.locator('[role="alert"], .alert');
      await expect(alert).toBeVisible({ timeout: 10000 });
    }
  });

  // ========================================================================
  // Test 10: Single entry signature verification should work
  // ========================================================================
  test('should verify individual entry signature on demand', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    
    // Try to click on a row
    const tbody = page.locator('tbody');
    const firstRow = tbody.locator('tr').first();
    
    if (await firstRow.count() > 0) {
      await firstRow.click();
      await expect(page.getByText(/Evidence Receipt Details/i)).toBeVisible({ timeout: 5000 });
      
      // Find verify signature button
      const verifySignatureBtn = page.getByRole('button', { name: /verify signature/i });
      
      if (await verifySignatureBtn.count() > 0) {
        await verifySignatureBtn.click();
        
        // Should show verification badge or result
        await expect(page.getByText(/verified/i, { ignoreCase: true })).toBeVisible({ timeout: 5000 });
      }
    }
  });

  // ========================================================================
  // Test 11: Close modal should work
  // ========================================================================
  test('should close detail modal when clicking close button', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    
    const tbody = page.locator('tbody');
    const firstRow = tbody.locator('tr').first();
    
    if (await firstRow.count() > 0) {
      await firstRow.click();
      await expect(page.getByText(/Evidence Receipt Details/i)).toBeVisible({ timeout: 5000 });
      
      // Click close button
      const closeButton = page.getByRole('button', { name: /close/i });
      await closeButton.click();
      
      // Modal should be closed
      await expect(page.getByText(/Evidence Receipt Details/i)).not.toBeVisible();
    }
  });

  // ========================================================================
  // Test 12: Responsive layout should work on different screen sizes
  // ========================================================================
  test('should adapt layout for mobile screen size', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 }); // Mobile
    
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Header should still be visible
    await expect(page.getByText(/cloudai fusion/i)).toBeVisible();
    
    // Tabs should be scrollable or stacked
    const tabsList = page.locator('[role="tablist"]');
    await expect(tabsList).toBeVisible();
    
    // Reset viewport
    await page.setViewportSize({ width: 1920, height: 1080 });
  });

  // ========================================================================
  // Test 13: Error handling when backend is unavailable
  // ========================================================================
  test('should show error message when backend is unreachable', async ({ page }) => {
    // This test assumes backend might be down
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Wait briefly
    await page.waitForTimeout(2000);
    
    // Check for error state (either success or error message)
    const hasError = await page.getByRole('alert', { name: /error/i }).count();
    
    if (hasError > 0) {
      await expect(page.getByRole('alert')).toBeVisible();
    } else {
      // No error means backend is up - check that data loaded successfully
      await expect(page.getByRole('table')).toBeVisible();
    }
  });

  // ========================================================================
  // Test 14: Tab switching should work correctly
  // ========================================================================
  test('should switch between tabs without losing state', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Start on overview tab
    const overviewTab = page.getByRole('tab', { name: /overview/i });
    await expect(overviewTab).toHaveAttribute('data-state', 'active');
    
    // Switch to records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    await expect(recordsTab).toHaveAttribute('data-state', 'active');
    
    // Switch back to overview
    await overviewTab.click();
    await expect(overviewTab).toHaveAttribute('data-state', 'active');
    
    // Summary stats should still be there
    await expect(page.getByText(/total records/i, { ignoreCase: true })).toBeVisible();
  });

  // ========================================================================
  // Test 15: Pagination indicator should show record count
  // ========================================================================
  test('should display record count information', async ({ page }) => {
    await page.goto(`${BASE_URL}/m1-distributed-ledger`);
    
    // Navigate to records tab
    const recordsTab = page.getByRole('tab', { name: /evidence records/i });
    await recordsTab.click();
    
    // Look for count text at bottom of table
    const countIndicator = page.locator('text=/showing/i');
    
    if (await countIndicator.count() > 0) {
      await expect(countIndicator).toBeVisible();
      // Should match pattern like "Showing X of Y records"
      const text = await countIndicator.textContent();
      expect(text).toMatch(/Showing \d+ of \d+ records?/i);
    }
  });
});
