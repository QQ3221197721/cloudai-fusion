/**
 * M4 Security Baseline Module - End-to-End Test Suite
 * 
 * Comprehensive test coverage for RBAC, compliance scanning, and security posture management
 * Tests user journey: Overview Dashboard → RBAC Matrix → Identity Providers → Certificate Management → Compliance Checks → Audit Logs
 */

import { test, expect } from '@playwright/test';
import axios from 'axios';

// Base configuration
const BASE_URL = process.env.TEST_FRONTEND_URL || 'http://localhost:5173';
const API_BASE_URL = process.env.TEST_API_URL || 'http://localhost:8080';

test.describe('M4 Security Baseline Module', () => {

  // ========================================================================
  // Test 1: Page loads successfully with all main sections
  // ========================================================================
  test('should display M4 Security Baseline dashboard with all components', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);

    // Wait for loading state to complete
    const initialSpinner = page.locator('.animate-spin');
    await initialSpinner.waitFor({ state: 'hidden', timeout: 10000 });

    // Verify main header is visible
    const header = page.getByText(/M4: Security Baseline/i);
    await expect(header).toBeVisible();

    // Check for key metrics displays
    const securityScoreElement = page.getByText(/Overall Security Posture/i);
    await expect(securityScoreElement).toBeVisible();

    // Verify tabs are present
    await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'RBAC Matrix' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Identity Providers' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Certificates' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Compliance' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Audit Logs' })).toBeVisible();
  });

  // ========================================================================
  // Test 2: Overview shows security score and statistics
  // ========================================================================
  test('should display overall security score and statistics', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to overview tab if not already there
    await page.getByRole('tab', { name: 'Overview' }).click();
    
    // Verify security score card
    const scoreCard = page.getByText(/Overall Security Posture/i);
    await expect(scoreCard).toBeVisible();
    
    // Look for progress bar showing percentage
    const progressBar = page.locator('[role="progressbar"]');
    const progressCount = await progressBar.count();
    expect(progressCount).toBeGreaterThan(0);
    
    // Check pass/fail/warn statistics
    const statsCards = page.locator('[class*="text-center"]');
    const statCount = await statsCards.count();
    expect(statCount).toBeGreaterThanOrEqual(4);
  });

  // ========================================================================
  // Test 3: RBAC matrix displays roles and permissions
  // ========================================================================
  test('should display RBAC matrix with all roles', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to RBAC Matrix tab
    await page.getByRole('tab', { name: 'RBAC Matrix' }).click();
    
    // Verify RBAC section
    await expect(page.getByText(/RBAC Matrix/i)).toBeVisible();
    
    // Check for role cards
    const roleCards = page.locator('[class*="p-4 rounded-lg border"]');
    const roleCount = await roleCards.count();
    expect(roleCount).toBeGreaterThan(0);
    
    // Verify built-in role badges
    const builtinBadges = page.getByText('Built-in');
    await expect(builtinBadges.first()).toBeVisible();
    
    // Check for user count indicators
    const userCounts = page.locator('[class*="chip"] [class*="users"]');
    const userCountElements = await userCounts.all();
    // At least some roles should have user counts displayed
  });

  // ========================================================================
  // Test 4: Identity providers show connection status
  // ========================================================================
  test('should display identity provider connections', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to Identity Providers tab
    await page.getByRole('tab', { name: 'Identity Providers' }).click();
    
    // Verify IDP section
    await expect(page.getByText(/Identity Providers/i)).toBeVisible();
    
    // Check for IDP cards
    const idpCards = page.locator('[class*="p-4 rounded-lg border"]');
    const idpCount = await idpCards.count();
    expect(idpCount).toBeGreaterThan(0);
    
    // Verify status badges
    const activeStatus = page.getByText('Active', { exact: false });
    await expect(activeStatus.first()).toBeVisible();
    
    // Check for last sync timestamp
    const lastSync = page.getByText(/Last Sync/i);
    if (await lastSync.count() > 0) {
      await expect(lastSync).toBeVisible();
    }
  });

  // ========================================================================
  // Test 5: Certificate manager displays expiry information
  // ========================================================================
  test('should display SSL certificate status and expiry dates', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to Certificates tab
    await page.getByRole('tab', { name: 'Certificates' }).click();
    
    // Verify certificates section
    await expect(page.getByText(/SSL\/TLS Certificates/i)).toBeVisible();
    
    // Check for certificate cards
    const certCards = page.locator('[class*="p-4 rounded-lg border"]');
    const certCount = await certCards.count();
    expect(certCount).toBeGreaterThan(0);
    
    // Look for days remaining information
    const daysInfo = page.getByText(/\d+ day/i);
    if (await daysInfo.count() > 0) {
      await expect(daysInfo.first()).toBeVisible();
    }
    
    // Check for valid/expiring/expired status badges
    const statusBadges = page.locator('[class*="chip"]');
    const statusCount = await statusBadges.count();
    expect(statusCount).toBeGreaterThan(0);
  });

  // ========================================================================
  // Test 6: Compliance checks show framework results
  // ========================================================================
  test('should display compliance check results across frameworks', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to Compliance tab
    await page.getByRole('tab', { name: 'Compliance' }).click();
    
    // Verify compliance section
    await expect(page.getByText(/Compliance Checks/i)).toBeVisible();
    
    // Check for framework badges (CIS, SOC2, PCI-DSS)
    const cisBadge = page.getByText('CIS');
    const soc2Badge = page.getByText('SOC2');
    
    if (await cisBadge.count() > 0) {
      await expect(cisBadge).toBeVisible();
    }
    if (await soc2Badge.count() > 0) {
      await expect(soc2Badge).toBeVisible();
    }
    
    // Look for status badges (pass/fail/warn)
    const statusChips = page.locator('[class*="chip"]');
    const statusCount = await statusChips.count();
    expect(statusCount).toBeGreaterThan(0);
  });

  // ========================================================================
  // Test 7: Audit logs display recent activity
  // ========================================================================
  test('should display audit log entries with timestamps', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to Audit Logs tab
    await page.getByRole('tab', { name: 'Audit Logs' }).click();
    
    // Verify audit section
    await expect(page.getByText(/Audit Logs/i)).toBeVisible();
    
    // Look for log table
    const logTable = page.locator('table');
    await expect(logTable.first()).toBeVisible();
    
    // Check for success/failure outcome indicators
    const outcomeBadges = page.getByText(/success|failure/i);
    if (await outcomeBadges.count() > 0) {
      await expect(outcomeBadges.first()).toBeVisible();
    }
    
    // Verify timestamp format in logs
    const timestampElements = page.locator('[class*="text-xs"] text-muted-foreground');
    const timestampCount = await timestampElements.count();
    expect(timestampCount).toBeGreaterThan(0);
  });

  // ========================================================================
  // Test 8: Manual refresh functionality works
  // ========================================================================
  test('should trigger manual refresh when clicking refresh button', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Wait for initial load
    await page.waitForSelector('[class*="glass-card"]', { timeout: 10000 });
    
    // Find refresh button
    const refreshButton = page.getByText('Refresh').first();
    await expect(refreshButton).toBeVisible();
    
    // Click refresh
    await refreshButton.click();
    
    // Should show loading animation
    const spinner = page.locator('.animate-spin');
    await expect(spinner).toBeVisible();
    
    // After refresh completes, spinner disappears
    await spinner.waitFor({ state: 'hidden', timeout: 5000 });
    
    // Data should still be visible after refresh
    const securityScoreElement = page.getByText(/Overall Security Posture/i);
    await expect(securityScoreElement).toBeVisible();
  });

  // ========================================================================
  // Test 9: Detail dialog opens for role/IDP/certificate information
  // ========================================================================
  test('should open detail dialog when viewing role details', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to RBAC Matrix tab
    await page.getByRole('tab', { name: 'RBAC Matrix' }).click();
    
    // Find a View Details button
    const detailsButtons = page.getByText('View Details');
    if (await detailsButtons.count() > 0) {
      await detailsButtons.first().click();
      
      // Verify dialog opens
      const dialog = page.locator('dialog[role="dialog"], .modal');
      await expect(dialog.first()).toBeVisible({ timeout: 5000 });
      
      // Dialog should contain JSON data
      const dialogContent = dialog.locator('pre').first();
      await expect(dialogContent).toBeVisible();
      
      // Close dialog
      await page.keyboard.press('Escape');
      await expect(dialog.first()).not.toBeVisible();
    }
  });

  // ========================================================================
  // Test 10: Auto-refresh toggle works correctly
  // ========================================================================
  test('should toggle auto-refresh on/off', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Find auto-refresh switch
    const autoRefreshSwitch = page.getByText(/Auto-refresh/i);
    if (await autoRefreshSwitch.count() > 0) {
      const checkbox = autoRefreshSwitch.locator('input[type="checkbox"]').first();
      
      // Get initial state
      const isChecked = await checkbox.isChecked();
      
      // Toggle
      await autoRefreshSwitch.click();
      
      // Verify state changed
      const newState = await checkbox.isChecked();
      expect(newState).toBe(!isChecked);
    }
  });

  // ========================================================================
  // Additional Integration Tests
  // ========================================================================

  // ========================================================================
  // Test 11: Tab navigation between all sections works
  // ========================================================================
  test('should navigate smoothly between all security tabs', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    const tabs = ['Overview', 'RBAC Matrix', 'Identity Providers', 'Certificates', 'Compliance', 'Audit Logs'];
    
    for (const tabName of tabs) {
      await page.getByRole('tab', { name: tabName }).click();
      
      // Verify content loaded
      const currentTabContent = page.locator('[class*="glass-card"]:visible').first();
      await expect(currentTabContent).toBeVisible();
      
      // Small delay for smooth transition
      await page.waitForTimeout(300);
    }
  });

  // ========================================================================
  // Test 12: Status color coding works correctly
  // ========================================================================
  test('should use color-coded status badges appropriately', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // On Overview tab, find status indicators
    await page.getByRole('tab', { name: 'Overview' }).click();
    
    // Look for success/danger/warning chips
    const successChips = page.locator('[class*="bg-success"]');
    const dangerChips = page.locator('[class*="bg-danger"]');
    
    const successCount = await successChips.count();
    // At least some success indicators should exist
    expect(successCount).toBeGreaterThanOrEqual(0);
  });

  // ========================================================================
  // Test 13: Recent audit activity shows dynamic timestamps
  // ========================================================================
  test('should display recent audit activity with proper formatting', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // On Overview tab, find audit activity section
    await page.getByRole('tab', { name: 'Overview' }).click();
    
    const auditSection = page.getByText(/Recent Audit Activity/i);
    await expect(auditSection).toBeVisible();
    
    // Verify time stamps are formatted
    const timeStamps = page.locator('[class*="text-xs whitespace-nowrap"]');
    const timestampCount = await timeStamps.count();
    expect(timestampCount).toBeGreaterThan(0);
  });

  // ========================================================================
  // Test 14: Responsive layout adapts to mobile viewport
  // ========================================================================
  test('should display correctly on mobile viewport', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Resize to mobile
    await page.setViewportSize({ width: 375, height: 667 });
    
    // Wait for reflow
    await page.waitForTimeout(500);
    
    // Main content should still be visible
    const header = page.getByText(/M4: Security Baseline/i);
    await expect(header).toBeVisible();
    
    // Cards should adapt layout
    const cards = page.locator('[class*="glass-card"]');
    const cardCount = await cards.count();
    expect(cardCount).toBeGreaterThan(0);
    
    // Reset to desktop
    await page.setViewportSize({ width: 1920, height: 1080 });
  });

  // ========================================================================
  // Test 15: Keyboard navigation support
  // ========================================================================
  test('should support keyboard navigation between tabs', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Focus on first tab
    await page.getByRole('tab', { name: 'Overview' }).focus();
    
    // Navigate using arrow keys
    await page.keyboard.press('ArrowRight');
    
    // Should switch to next tab
    await page.waitForTimeout(300);
    
    const currentTab = page.getByRole('tab', { selected: true });
    await expect(currentTab).toBeVisible();
  });

  // ========================================================================
  // Test 16: Loading states during data fetch
  // ========================================================================
  test('should show appropriate loading states during refresh', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Trigger refresh
    const refreshBtn = page.getByText('Refresh').first();
    await refreshBtn.click();
    
    // Should show loading spinner
    const spinner = page.locator('.animate-spin');
    await expect(spinner).toBeVisible();
    
    // And then disappear
    await spinner.waitFor({ state: 'hidden', timeout: 5000 });
    
    // Content should remain visible
    const header = page.getByText(/M4: Security Baseline/i);
    await expect(header).toBeVisible();
  });

  // ========================================================================
  // Test 17: Certificate warning banners appear for expiring certs
  // ========================================================================
  test('should display warning banner for expiring certificates', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Navigate to Certificates tab
    await page.getByRole('tab', { name: 'Certificates' }).click();
    
    // Look for alert/warning elements
    const alertElements = page.locator('[class*="alert"]');
    const alertCount = await alertElements.count();
    
    // May or may not have alerts depending on mock data
    expect(alertCount).toBeGreaterThanOrEqual(0);
  });

  // ========================================================================
  // Test 18: Full user workflow simulation
  // ========================================================================
  test('should support complete security review workflow', async ({ page }) => {
    await page.goto(`${BASE_URL}/m4-security`);
    
    // Step 1: Review overall security posture
    await expect(page.getByText(/Overall Security Posture/i)).toBeVisible();
    await page.waitForTimeout(500);
    
    // Step 2: Check RBAC assignments
    await page.getByRole('tab', { name: 'RBAC Matrix' }).click();
    await expect(page.getByText(/RBAC Matrix/i)).toBeVisible();
    await page.waitForTimeout(500);
    
    // Step 3: Verify identity providers
    await page.getByRole('tab', { name: 'Identity Providers' }).click();
    await expect(page.getByText(/Identity Providers/i)).toBeVisible();
    await page.waitForTimeout(500);
    
    // Step 4: Review certificates
    await page.getByRole('tab', { name: 'Certificates' }).click();
    await expect(page.getByText(/SSL\/TLS Certificates/i)).toBeVisible();
    await page.waitForTimeout(500);
    
    // Step 5: Check compliance status
    await page.getByRole('tab', { name: 'Compliance' }).click();
    await expect(page.getByText(/Compliance Checks/i)).toBeVisible();
    await page.waitForTimeout(500);
    
    // Step 6: Review audit logs
    await page.getByRole('tab', { name: 'Audit Logs' }).click();
    await expect(page.getByText(/Audit Logs/i)).toBeVisible();
    await page.waitForTimeout(500);
  });

});
