/**
 * Playwright test fixtures.
 *
 * Re-exports the base Playwright test so specs share a single import point.
 */

import { test as base } from '@playwright/test'

export const test = base

export { expect } from '@playwright/test'
