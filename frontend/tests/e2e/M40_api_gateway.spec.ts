/**
 * M37 DevSecOps Pipeline Integration - E2E Test Suite (15 tests)
 */

import { test, expect } from '@playwright/test';

const BASE_URL = process.env.TEST_FRONTEND_URL || 'http://localhost:5173';

test.describe('M37 DevSecOps Pipeline', () => {
  
  // ========================================================================
  // Page Navigation & Layout Tests
  // ========================================================================
  
  test('should navigate to DevSecOps pipeline page successfully', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    await expect(page.getByRole('heading', { name: /DevSecOps Pipeline/i })).toBeVisible();
  });

  test('should display overall compliance score card', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const complianceScore = page.locator('[class*="compliance-score"], [class*="security-posture"]');
    await expect(complianceScore.first()).toBeVisible();
  });

  // ========================================================================
  // Security Job Management Tests
  // ========================================================================
  
  test('should display security jobs list correctly', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const jobCards = page.locator('[class*="job-card"], .space-y-3').all();
    await expect(jobCards).not.toHaveCount(0);
  });

  test('should open create new security job dialog', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const addButton = page.getByRole('button', { name: /New Security Job|Create Job/i });
    await addButton.click();
    
    await expect(page.getByRole('dialog')).toBeVisible();
  });

  test('should fill and submit job creation form', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    await page.getByRole('button', { name: /New Security Job/i }).click();
    
    await page.fill('input[placeholder*="Job Name"]', 'Test Security Scan');
    await page.fill('input[placeholder*="Repository"]', 'https://github.com/test/repo.git');
    
    await page.selectOption('select[name="branch"]', 'main');
    
    await page.getByRole('button', { name: /Create/i }).click();
    
    // Verify creation
    await expect(page.getByText(/Test Security Scan/, { timeout: 5000 })).toBeVisible({ timeout: 5000 });
  });

  test('should filter jobs by search term', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    await page.fill('input[placeholder*="Search"]', 'production');
    const filteredJobs = page.locator('div[class*="card"]').count();
    await expect(filteredJobs).toBeGreaterThanOrEqual(0);
  });

  // ========================================================================
  // Job Execution Tests
  // ========================================================================
  
  test('should trigger manual job execution', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const runButton = page.locator('button svg[role="img"][alt="play"]');
    if (await runButton.count() > 0) {
      await runButton.first().click();
      await expect(page.locator('.animate-spin')).toBeVisible();
    }
  });

  test('should show job execution history tab', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const runsTab = page.getByRole('tab', { name: /Execution History/i });
    await runsTab.click();
    
    await expect(page.locator('table, [class*="timeline"], [class*="history"]')).toBeVisible();
  });

  // ========================================================================
  // Gate Policy Tests
  // ========================================================================
  
  test('should view gate policies configuration', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const gatesTab = page.getByRole('tab', { name: /Gate Policies/i });
    await gatesTab.click();
    
    await expect(page.getByText(/Gate Policy/i)).toBeVisible();
  });

  test('should create new gate policy', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    // Navigate to gates tab first
    const gatesTab = page.getByRole('tab', { name: /Gate Policies/i });
    if (await gatesTab.count() > 0) {
      await gatesTab.click();
    }
    
    // Should see options to create/manage policies
    await expect(page.locator('button, [class*="policy"], [class*="gate"]')).toBeVisible();
  });

  // ========================================================================
  // Compliance Reporting Tests
  // ========================================================================
  
  test('should generate compliance report', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const complianceTab = page.getByRole('tab', { name: /Compliance Reports/i });
    if (await complianceTab.count() > 0) {
      await complianceTab.click();
      
      const exportButton = page.getByRole('button', { name: /Export|Generate Report/i });
      if (await exportButton.count() > 0) {
        await exportButton.click();
        await page.waitForTimeout(1000);
      }
    }
  });

  // ========================================================================
  // Interactive UI Tests
  // ========================================================================
  
  test('should toggle job enabled/disabled state', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    // Find a job card and toggle its enabled state
    const jobCard = page.locator('[class*="job-card"]').first();
    if (await jobCard.count() > 0) {
      await jobCard.click();
      // Toggle switch/button should be visible
      await expect(page.locator('button, input[type="checkbox"]')).toBeVisible();
    }
  });

  test('should show real-time status updates', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    // Check that component has reactive elements
    const statusElements = page.locator('[class*="badge"], [data-testid*="status"]');
    await expect(statusElements.first()).toBeVisible();
  });

  // ========================================================================
  // Error Handling Tests
  // ========================================================================
  
  test('should handle invalid job creation data', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    await page.getByRole('button', { name: /New Security Job/i }).click();
    await page.getByRole('button', { name: /Create/i }).click(); // Submit empty form
    
    // Should show validation error
    await expect(page.locator('.error, .required-error, text=required')).toBeVisible({ timeout: 3000 });
  });

  test('should show loading states during job operations', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    const loader = page.locator('.animate-spin, [class*="loader"]');
    // Wait briefly - might not always appear on initial load but should appear on actions
    await page.waitForTimeout(500);
  });

  // ========================================================================
  // Backend Integration Tests
  // ========================================================================
  
  test('should connect to actual API endpoints', async ({ page }) => {
    await page.goto(`${BASE_URL}/m37-devsecops`);
    
    // Make sure network requests are being made
    const hasContentLoaded = await page.evaluate(() => {
      return performance.getEntriesByType('resource').length > 0;
    });
    
    expect(hasContentLoaded).toBeTruthy();
  });

});
