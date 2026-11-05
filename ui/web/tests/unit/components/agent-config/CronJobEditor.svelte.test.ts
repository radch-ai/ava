import { afterEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import CronJobEditor from '#components/agent-config/CronJobEditor.svelte'
import {
	createCronJob,
	deleteCronJob,
	getCronJobs,
	updateCronJob
} from '#lib/services/agent'
import { invalidateCronJobs } from '#lib/services/agent/cron-jobs.svelte'
import type { CronJob } from '#lib/types/agent'

vi.mock('#lib/services/agent/config', () => ({
	getCronJobs: vi.fn(),
	createCronJob: vi.fn(),
	updateCronJob: vi.fn(),
	deleteCronJob: vi.fn()
}))

const job: CronJob = {
	id: '1',
	time: '07:00',
	frequency: 'daily',
	model: 'auto',
	prompt: 'Daily summary'
}

function mockGet(jobs: CronJob[]) {
	vi.mocked(getCronJobs).mockResolvedValue(jobs)
}

describe('CronJobEditor', () => {
	afterEach(() => {
		cleanup()
		invalidateCronJobs()
		vi.clearAllMocks()
	})

	it('shows the create-first placeholder without an agent', async () => {
		render(CronJobEditor, {})

		await expect
			.element(
				page.getByText('Create Agent first and then edit from Agent Settings.')
			)
			.toBeVisible()
		expect(getCronJobs).not.toHaveBeenCalled()
	})

	it('fetches the list on open and renders it', async () => {
		mockGet([job])

		render(CronJobEditor, { agentId: '_superagent' })

		await expect.element(page.getByText('07:00')).toBeVisible()
		await expect.element(page.getByText('Daily summary')).toBeVisible()
		await expect.element(page.getByText('daily', { exact: true })).toBeVisible()
		expect(getCronJobs).toHaveBeenCalledWith('_superagent')
	})

	it('creates a job through its endpoint on inline save', async () => {
		mockGet([])
		vi.mocked(createCronJob).mockImplementation(async (_id, created) => created)

		render(CronJobEditor, { agentId: '_superagent' })
		await page.getByRole('button', { name: /add scheduled prompt/i }).click()
		await page.getByLabelText(/prompt/i).fill('Weekly cache cleanup')
		await page
			.getByRole('button', { name: /^save$/i })
			.first()
			.click()

		await expect.element(page.getByText('Weekly cache cleanup')).toBeVisible()
		expect(createCronJob).toHaveBeenCalledTimes(1)
		// The stored row came back through the cache, not a refetch
		expect(getCronJobs).toHaveBeenCalledTimes(1)
	})

	it('saves the full edited job through its endpoint', async () => {
		mockGet([job])
		vi.mocked(updateCronJob).mockImplementation(async (_id, edited) => edited)

		render(CronJobEditor, { agentId: '_superagent' })
		await page.getByRole('button', { name: 'Edit' }).click()
		await page.getByLabelText(/prompt/i).fill('Evening summary')
		await page
			.getByRole('button', { name: /^save$/i })
			.first()
			.click()

		await expect.element(page.getByText('Evening summary')).toBeVisible()
		expect(updateCronJob).toHaveBeenCalledWith(
			'_superagent',
			expect.objectContaining({ id: '1', prompt: 'Evening summary' })
		)
	})

	it('deletes a job through its endpoint', async () => {
		mockGet([job])
		vi.mocked(deleteCronJob).mockResolvedValue(undefined)

		render(CronJobEditor, { agentId: '_superagent' })
		await page.getByRole('button', { name: 'Delete' }).click()

		await expect.element(page.getByText(/no scheduled prompts/i)).toBeVisible()
		expect(deleteCronJob).toHaveBeenCalledWith('_superagent', '1')
	})

	it('surfaces endpoint failures next to the list', async () => {
		mockGet([])
		vi.mocked(createCronJob).mockRejectedValue(new Error('boom'))

		render(CronJobEditor, { agentId: '_superagent' })
		await page.getByRole('button', { name: /add scheduled prompt/i }).click()
		await page.getByLabelText(/prompt/i).fill('Doomed prompt')
		await page
			.getByRole('button', { name: /^save$/i })
			.first()
			.click()

		await expect.element(page.getByText('boom')).toBeVisible()
	})
})
