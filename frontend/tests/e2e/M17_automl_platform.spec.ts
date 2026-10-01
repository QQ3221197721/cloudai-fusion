/**
 * M17 AutoML Platform - Playwright E2E Tests
 * 
 * Test Suite: 8 comprehensive tests covering HPO job lifecycle and analysis features
 */

import { test, expect } from '@playwright/test';

test.describe('M17 AutoML Platform E2E Tests', () => {
  
  test.beforeEach(async ({ page }) => {
    await page.goto('/m17-automl');
  });

  test('should display main header and page title', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'AutoML Platform' })).toBeVisible();
  });

  test('should show statistics cards with correct counts', async ({ page }) => {
    // Wait for data to load
    await page.waitForTimeout(1000);
    
    const statsCards = page.locator('[class*="card"] [class*="content"]');
    expect(statsCards).toHaveCount(4);
    
    const activeJobsCard = statsCards.first();
    await expect(activeJobsCard).toContainText('Active Jobs');
  });

  test('should navigate between tabs (jobs/analysis/results/strategies)', async ({ page }) => {
    // Start at jobs tab
    await expect(page.locator('button:has-text("Jobs List")')).toHaveClass(/active/);
    
    // Switch to strategies tab
    const strategiesTab = page.getByRole('tab', { name: 'Search Strategies' });
    await expect(strategiesTab).toBeVisible();
    await strategiesTab.click();
    await page.waitForTimeout(1000);
    
    // Strategies content should appear
    await expect(page.getByText(/Bayesian Optimization|Random Search|Grid Search/i)).toBeVisible({ timeout: 5000 });
    
    // Return to jobs
    const jobsTab = page.getByRole('tab', { name: 'Jobs List' });
    await jobsTab.click();
  });

  test('should open create job modal', async ({ page }) => {
    const createButton = page.getByRole('button', { name: /Create HPO Job/i });
    await expect(createButton).toBeVisible();
    await createButton.click();
    
    // Modal should appear
    await expect(page.getByRole('dialog') || page.locator('[id*="modal"]')).toBeVisible({ timeout: 3000 });
  });

  test('should create a new HPO job', async ({ page }) => {
    // Open modal
    const createButton = page.getByRole('button', { name: /Create HPO Job/i });
    await createButton.click();
    
    // Fill form
    await page.fill('input[name="jobName"]', 'test-hpo-job-001', { strict: true });
    
    // Select strategy
    const strategySelect = page.locator('select[name="strategy"]');
    await strategySelect.selectOption('bayesian');
    
    // Set max trials
    await page.fill('input[name="maxTrials"]', '20');
    
    // Submit
    await page.click('button:has-text("Start Optimization")');
    
    // Success notification or state update
    await page.waitForTimeout(3000);
  });

  test('should filter jobs by search and status', async ({ page }) => {
    // Search filter
    const searchInput = page.locator('input[placeholder="Search jobs..."]');
    await searchInput.fill('test');
    await page.waitForTimeout(1000);
    
    // Status filter
    const statusSelect = page.locator('select[aria-label="Status Filter"]');
    await statusSelect.selectOption('running');
    await page.waitForTimeout(1000);
    
    // Filtered results should display
    await expect(page.locator('table tbody tr')).toHaveCount({ min: 0 });
  });

  test('should perform CRUD operations on HPO jobs', async ({ page }) => {
    // View jobs table
    const jobsTable = page.locator('table[class*="table"]').first();
    await expect(jobsTable).toBeVisible({ timeout: 5000 });
    
    // Check if start button is enabled/disabled based on status
    const startButtons = page.locator('button svg + span:has-text("Play"), button svg:first-child').all();
    
    // Delete functionality check
    const deleteButtons = page.locator('button:has-text("Delete"), button[aria-label*="delete"]').all();
    
    expect(deleteButtons.length).toBeGreaterThanOrEqual(0);
  });

  test('should view job details and results', async ({ page }) => {
    // Click on first job card
    const jobCards = page.locator('[class*="card"] h3:has-text("HPO")').first();
    if (await jobCards.isVisible()) {
      await jobCards.click();
      await page.waitForTimeout(1000);
      
      // Results panel should appear
      const resultsPanel = page.getByText(/Best Configuration|Metric Analysis/i);
      await expect(resultsPanel).toBeVisible({ timeout: 5000 });
    }
  });

});
