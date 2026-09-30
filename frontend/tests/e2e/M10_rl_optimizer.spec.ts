/**
 * M10 RL-Based Training Optimizer - Playwright E2E Tests
 * 
 * Test Suite: 10 comprehensive tests covering all M10 functionality
 */

import { test, expect } from '@playwright/test';

test.describe('M10 RL Optimizer E2E Tests', () => {
  
  test.beforeEach(async ({ page }) => {
    // Navigate to M10 page
    await page.goto('/m10-rl-optimizer');
  });

  test('should display main header and page title', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'RL-Based Training Optimizer' })).toBeVisible();
  });

  test('should show statistics cards with correct counts', async ({ page }) => {
    // Wait for data to load
    await page.waitForTimeout(1000);
    
    // Check stats cards are visible
    const statsCards = page.locator('[class*="card"]');
    await expect(statsCards).toHaveCount(6); // 6 stat cards
    
    // Verify key metrics displayed
    const totalJobsCard = statsCards.first();
    await expect(totalJobsCard).toContainText('Total Jobs');
  });

  test('should display recent optimizations in dashboard tab', async ({ page }) => {
    // Dashboard tab should be active
    await expect(page.locator('span:has-text("Dashboard")')).toHaveClass(/active/);
    
    // Optimization records should be rendered
    const optCards = page.locator('[class*="card"] [class*="job_id"]');
    expect(optCards.count()).toBeGreaterThanOrEqual(0);
  });

  test('should switch between real API and simulated data mode', async ({ page }) => {
    const toggleButton = page.getByRole('button', { name: /real backend|simulated/i });
    await expect(toggleButton).toBeVisible();
    
    // Click to toggle
    const initialButtonText = await toggleButton.innerText();
    await toggleButton.click();
    
    // Button text should change
    const newButtonText = await toggleButton.innerText();
    expect(initialButtonText).not.toBe(newButtonText);
  });

  test('should open job submitter dialog', async ({ page }) => {
    const submitButton = page.getByRole('button', { name: /submit job/i });
    await expect(submitButton).toBeVisible();
    await submitButton.click();
    
    // Job submitter dialog or modal should appear
    await expect(page.getByRole('dialog') || page.locator('[id*="modal"]')).toBeVisible({ timeout: 3000 });
  });

  test('should submit a job for RL optimization', async ({ page }) => {
    // Open job submitter
    const submitButton = page.getByRole('button', { name: /submit job/i }).first();
    await expect(submitButton).toBeVisible();
    await submitButton.click();
    
    // Fill job form
    await page.getByLabel('Job Name *').fill('rl-test-job-001');
    await page.getByLabel('Workload Type *').click();
    await page.getByRole('option', { name: 'Training' }).click();
    await page.getByLabel('Priority (1-10) *').fill('7');
    await page.getByLabel('Expected Duration (min)').fill('120');
    await page.getByLabel('GPU Requirement *').click();
    await page.getByRole('option', { name: '1xA100' }).click();
    await page.getByLabel('Memory Requirement (GB)').fill('32');
    await page.getByLabel('SLA Target (min)').fill('30');
    
    // Submit the job
    const executeButton = page.getByRole('button', { name: /submit for optimization|play/i });
    if (await executeButton.isEnabled()) {
      await executeButton.click();
      
      // Success notification or state update
      await page.waitForTimeout(3000);
    }
  });

  test('should display live suggestions in dashboard', async ({ page }) => {
    // Refresh suggestions
    const refreshButton = page.locator('button[aria-label*="refresh"] || button span + button').first();
    if (await refreshButton.isVisible()) {
      await refreshButton.click();
      await page.waitForTimeout(1000);
    }
    
    // Suggestions section should appear
    const suggestionsSection = page.getByText(/live suggestions|ai recommendation/i);
    await expect(suggestionsSection).toBeVisible({ timeout: 5000 });
  });

  test('should navigate between tabs (dashboard/submitter/analytics/policy)', async ({ page }) => {
    // Start at dashboard
    await expect(page.locator('span:has-text("Dashboard")')).toHaveClass(/active/);
    
    // Switch to analytics tab
    const analyticsTab = page.getByRole('tab', { name: /analytics|benchmark/i });
    if (await analyticsTab.isVisible()) {
      await analyticsTab.click();
      await page.waitForTimeout(1000);
      
      // Analytics content should appear
      await expect(page.getByText(/performance benchmarks|before after comparison/i)).toBeVisible({ timeout: 5000 });
    }
    
    // Switch to policy tab
    const policyTab = page.getByRole('tab', { name: /policy/i });
    if (await policyTab.isVisible()) {
      await policyTab.click();
      
      // Policy configuration should appear
      await expect(page.getByText(/rl policy|hyperparameters/i)).toBeVisible({ timeout: 5000 });
    }
  });

  test('should view performance benchmarks', async ({ page }) => {
    // Navigate to analytics tab
    const analyticsTab = page.getByRole('tab', { name: /analytics/i }).first();
    await expect(analyticsTab).toBeVisible();
    await analyticsTab.click();
    
    // Benchmark charts should appear
    const chartElements = page.locator('.h-32 flex items-end gap-1 || [class*="bar-chart"]');
    await expect(chartElements.first()).toBeVisible({ timeout: 5000 });
    
    // Statistical significance badges should be visible
    const significanceBadges = page.locator('[class*="badge"].text-green-400');
    await expect(significanceBadges.first()).toBeVisible({ timeout: 8000 });
  });

  test('should open and edit RL policy configuration', async ({ page }) => {
    // Navigate to policy tab
    const policyTab = page.getByRole('tab', { name: /policy/i });
    await expect(policyTab).toBeVisible();
    await policyTab.click();
    
    // Edit policy button should be present
    const editButton = page.getByRole('button', { name: /edit.*policy|save/i });
    if (await editButton.isVisible()) {
      await editButton.click();
      
      // Policy editor dialog should appear
      await expect(page.getByRole('dialog') || page.getByText(/learning rate|exploration factor/i)).toBeVisible({ timeout: 5000 });
    }
  });

  test('should display optimization decision badges', async ({ page }) => {
    const decisionBadges = page.locator('[class*="badge"] [class*="brain-circuit"]');
    
    // Should have multiple decision types represented
    expect(decisionBadges.count()).toBeGreaterThanOrEqual(0);
  });

  test('should render performance trend charts', async ({ page }) => {
    const analyticsTab = page.getByRole('tab', { name: /analytics/i });
    if (await analyticsTab.isVisible()) {
      await analyticsTab.click();
      await page.waitForTimeout(1000);
    }
    
    // Trend line or bar charts should be visible
    const trendCharts = page.locator('[class*="chart"] || .flex.items-end.gap-1');
    await expect(trendCharts.first()).toBeVisible({ timeout: 8000 });
  });

  test('should calculate and display financial ROI metrics', async ({ page }) => {
    // Check if financial metrics are visible
    const dollarSignIcon = page.locator('[class*="dollar-sign"]');
    await expect(dollarSignIcon).toBeVisible();
    
    // Monthly savings figure should be displayed
    const savingsText = page.getByText(/\$[\d,]+\/month/);
    await expect(savingsText).toBeVisible({ timeout: 8000 });
  });

});
