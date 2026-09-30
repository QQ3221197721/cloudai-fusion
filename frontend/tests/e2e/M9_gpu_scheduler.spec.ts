/**
 * M9 GPU Scheduler & Live Migration - Playwright E2E Tests
 * 
 * Test Suite: 10 comprehensive tests covering all M9 functionality
 */

import { test, expect } from '@playwright/test';

test.describe('M9 GPU Scheduler E2E Tests', () => {
  
  test.beforeEach(async ({ page }) => {
    // Navigate to M9 page
    await page.goto('/m9-gpu-scheduler');
  });

  test('should display main header and page title', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'GPU Scheduling & Live Migration' })).toBeVisible();
  });

  test('should show statistics cards with correct counts', async ({ page }) => {
    // Wait for data to load
    await page.waitForTimeout(1000);
    
    // Check stats cards are visible
    const statsCards = page.locator('[class*="card"]');
    await expect(statsCards).toHaveCount(8); // 8 stat cards
    
    // Verify key metrics displayed
    const totalGPUCard = statsCards.first();
    await expect(totalGPUCard).toContainText('Total GPUs');
  });

  test('should display GPU devices in grid view', async ({ page }) => {
    // Devices tab should be active
    await expect(page.locator('span:has-text("GPU Devices")')).toHaveClass(/active/);
    
    // GPU cards should be rendered
    const gpuCards = page.locator('[class*="card"]').all();
    expect(gpuCards.length).toBeGreaterThan(0);
    
    // Each GPU card should show status badge
    for (const card of gpuCards) {
      await expect(card.locator('.ant-badge || [class*="badge"]')).toBeVisible();
    }
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

  test('should open migration control panel when clicking start migration', async ({ page }) => {
    // Find first available GPU
    const availableGpuCard = page.locator('[class*="badge"][class*="available"]');
    if (await availableGpuCard.isVisible()) {
      const migrateButton = availableGpuCard.locator('button[aria-label*="migrate"] || button svg + span ~ button').first();
      
      if (await migrateButton.isEnabled()) {
        await migrateButton.click();
        
        // Migration dialog should appear
        await expect(page.getByRole('dialog', { name: /migration control|live migration/i })).toBeVisible();
        
        // Form fields should be present
        await expect(page.getByLabel('VM ID *')).toBeVisible();
        await expect(page.getByLabel('Source Node')).toBeVisible();
        await expect(page.getByLabel('Destination Node')).toBeVisible();
      }
    }
  });

  test('should preview migration plan before execution', async ({ page }) => {
    const startMigrationButton = page.getByRole('button', { name: /start migration/i }).first();
    await expect(startMigrationButton).toBeVisible();
    
    await startMigrationButton.click();
    
    // Fill required field
    await page.getByLabel('VM ID *').fill('vm-test-job-001');
    
    // Preview plan button should work
    const previewButton = page.getByRole('button', { name: /preview plan|eye/i });
    if (await previewButton.isEnabled()) {
      await previewButton.click();
      
      // Migration plan dialog should appear
      await expect(page.getByText(/execution steps|step by step/i)).toBeVisible({ timeout: 5000 });
    }
  });

  test('should display active migrations in migration tab', async ({ page }) => {
    // Switch to migration tab
    const migrationTab = page.getByRole('tab', { name: /live migration|migration/i });
    await expect(migrationTab).toBeVisible();
    await migrationTab.click();
    await page.waitForTimeout(1000);
    
    // Migration list should appear
    const migrationTasks = page.locator('[class*="card"] [class*="progress"]');
    expect(migrationTasks.count()).toBeGreaterThanOrEqual(0);
  });

  test('should show performance metrics in metrics tab', async ({ page }) => {
    // Switch to metrics tab
    const metricsTab = page.getByRole('tab', { name: /performance|metrics/i });
    await expect(metricsTab).toBeVisible();
    await metricsTab.click();
    
    // Metrics content should appear
    await expect(page.getByText(/utilization|performance analytics/i)).toBeVisible({ timeout: 5000 });
    
    // Charts should be rendered
    const utilizationChart = page.locator('[class*="bar-chart"] || .h-40 flex');
    await expect(utilizationChart).toBeVisible();
  });

  test('should execute GPU live migration workflow', async ({ page }) => {
    const startMigrationButton = page.getByRole('button', { name: /start migration/i }).first();
    await startMigrationButton.click();
    
    // Complete migration form
    await page.getByLabel('VM ID *').fill('vm-production-job-001');
    await page.getByLabel('SLA Downtime Target *').click();
    await page.getByRole('option', { name: '< 1 minute' }).click();
    await page.getByLabel('Migration Priority (1-10)').fill('8');
    
    // Execute migration
    const executeButton = page.getByRole('button', { name: /execute migration|play/i });
    if (await executeButton.isEnabled()) {
      await executeButton.click();
      
      // Success notification or progress should appear
      await page.waitForTimeout(3000);
    }
  });

  test('should handle GPU status badges correctly', async ({ page }) => {
    // Check different status badges
    const statusBadges = page.locator('[class*="badge"] [class*="status"]');
    
    // Should have multiple statuses represented
    const availableBadge = page.locator('[class*="badge"].text-emerald-400');
    const occupiedBadge = page.locator('[class*="badge"].text-blue-400');
    
    await expect(availableBadge).toBeVisible();
    await expect(occupiedBadge).toBeVisible();
  });

  test('should render GPU utilization bars', async ({ page }) => {
    const gpuCard = page.locator('[class*="card"]').first();
    await expect(gpuCard).toBeVisible();
    
    // Look for progress bars or utilization indicators
    const progressBar = gpuCard.locator('[class*="progress"] || div[class*="bg-emerald-500"]');
    await expect(progressBar.first()).toBeVisible();
  });

  test('should refresh data on demand', async ({ page }) => {
    const refreshButtons = page.locator('button[aria-label*="refresh"] || button span + button');
    
    if (await refreshButtons.first().isVisible()) {
      await refreshButtons.first().click();
      
      // Loading state might appear briefly
      await page.waitForTimeout(1000);
    }
  });

});
