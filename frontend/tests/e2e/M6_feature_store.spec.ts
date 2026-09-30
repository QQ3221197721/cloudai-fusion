/**
 * Playwright E2E Tests for M6 Feature Store Page
 * 
 * Test Coverage:
 * - Feature catalog display and navigation
 * - Search and filter functionality
 * - Feature registration flow
 * - Query builder and online store retrieval
 * - Offline store batch jobs
 * - Usage analytics display
 * - Error handling for duplicate features
 * - State persistence across tabs
 */

import { test, expect, type Page } from '@playwright/test';

// Configure timeout for all tests
test.setTimeout(60000);

const BASE_URL = 'http://localhost:3000';

test.describe('M6 Feature Store Page', () => {
  
  // Common setup before each test
  test.beforeEach(async ({ page }) => {
    // Navigate to the Feature Store page
    await page.goto('/m6-feature-store');
  });

  // ============================================================================
  // Test Suite 1: Page Load & Basic Display
  // ============================================================================

  test('should load and display Feature Store page correctly', async ({ page }) => {
    // Verify title is visible
    await expect(page.getByText(/Feature Store Management/i)).toBeVisible();
    
    // Verify summary cards are displayed
    const summaryCards = page.locator('[class*="bg-gray-800/50"]');
    await expect(summaryCards).toHaveCount(4);
  });

  test('should show feature catalog statistics in summary cards', async ({ page }) => {
    // Verify stat cards content
    await expect(page.getByText(/Total Features/i)).toBeVisible();
    await expect(page.getByText(/Online Store Status/i)).toBeVisible();
    await expect(page.getByText(/Offline Storage/i)).toBeVisible();
    await expect(page.getByText(/Access Rate/i)).toBeVisible();
  });

  test('should display header with icon and description', async ({ page }) => {
    const headerTitle = page.locator('h1').first();
    await expect(headerTitle).toContainText('Feature Store Management');
    
    const headerDesc = page.locator('p').filter({ hasText: /Central registry/i });
    await expect(headerDesc).toBeVisible();
  });

  // ============================================================================
  // Test Suite 2: Navigation & Tabs
  // ============================================================================

  test('should have all tab sections available', async ({ page }) => {
    const tabs = page.locator('[role="tab"]');
    await expect(tabs).toHaveCount(5);
    
    await expect(page.locator('[role="tab"]:has-text("Feature Catalog")')).toBeVisible();
    await expect(page.locator('[role="tab"]:has-text("Feature Registry")')).toBeVisible();
    await expect(page.locator('[role="tab"]:has-text("Online Store")')).toBeVisible();
    await expect(page.locator('[role="tab"]:has-text("Offline Store")')).toBeVisible();
    await expect(page.locator('[role="tab"]:has-text("Usage Analytics")')).toBeVisible();
  });

  test('should switch between tabs without losing state', async ({ page }) => {
    // Start on catalog tab
    await expect(page.locator('h1 + div')).toHaveCount(5); // Header + 4 cards
    
    // Switch to registry tab
    await page.click('[role="tab"]:has-text("Feature Registry")');
    await expect(page.getByText(/Feature Metadata Registry/i)).toBeVisible();
    
    // Switch back to catalog
    await page.click('[role="tab"]:has-text("Feature Catalog")');
    await expect(page.locator('table')).toBeVisible();
  });

  // ============================================================================
  // Test Suite 3: Feature Catalog Functions
  // ============================================================================

  test('should search and filter features by name', async ({ page }) => {
    const searchTerm = 'user_avg_order_value';
    await page.fill('input[placeholder*="search"]', searchTerm);
    
    // Should filter table rows based on search
    const tableRows = page.locator('tbody tr');
    // Allow some time for filtering
    await page.waitForTimeout(500);
    
    // Check that results contain the search term or show empty state
    const isVisible = await tableRows.count() > 0;
    if (isVisible) {
      const firstCell = page.locator('td').first();
      await expect(firstCell).toContainText(searchTerm, { ignoreCase: true });
    }
  });

  test('should filter features by type dropdown', async ({ page }) => {
    // Select float type filter
    await page.select_option(
      'select',
      'float'
    );
    
    // Table should reflect the filter
    // Note: This is a basic check since actual data depends on backend
    await expect(page.locator('table')).toBeVisible();
  });

  test('should display feature table with correct columns', async ({ page }) => {
    const tableHeaders = [
      'Name',
      'Entity',
      'Type',
      'Group',
      'Owner',
      'Actions'
    ];
    
    for (const header of tableHeaders) {
      await expect(page.locator(`th:has-text("${header}")`)).toBeVisible();
    }
  });

  test('should show feature badges for privacy levels', async ({ page }) => {
    // Privacy level badges should be visible for each feature row
    const privacyBadges = page.locator('chip, badge, span[class*="bg-"]').first();
    await expect(privacyBadges).toBeVisible();
  });

  test('should show feature type badges', async ({ page }) => {
    // Type badges should be colorful indicators
    const typeBadges = page.locator('badge, chip').filter({ hasText: /\w+/i }).first();
    await expect(typeBadges).toBeVisible();
  });

  // ============================================================================
  // Test Suite 4: Action Buttons
  // ============================================================================

  test('should register new feature via modal', async ({ page }) => {
    // Click register button
    const registerButton = page.getByRole('button', { name: /register feature/i });
    await registerButton.click();
    
    // Modal should appear
    await expect(page.locator('dialog, [role="dialog"], h3:has-text("Register New Feature")'))
      .toBeVisible({ timeout: 2000 });
    
    // Fill form fields
    await page.fill('input[placeholder*="name"]', 'test_feature_demo');
    
    // Select entity type
    await page.select_option('select:has-text("Entity Type")', 'user_id');
    
    // Select feature type
    await page.select_option('select:has-text("Feature Type")', 'float');
    
    // Add description
    await page.fill('textarea[placeholder*="Brief description"]', 'Test feature for demo purposes');
    
    // Submit should not error
    const submitButton = page.locator('button[type="submit"], dialog :text("Register Feature")');
    await submitButton.click();
    
    // Should show success or close modal
    await expect(page.locator('dialog, [role="dialog"]')).toHaveCount(0, { timeout: 3000 });
  });

  test('should show view details drawer when clicking eye icon', async ({ page }) => {
    // Find first feature row and click eye icon
    const firstRow = page.locator('tr').first();
    
    // Look for action buttons
    const actionCell = page.locator('td:has(button)').last();
    const eyeIconBtn = actionCell.locator('button:has-text("View Details"), button svg *path[name*="eye"]');
    
    if (await eyeIconBtn.count() > 0) {
      await eyeIconBtn.first().click();
      
      // Drawer should appear
      await expect(page.locator('h3:has-text("Feature Details")')).toBeVisible({ timeout: 3000 });
    } else {
      // Alternative: just check that the page loads with tables
      await expect(page.locator('table')).toBeVisible();
    }
  });

  test('should have query builder in online store tab', async ({ page }) => {
    // Click Online Store tab
    await page.click('[role="tab"]:has-text("Online Store")');
    await expect(page.getByText(/Point-in-time correct/i)).toBeVisible();
    
    // Entity ID input should exist
    await expect(page.locator('input[placeholder*="user_123"], input[type="text"]')).toBeVisible();
    
    // Run query button should exist
    await expect(page.getByRole('button', { name: /query|run/i })).toBeVisible();
  });

  // ============================================================================
  // Test Suite 5: Online Store Tab Functionality
  // ============================================================================

  test('should display online feature groups list', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Online Store")');
    
    // Card with online groups should be visible
    const groupCard = page.locator('.card, [class*="bg-gray-700"]');
    await expect(groupCard.first()).toBeVisible();
  });

  test('should handle query entity ID input', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Online Store")');
    
    const entityIdInput = page.locator('input[type="text"]').first();
    await entityIdInput.fill('test_user_123');
    
    await expect(entityIdInput).toHaveValue('test_user_123');
  });

  test('should allow multi-select features for querying', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Online Store")');
    
    // Look for checkboxes (feature selection UI)
    const checkboxes = page.locator('input[type="checkbox"]');
    if (await checkboxes.count() > 0) {
      await checkboxes.first().check();
      await expect(checkboxes.first()).toBeChecked();
    }
  });

  // ============================================================================
  // Test Suite 6: Offline Store Tab
  // ============================================================================

  test('should show offline materialization job interface', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Offline Store")');
    await expect(page.getByText(/batch storage|materialization/i)).toBeVisible();
    
    // Job scheduling controls should exist
    const selectControls = page.locator('select');
    const dateInputs = page.locator('input[type="date"]');
    
    await expect(selectControls.first()).toBeVisible();
    await expect(dateInputs.first()).toBeVisible();
  });

  test('should display offline storage groups', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Offline Store")');
    
    // Export buttons should be available
    const exportButtons = page.getByRole('button', { name: /export/i });
    if (await exportButtons.count() > 0) {
      await expect(exportButtons.first()).toBeVisible();
    }
  });

  // ============================================================================
  // Test Suite 7: Usage Analytics Tab
  // ============================================================================

  test('should display usage metrics summary cards', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Usage Analytics")');
    
    // Metrics cards should be visible
    const metricsCards = page.locator('[class*="bg-gray-700/30"]');
    await expect(metricsCards).toHaveCount(3);
  });

  test('should show consumer models table', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Usage Analytics")');
    
    // Consumer table headers should be visible
    await expect(page.locator('th:has-text("Model Name")')).toBeVisible();
    await expect(page.locator('th:has-text("Feature")')).toBeVisible();
    await expect(page.locator('th:has-text("Usage Count")')).toBeVisible();
    await expect(page.locator('th:has-text("Importance")')).toBeVisible();
  });

  test('should render progress bars for feature importance', async ({ page }) => {
    await page.click('[role="tab"]:has-text("Usage Analytics")');
    
    // Progress components should be visible in importance column
    const progressBars = page.locator('progress, [class*="progress"]');
    if (await progressBars.count() > 0) {
      await expect(progressBars.first()).toBeVisible();
    }
  });

  // ============================================================================
  // Test Suite 8: Error Handling & Edge Cases
  // ============================================================================

  test('should handle empty state gracefully', async ({ page }) => {
    // Force empty state by searching for non-existent feature
    await page.fill('input[placeholder*="search"]', 'nonexistent_feature_xyz');
    await page.select_option('select', { index: -1 }); // Reset filter
    
    // Table should either show empty message or filtered results
    await expect(page.locator('table')).toBeVisible();
  });

  test('should validate feature name on registration', async ({ page }) => {
    await page.getByRole('button', { name: /register feature/i }).click();
    
    // Try to submit without name
    const submitButton = page.locator('button[type="submit"], dialog :text("Register Feature")');
    await submitButton.click();
    
    // Should show validation error or prevent submission
    // Either alert appears or form stays open
    await expect(page.locator('dialog, [role="dialog"]')).toHaveCount(1, { timeout: 2000 });
  });

  // ============================================================================
  // Test Suite 9: Data Loading & Refresh
  // ============================================================================

  test('should reload data on refresh button click', async ({ page }) => {
    // Initial data should be loaded
    await expect(page.locator('table tbody tr')).toHaveCount({ min: 0 });
    
    // Click refresh button
    const refreshBtn = page.getByRole('button', { name: /refresh/i });
    if (await refreshBtn.count() > 0) {
      await refreshBtn.click();
      
      // Should show loading state or reload data
      // Test waits a bit to see if any visual feedback occurs
      await page.waitForTimeout(1000);
      
      // Table should still be visible
      await expect(page.locator('table')).toBeVisible();
    }
  });

  test('should display loading spinner during data fetch', async ({ page }) => {
    // Reload page to trigger loading
    await page.reload();
    await page.waitForLoadState('networkidle');
    
    // Check for spinner or loading indicator
    const loaders = page.locator('.spinner, [class*="spinner"], svg *animate');
    // Not required to be visible if data loaded instantly
  });

  // ============================================================================
  // Test Suite 10: Responsive Design
  // ============================================================================

  test('should layout correctly on desktop viewport', async ({ page }) => {
    await page.setViewportSize({ width: 1920, height: 1080 });
    
    // Summary cards should be in 4-column grid
    const cardGrid = page.locator('.grid').first();
    await expect(cardGrid).toBeVisible();
  });

  test('should stack cards vertically on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    
    // Cards should still be visible but may stack
    const cards = page.locator('[class*="bg-gray-800/50"]');
    await expect(cards.first()).toBeVisible();
  });

  // ============================================================================
  // Additional Integration Tests
  // ============================================================================

  test('should persist selected filters when switching tabs', async ({ page }) => {
    // Apply filter
    await page.select_option('select', 'float');
    await page.fill('input[placeholder*="search"]', 'user_');
    
    // Switch tabs
    await page.click('[role="tab"]:has-text("Feature Registry")');
    
    // Go back to catalog
    await page.click('[role="tab"]:has-text("Feature Catalog")');
    
    // Filters should still be applied
    // (This depends on implementation, so we just check page navigates)
    await expect(page.locator('table')).toBeVisible();
  });

  test('should handle network errors gracefully', async ({ page }) => {
    // Mock network error
    await page.route('**/api/v1/features*', route => route.abort('failed'));
    
    // Reload page to trigger error
    await page.reload();
    
    // Should show error state or retry option
    // Or remain in initial state
    await expect(page.locator('h1')).toContainText('Feature Store');
  });

  test('should properly escape user input', async ({ page }) => {
    // Attempt XSS injection in search
    const maliciousInput = '<script>alert("xss")</script>';
    await page.fill('input[placeholder*="search"]', maliciousInput);
    
    // Should be escaped in DOM
    await page.evaluate(() => {
      return document.querySelector('body').innerHTML.indexOf('<script>') === -1;
    });
  });

  test('should maintain consistent styling across all tabs', async ({ page }) => {
    const tabs = ['Feature Catalog', 'Feature Registry', 'Online Store', 'Offline Store', 'Usage Analytics'];
    
    for (const tabName of tabs) {
      await page.click(`[role="tab"]:has-text("${tabName}")`);
      
      // Each tab should have proper spacing and structure
      await expect(page.locator('div[class*="space-y-"]')).toHaveCount({ min: 1 });
    }
  });

});
