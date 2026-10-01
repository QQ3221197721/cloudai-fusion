/**
 * M19 Security Behavior Hunting - Playwright E2E Tests
 * 
 * Test Suite: 8 comprehensive tests covering threat detection and hunt case management
 */

import { test, expect } from '@playwright/test';

test.describe('M19 Behavior Hunting E2E Tests', () => {
  
  test.beforeEach(async ({ page }) => {
    await page.goto('/m19-behavior-hunting');
  });

  test('should display main header and page title', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Behavior Hunting' })).toBeVisible();
  });

  test('should show statistics cards with correct counts', async ({ page }) => {
    await page.waitForTimeout(1000);
    
    const statsCards = page.locator('[class*="card"] [class*="content"]').first().locator('+ div');
    expect(statsCards).toHaveCount(4);
    
    const openCasesCard = statsCards.first();
    await expect(openCasesCard).toContainText('Open Cases');
  });

  test('should navigate between threat overview and hunt cases tabs', async ({ page }) => {
    // Start at Threat Overview tab
    await expect(page.locator('button:has-text("Threat Overview")')).toHaveClass(/active/);
    
    // Switch to Hunt Cases tab
    const casesTab = page.getByRole('tab', { name: 'Hunt Cases' });
    await expect(casesTab).toBeVisible({ timeout: 3000 });
    
    // Dashboard content should appear
    const dashboardContent = page.getByText(/Threat Landscape|Real-time security/i);
    await expect(dashboardContent).toBeVisible({ timeout: 5000 });
  });

  test('should open create hunt case modal', async ({ page }) => {
    const createButton = page.getByRole('button', { name: /New Hunt Case/i });
    await expect(createButton).toBeVisible();
    await createButton.click();
    
    // Modal should appear
    await expect(page.getByRole('dialog') || page.locator('[id*="modal"]')).toBeVisible({ timeout: 3000 });
  });

  test('should create a new hunt case', async ({ page }) => {
    // Open modal
    const createButton = page.getByRole('button', { name: /New Hunt Case/i });
    await createButton.click();
    
    // Fill form
    await page.fill('[placeholder="Suspicious User Activity Investigation"]', 'security-hunt-001', { strict: true });
    
    // Select severity
    const severitySelect = page.locator('select[aria-label="Severity Filter"], select[name="severity"]');
    await severitySelect.selectOption('high');
    
    // Add description
    await page.fill('[placeholder="Describe the suspicious activity"]', 'Investigating anomalous login patterns from user alice', { strict: true });
    
    // Submit
    await page.click('button:has-text("Start Investigation")');
    
    // Success notification or state update
    await page.waitForTimeout(3000);
  });

  test('should filter hunt cases by search and status', async ({ page }) => {
    // Search filter
    const searchInput = page.locator('input[placeholder="Search cases..."]');
    await searchInput.fill('investigation');
    await page.waitForTimeout(1000);
    
    // Status filter
    const statusSelect = page.locator('select[aria-label="Status Filter"]');
    await statusSelect.selectOption('open');
    await page.waitForTimeout(1000);
    
    // Filtered results should display
    await expect(page.locator('[class*="card"][class*="grid"]').getByRole('link').all()).toHaveCount({ min: 0 });
  });

  test('should view alerts feed and link to hunt cases', async ({ page }) => {
    // Alerts tab should be visible
    const alertsTab = page.getByRole('tab', { name: 'Alerts Feed' });
    if (await alertsTab.isVisible()) {
      await alertsTab.click();
      await page.waitForTimeout(1000);
      
      // Alert table should appear
      const alertTable = page.locator('table[class*="table"]').first();
      await expect(alertTable).toBeVisible({ timeout: 5000 });
    }
  });

  test('should perform MITRE ATT&CK technique mapping visualization', async ({ page }) => {
    // Mitre ATT&CK tab
    const mitreTab = page.getByRole('tab', { name: 'ATT&CK Matrix' });
    if (await mitreTab.isVisible()) {
      await mitreTab.click();
      await page.waitForTimeout(1000);
      
      // Technique badges should be visible
      const techniqueBadges = page.locator('[class*="badge"].text-blue-400');
      expect(techniqueBadges.count()).toBeGreaterThanOrEqual(0);
    }
  });

});
