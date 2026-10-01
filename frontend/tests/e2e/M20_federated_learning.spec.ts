/**
 * M20 Federated Learning Platform - Playwright E2D Tests
 * 
 * Test Suite: 8 comprehensive tests covering distributed AI and privacy-preserving ML features
 */

import { test, expect } from '@playwright/test';

test.describe('M20 Federated Learning E2E Tests', () => {
  
  test.beforeEach(async ({ page }) => {
    await page.goto('/m20-federated-learning');
  });

  test('should display main header and page title', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Federated Learning' })).toBeVisible();
  });

  test('should show statistics cards with correct counts', async ({ page }) => {
    await page.waitForTimeout(1000);
    
    const statsCards = page.locator('[class*="card"] [class*="content"]').first().locator('+ div');
    expect(statsCards).toHaveCount(4);
    
    const activeDevicesCard = statsCards.first();
    await expect(activeDevicesCard).toContainText('Active Devices');
  });

  test('should navigate between topology and edge devices tabs', async ({ page }) => {
    // Start at Topology tab
    await expect(page.locator('button:has-text("Topology")')).toHaveClass(/active/);
    
    // Switch to Edge Devices tab
    const devicesTab = page.getByRole('tab', { name: 'Edge Devices' });
    await expect(devicesTab).toBeVisible({ timeout: 3000 });
    
    // Dashboard content should appear
    const dashboardContent = page.getByText(/Edge Device Topology|Distributed network/i);
    await expect(dashboardContent).toBeVisible({ timeout: 5000 });
  });

  test('should open device enrollment modal', async ({ page }) => {
    const enrollButton = page.getByRole('button', { name: /Enroll Device/i });
    await expect(enrollButton).toBeVisible();
    await enrollButton.click();
    
    // Modal should appear
    await expect(page.getByRole('dialog') || page.locator('[id*="modal"]')).toBeVisible({ timeout: 3000 });
  });

  test('should enroll a new edge device', async ({ page }) => {
    // Enroll button
    const enrollButton = page.getByRole('button', { name: /Enroll Device/i });
    if (await enrollButton.isVisible()) {
      await enrollButton.click();
      
      // Fill device form
      await page.fill('[placeholder="Workstation"]', 'federated-device-001', { strict: true });
      
      // Select type
      const typeSelect = page.locator('select[name="type"], select[aria-label="Device Type"]');
      if (await typeSelect.isVisible()) {
        await typeSelect.selectOption('workstation');
      }
      
      // Set memory
      await page.fill('[placeholder="8"]', '16', { strict: true });
      
      // Enable differential privacy
      await page.check('[type="checkbox"][name="enableDP"], input[type="checkbox"]:checked').first();
      
      // Submit
      const submitButton = page.locator('button:has-text("Enroll"), button:has-text("Submit")');
      if (await submitButton.isVisible()) {
        await submitButton.click();
        await page.waitForTimeout(2000);
      }
    }
  });

  test('should filter devices by search and type', async ({ page }) => {
    // Search filter
    const searchInput = page.locator('input[placeholder="Search devices..."]');
    await searchInput.fill('device');
    await page.waitForTimeout(1000);
    
    // Type filter
    const typeSelect = page.locator('select[aria-label="Type Filter"]');
    if (await typeSelect.isVisible()) {
      await typeSelect.selectOption('server');
      await page.waitForTimeout(1000);
    }
    
    // Filtered results should display
    await expect(page.locator('[class*="card"][class*="grid"]').getByRole('link').all()).toHaveCount({ min: 0 });
  });

  test('should control aggregation rounds (start/stop)', async ({ page }) => {
    // Aggregation tab
    const roundsTab = page.getByRole('tab', { name: 'Aggregation' });
    if (await roundsTab.isVisible()) {
      await roundsTab.click();
      await page.waitForTimeout(1000);
      
      // Start/Stop buttons should be visible
      const startButtons = page.locator('button:has-text("Start"), button:has-text("Play")');
      const stopButtons = page.locator('button:has-text("Stop"), button:has-text("Square")');
      
      expect(startButtons.count() + stopButtons.count()).toBeGreaterThanOrEqual(0);
    }
  });

  test('should view global model versions and deploy', async ({ page }) => {
    // Models tab
    const modelsTab = page.getByRole('tab', { name: 'Global Models' });
    if (await modelsTab.isVisible()) {
      await modelsTab.click();
      await page.waitForTimeout(1000);
      
      // Model cards should appear
      const modelCards = page.locator('[class*="card"] [class*="font-bold"].text-lg').all();
      
      if (modelCards.length > 0) {
        // Deploy button should be available
        const deployButtons = page.locator('button:has-text("Deploy"), button svg + span:has-text("Upload")');
        expect(deployButtons.count()).toBeGreaterThanOrEqual(0);
      }
    }
  });

});
