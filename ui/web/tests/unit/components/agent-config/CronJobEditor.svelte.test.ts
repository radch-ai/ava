import { afterEach, describe, expect, it } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import CronJobEditor from '#components/agent-config/CronJobEditor.svelte'
import type { CronJob } from '#lib/types/agent'

describe('CronJobEditor', () => {
	afterEach(async () => {
		cleanup()
	})

	describe('Empty state', () => {
		it('shows "Add" button when no jobs exist', async () => {
			render(CronJobEditor, { jobs: [] })

			await expect
				.element(page.getByRole('button', { name: /add/i }))
				.toBeVisible()
		})

		it('shows empty state message', async () => {
			render(CronJobEditor, { jobs: [] })

			await expect
				.element(page.getByText(/no scheduled prompts/i))
				.toBeVisible()
		})
	})

	describe('List display', () => {
		it('displays cron jobs in a list', async () => {
			const jobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Good morning! Here is your daily summary.'
				},
				{
					id: '2',
					time: '12:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Lunch time reminder.'
				}
			]

			render(CronJobEditor, { jobs })

			await expect.element(page.getByText('07:00')).toBeVisible()
			await expect.element(page.getByText('12:00')).toBeVisible()
		})

		it('shows frequency badge for each job', async () => {
			const jobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily summary'
				}
			]

			render(CronJobEditor, { jobs })

			// Badge is in a span with bg-primary/10 class - use getByText with exact match
			// Since "daily" is lowercase in the badge, use exact match
			const badge = page.getByText('daily', { exact: true })
			await expect.element(badge).toBeVisible()
		})

		it('shows prompt text in list', async () => {
			const jobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily summary'
				}
			]

			render(CronJobEditor, { jobs })

			await expect.element(page.getByText('Daily summary')).toBeVisible()
		})

		it('shows cron expression for custom frequency', async () => {
			const jobs: CronJob[] = [
				{
					id: '1',
					time: '09:00',
					frequency: 'custom',
					cronExpression: '0 9 * * 1-5',
					model: 'gpt-4',
					prompt: 'Weekday morning reminder'
				}
			]

			render(CronJobEditor, { jobs })

			await expect.element(page.getByText('0 9 * * 1-5')).toBeVisible()
		})
	})

	describe('Job actions', () => {
		it('has edit button for each job', async () => {
			const jobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily summary'
				}
			]

			render(CronJobEditor, { jobs })

			await expect
				.element(page.getByRole('button', { name: 'Edit' }))
				.toBeVisible()
		})

		it('has delete button for each job', async () => {
			const jobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily summary'
				}
			]

			render(CronJobEditor, { jobs })

			await expect
				.element(page.getByRole('button', { name: 'Delete' }))
				.toBeVisible()
		})

		it('removes job from list when delete is clicked', async () => {
			const initialJobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily summary'
				}
			]

			render(CronJobEditor, { jobs: initialJobs })

			// Verify job is shown
			await expect.element(page.getByText('Daily summary')).toBeVisible()

			const deleteButton = page.getByRole('button', { name: 'Delete' })
			await deleteButton.click()

			// Verify job is removed from UI
			await expect
				.element(page.getByText('Daily summary'))
				.not.toBeInTheDocument()
			await expect
				.element(page.getByText(/no scheduled prompts/i))
				.toBeVisible()
		})
	})

	describe('Inline editor', () => {
		it('shows inline editor when add is clicked', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Should show time input - use textbox role with aria-label name
			const timeInput = page.getByRole('textbox', { name: 'Time' })
			await expect.element(timeInput).toBeVisible()
		})

		it('shows frequency dropdown in editor', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Should show frequency trigger button
			await expect.element(page.getByText('Daily')).toBeVisible()
		})

		it('shows prompt textarea in editor', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Should show prompt textarea by aria-label
			const textarea = page.getByRole('textbox', { name: 'Prompt' })
			await expect.element(textarea).toBeVisible()
		})

		it('has cancel and save buttons in editor', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			await expect
				.element(page.getByRole('button', { name: 'Cancel' }))
				.toBeVisible()
			await expect
				.element(page.getByRole('button', { name: 'Save' }))
				.toBeVisible()
		})

		it('adds job to list when save is clicked with new job', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Fill in the form
			const textarea = page.getByRole('textbox', { name: 'Prompt' })
			await textarea.fill('Test prompt')

			const saveButton = page.getByRole('button', { name: 'Save' })
			await saveButton.click()

			// Verify job appears in the list (UI-based test)
			await expect.element(page.getByText('Test prompt')).toBeVisible()
		})

		it('updates job in list when edit is saved', async () => {
			const initialJobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily summary'
				}
			]

			render(CronJobEditor, { jobs: initialJobs })

			const editButton = page.getByRole('button', { name: 'Edit' })
			await editButton.click()

			// Modify the prompt
			const textarea = page.getByRole('textbox', { name: 'Prompt' })
			await textarea.fill('Updated prompt')

			const saveButton = page.getByRole('button', { name: 'Save' })
			await saveButton.click()

			// Verify updated prompt appears in the list (UI-based test)
			await expect.element(page.getByText('Updated prompt')).toBeVisible()
			await expect
				.element(page.getByText('Daily summary'))
				.not.toBeInTheDocument()
		})

		it('cancels editing when cancel is clicked', async () => {
			const jobs: CronJob[] = [
				{
					id: '1',
					time: '07:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily summary'
				}
			]

			render(CronJobEditor, { jobs })

			const editButton = page.getByRole('button', { name: 'Edit' })
			await editButton.click()

			const cancelButton = page.getByRole('button', { name: 'Cancel' })
			await cancelButton.click()

			// Editor should be hidden, list should be visible
			await expect
				.element(page.getByRole('button', { name: 'Edit' }))
				.toBeVisible()
		})
	})

	describe('Validation', () => {
		it('disables save when prompt is empty', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Save should be disabled without prompt
			const saveButton = page.getByRole('button', { name: 'Save' })
			await expect.element(saveButton).toBeDisabled()
		})

		it('enables save when prompt is filled', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			const textarea = page.getByRole('textbox', { name: 'Prompt' })
			await textarea.fill('Valid prompt')

			const saveButton = page.getByRole('button', { name: 'Save' })
			await expect.element(saveButton).not.toBeDisabled()
		})
	})

	describe('Custom frequency', () => {
		it('shows cron expression input when custom frequency is selected', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Select custom frequency
			const frequencyTrigger = page.getByText('Daily')
			await frequencyTrigger.click()

			const customOption = page.getByRole('option', { name: 'Custom' })
			await customOption.click()

			// Should show cron expression input
			await expect.element(page.getByLabelText('Cron Expression')).toBeVisible()
		})

		it('disables save when custom frequency is selected but cron expression is empty', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Fill prompt
			const textarea = page.getByRole('textbox', { name: 'Prompt' })
			await textarea.fill('Valid prompt')

			// Select custom frequency
			const frequencyTrigger = page.getByText('Daily')
			await frequencyTrigger.click()

			const customOption = page.getByRole('option', { name: 'Custom' })
			await customOption.click()

			// Save should be disabled without cron expression
			const saveButton = page.getByRole('button', { name: 'Save' })
			await expect.element(saveButton).toBeDisabled()
		})

		it('enables save when custom frequency has cron expression', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Fill prompt
			const textarea = page.getByRole('textbox', { name: 'Prompt' })
			await textarea.fill('Valid prompt')

			// Select custom frequency
			const frequencyTrigger = page.getByText('Daily')
			await frequencyTrigger.click()

			const customOption = page.getByRole('option', { name: 'Custom' })
			await customOption.click()

			// Fill cron expression
			const cronInput = page.getByLabelText('Cron Expression')
			await cronInput.fill('0 9 * * 1-5')

			// Save should be enabled
			const saveButton = page.getByRole('button', { name: 'Save' })
			await expect.element(saveButton).not.toBeDisabled()
		})

		it('saves job with cron expression', async () => {
			render(CronJobEditor, { jobs: [] })

			const addButton = page.getByRole('button', { name: /add/i })
			await addButton.click()

			// Fill prompt
			const textarea = page.getByRole('textbox', { name: 'Prompt' })
			await textarea.fill('Weekday reminder')

			// Select custom frequency
			const frequencyTrigger = page.getByText('Daily')
			await frequencyTrigger.click()

			const customOption = page.getByRole('option', { name: 'Custom' })
			await customOption.click()

			// Fill cron expression
			const cronInput = page.getByLabelText('Cron Expression')
			await cronInput.fill('0 9 * * 1-5')

			const saveButton = page.getByRole('button', { name: 'Save' })
			await saveButton.click()

			// Verify job appears in the list with cron expression (UI-based test)
			await expect.element(page.getByText('Weekday reminder')).toBeVisible()
			await expect.element(page.getByText('0 9 * * 1-5')).toBeVisible()
		})
	})
})
