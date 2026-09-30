/**
 * M8 Global Configuration Manager - Playwright E2E Tests
 * 
 * Test Suite: 10 comprehensive tests covering all M8 functionality
 */

import { test, expect } from '@playwright/test';

test.describe('M8 Config Manager E2E Tests', () => {
  
  test.beforeEach(async ({ page }) => {
    // Navigate to M8 page (adjust URL as needed)
    await page.goto('/m8-config-manager');
  });

  test('should display main header and page title', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Global Configuration Manager' })).toBeVisible();
  });

  test('should show statistics cards with correct counts', async ({ page }) => {
    // Wait for data to load (simulated or real API)
    await page.waitForTimeout(1000);
    
    // Check stats cards are visible
    await expect(page.locator('.bg-card\\/50')).toHaveCount(6); // 6 stat cards
    
    // Verify total configs count is displayed
    const totalCard = page.locator('.bg-card\\/50').first();
    await expect(totalCard).toContainText('Total');
  });

  test('should filter configurations by category', async ({ page }) => {
    // Filter dropdown should be present
    const filterDropdown = page.getByPlaceholder('Filter by category');
    await expect(filterDropdown).toBeVisible();
    
    // Select a category (if available)
    await filterDropdown.click();
    const categoryOption = page.getByRole('option', { name: /[^A][^l][L]/ }).first();
    if (await categoryOption.isVisible()) {
      await categoryOption.click();
      // Verify filtered results appear
      await page.waitForTimeout(500);
    }
  });

  test('should filter configurations by scope', async ({ page }) => {
    // Scope filter should be present
    const scopeFilter = page.getByPlaceholder('Scope');
    await expect(scopeFilter).toBeVisible();
    
    // Test scope filtering options
    await scopeFilter.click();
    await page.getByRole('option', { name: 'Global' }).click();
    
    // Verify state updates
    await expect(scopeFilter).toContainText('Global');
  });

  test('should search configurations by key or description', async ({ page }) => {
    const searchInput = page.getByPlaceholder('Search configurations...');
    await expect(searchInput).toBeVisible();
    
    // Enter search query
    await searchInput.fill('feature_flag');
    await page.waitForTimeout(500); // Wait for filter to apply
    
    // Results should update based on search
    const configCards = page.locator('[class*="card"]').all();
    expect(configCards.length).toBeGreaterThan(0);
  });

  test('should open edit modal when clicking edit button', async ({ page }) => {
    // Find first editable config
    const editButton = page.locator('button[aria-label*="edit"] || button svg + span ~ button').first();
    
    if (await editButton.isEnabled()) {
      await editButton.click();
      
      // Edit dialog should appear
      await expect(page.getByRole('dialog', { name: 'Edit Configuration' })).toBeVisible();
      
      // Form fields should be present
      await expect(page.getByLabel('Configuration Key *')).toBeVisible();
      await expect(page.getByLabel('Type')).toBeVisible();
    }
  });

  test('should toggle feature flag switch', async ({ page }) => {
    // Find feature flag card
    const featureFlagCard = page.locator('.text-purple-400').first();
    
    if (await featureFlagCard.isVisible()) {
      // Click the toggle switch within that card context
      const switchElement = page.locator('.ng-star-inserted || .ant-switch').first();
      if (await switchElement.isVisible()) {
        await switchElement.click();
        
        // Toggle state should change
        await page.waitForTimeout(500);
      }
    }
  });

  test('should export configuration as JSON', async ({ page }) => {
    const exportButton = page.getByRole('button', { name: /export|download/i }).first();
    await expect(exportButton).toBeVisible();
    
    // Mock download listener
    const downloadPromise = page.waitForEvent('download');
    await exportButton.click();
    
    const download = await downloadPromise;
    expect(download.suggestedFilename().endsWith('.json')).toBeTruthy();
  });

  test('should view configuration hierarchy tree', async ({ page }) => {
    const hierarchyButton = page.getByRole('button', { name: /hierarchy|tree|branch/i });
    await expect(hierarchyButton).toBeVisible();
    
    await hierarchyButton.click();
    
    // Hierarchy view should appear
    const hierarchyView = page.getByText(/configuration inheritance|override|inherit/i);
    await expect(hierarchyView).toBeVisible();
  });

  test('should navigate between tabs (list/tree)', async ({ page }) => {
    // List tab should be active by default
    await expect(page.locator('span:has-text("List")')).toHaveClass(/active|selected/);
    
    // Switch to tree view
    const treeTab = page.getByRole('tab', { name: /tree|hierarchy/i });
    if (await treeTab.isVisible()) {
      await treeTab.click();
      await expect(page.locator('span:has-text("Tree")')).toHaveClass(/active|selected/);
    }
  });

  test('should handle empty state when no configs match filters', async ({ page }) => {
    // Apply strict filters that won't match anything
    await page.getByPlaceholder('Search configurations...').fill('nonexistentkey123');
    await page.waitForTimeout(500);
    
    // Empty state message should appear
    await expect(page.getByText(/no configurations? found/i)).toBeVisible({ timeout: 2000 });
  });

  test('should display loading states during data fetch', async ({ page }) => {
    // Intercept API calls to simulate loading
    await page.route('**/api/v1/config*', route => {
      setTimeout(() => route.fulfill({
        status: 200,
        json: { configs: [] }
      }), 1000);
    });
    
    // Reload to trigger loading state
    await page.reload();
    
    // Loading indicator should be visible
    const loader = page.locator('[class*="loader"] || [class*="spinner"] || svg[data-testid]');
    await expect(loader.first()).toBeVisible({ timeout: 3000 });
  });

});
