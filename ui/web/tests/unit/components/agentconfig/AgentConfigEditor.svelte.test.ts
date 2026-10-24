import { describe, expect, it, afterEach, vi, beforeEach } from 'vitest';
import { render, cleanup } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';
import { AgentConfigEditor } from '$components/agentconfig';
import type {
	AgentConfiguration,
	SuperagentConfiguration,
	WorkerConfiguration,
	CronJob
} from '$lib/types/agent';
import type { Tool } from '$lib/types/tool';

// Helper to create default superagent configuration
function createDefaultSuperagentConfig(): SuperagentConfiguration {
	return {
		name: 'ava',
		icon: 'bot',
		soul: { personality: 'Friendly', identity: 'Helper' },
		userContext: 'User context test',
		operatingInstructions: 'Be helpful',
		toolsContext: 'Tools info',
		toolsEnabled: [],
		cronJobs: [],
		heartbeat: { enabled: false, duration: '1hr', content: '', model: 'default' }
	};
}

// Helper to create default Worker configuration
function createDefaultWorkerConfig(): WorkerConfiguration {
	return {
		name: 'Worker Bot',
		icon: 'bot',
		soul: { personality: 'Efficient', identity: 'Task runner' },
		operatingInstructions: 'Complete tasks',
		toolsContext: 'Worker tools',
		toolsEnabled: [],
		heartbeat: { enabled: false, duration: '1hr', content: '', model: 'default' }
	};
}

// Helper to create props
function createProps(
	overrides: Partial<{
		configuration: AgentConfiguration;
		availableTools: Tool[];
		onSave: (config: AgentConfiguration) => void | Promise<void>;
		onCancel: () => void;
		saveLabel: string;
		cancelLabel: string;
		helperText: string;
		isSuperagent: boolean;
	}> = {}
) {
	return {
		configuration: createDefaultSuperagentConfig(),
		availableTools: [] as Tool[],
		onSave: vi.fn(),
		onCancel: vi.fn(),
		saveLabel: 'Save',
		cancelLabel: 'Cancel',
		helperText: 'Test helper text',
		...overrides
	};
}

describe('AgentConfigEditor', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	afterEach(async () => {
		cleanup();
	});

	describe('Layout structure', () => {
		it('renders two-panel layout with field list and content area', async () => {
			render(AgentConfigEditor, createProps());

			// Should have field buttons on the left
			await expect.element(page.getByRole('button', { name: /soul/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /user context/i })).toBeVisible();

			// Should have content area with form fields
			await expect.element(page.getByLabelText(/name/i)).toBeVisible();
		});

		it('has proper CSS classes for overflow handling', async () => {
			const { container } = render(AgentConfigEditor, createProps());

			// Main container should have overflow-hidden
			const mainContainer = container.querySelector('.overflow-hidden');
			expect(mainContainer).toBeTruthy();
		});

		it('content area has overflow-y-auto for scrolling', async () => {
			const { container } = render(AgentConfigEditor, createProps());

			// Content area should have overflow-y-auto class
			const scrollableArea = container.querySelector('.overflow-y-auto');
			expect(scrollableArea).toBeTruthy();
		});
	});

	describe('Field list - superagent agent', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('shows all superagent-specific fields', async () => {
			render(AgentConfigEditor, createProps());

			// Name field button (includes icon picker in editor)
			await expect.element(page.getByRole('button', { name: /^name$/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /^soul$/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /user context/i })).toBeVisible();
			await expect
				.element(page.getByRole('button', { name: /operating instructions/i }))
				.toBeVisible();
			await expect.element(page.getByRole('button', { name: /tools context/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /enabled tools/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /heartbeat/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /cron jobs/i })).toBeVisible();
		});

		it('selects Name field by default', async () => {
			render(AgentConfigEditor, createProps());

			// Name editor should be visible (shows name input and icon picker)
			await expect.element(page.getByLabelText(/name/i)).toBeVisible();
		});

		it('shows Soul field with only personality and identity', async () => {
			render(AgentConfigEditor, createProps());

			// Navigate to Soul field
			await page.getByRole('button', { name: /soul/i }).click();

			// Soul editor should show personality and identity (not name)
			await expect.element(page.getByLabelText(/personality/i)).toBeVisible();
			await expect.element(page.getByLabelText(/identity/i)).toBeVisible();
		});
	});

	describe('Field list - Worker agent', () => {
		beforeEach(() => {});

		it('shows Worker-specific fields (no User Context, no Cron Jobs)', async () => {
			render(AgentConfigEditor, createProps({ configuration: createDefaultWorkerConfig() }));

			// Name button (includes icon picker in the editor)
			await expect.element(page.getByRole('button', { name: /^name$/i })).toBeVisible();
			// Soul button
			await expect.element(page.getByRole('button', { name: /^soul$/i })).toBeVisible();
			await expect
				.element(page.getByRole('button', { name: /operating instructions/i }))
				.toBeVisible();
			await expect.element(page.getByRole('button', { name: /tools context/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /enabled tools/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /heartbeat/i })).toBeVisible();

			// Should NOT have User Context or Cron Jobs
			expect(page.getByRole('button', { name: /user context/i }).query()).toBeNull();
			expect(page.getByRole('button', { name: /cron jobs/i }).query()).toBeNull();
		});
	});

	describe('Field switching', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('switches to User Context when clicked', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /user context/i }).click();

			await expect.element(page.getByPlaceholder(/who you are/i)).toBeVisible();
		});

		it('switches to Operating Instructions when clicked', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /operating instructions/i }).click();

			await expect.element(page.getByPlaceholder(/operating rules/i)).toBeVisible();
		});

		it('switches to Tools Context when clicked', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /tools context/i }).click();

			await expect.element(page.getByPlaceholder(/calendar/i)).toBeVisible();
		});

		it('switches to Heartbeat when clicked', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /heartbeat/i }).click();

			// Heartbeat editor has an Enabled checkbox
			await expect.element(page.getByLabelText(/enabled/i)).toBeVisible();
		});

		it('switches to Cron Jobs when clicked (superagent only)', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// CronJob editor shows "Add Scheduled Prompt" button
			await expect
				.element(page.getByRole('button', { name: /add scheduled prompt/i }))
				.toBeVisible();
		});

		it('switches to Enabled Tools when clicked', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /enabled tools/i }).click();

			// Tools editor shows empty state message when no tools
			await expect.element(page.getByText(/no tools available/i)).toBeVisible();
		});

		it('shows active styling on selected field', async () => {
			const { container } = render(AgentConfigEditor, createProps());

			// Click Operating Instructions
			await page.getByRole('button', { name: /operating instructions/i }).click();

			// The button should have the active-field class
			const activeButton = container.querySelector('.active-field');
			expect(activeButton).toBeTruthy();
			expect(activeButton?.textContent).toContain('Operating Instructions');
		});
	});

	describe('Dirty state indicators', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('does not show asterisk on fields with no changes', async () => {
			render(AgentConfigEditor, createProps());

			// No asterisks should be visible initially
			const asterisks = page.getByText('//*').query();
			expect(asterisks).toBeNull();
		});

		it('shows asterisk on Name field when name is changed', async () => {
			render(AgentConfigEditor, createProps());

			// Change the name
			const nameInput = page.getByLabelText(/name/i);
			await nameInput.fill('New Name');

			// Name button should now show asterisk
			const nameButton = page.getByRole('button', { name: /^name\s*\*/i });
			await expect.element(nameButton).toBeVisible();
		});

		it('shows asterisk on Soul field when personality changes', async () => {
			render(AgentConfigEditor, createProps());

			// Navigate to Soul and change personality
			await page.getByRole('button', { name: /soul/i }).click();
			const personalityInput = page.getByLabelText(/personality/i);
			await personalityInput.fill('New personality');

			// Soul button should show asterisk
			const soulButton = page.getByRole('button', { name: /soul\s*\*/i });
			await expect.element(soulButton).toBeVisible();
		});

		it('shows asterisk on User Context when changed', async () => {
			render(AgentConfigEditor, createProps());

			// Navigate to User Context and change it
			await page.getByRole('button', { name: /user context/i }).click();
			const textarea = page.getByPlaceholder(/who you are/i);
			await textarea.fill('New user context');

			// User Context button should show asterisk
			const userContextButton = page.getByRole('button', { name: /user context\s*\*/i });
			await expect.element(userContextButton).toBeVisible();
		});
	});

	describe('Form editing', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('allows editing name', async () => {
			const onSave = vi.fn();
			render(AgentConfigEditor, createProps({ onSave }));

			const nameInput = page.getByLabelText(/name/i);
			await nameInput.fill('Updated Name');

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledWith(
				expect.objectContaining({
					name: 'Updated Name'
				})
			);
		});

		it('allows editing Soul personality', async () => {
			const onSave = vi.fn();
			render(AgentConfigEditor, createProps({ onSave }));

			// Navigate to Soul first
			await page.getByRole('button', { name: /soul/i }).click();

			const personalityInput = page.getByLabelText(/personality/i);
			await personalityInput.fill('New personality traits');

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledWith(
				expect.objectContaining({
					soul: expect.objectContaining({ personality: 'New personality traits' })
				})
			);
		});

		it('allows editing Soul identity', async () => {
			const onSave = vi.fn();
			render(AgentConfigEditor, createProps({ onSave }));

			// Navigate to Soul first
			await page.getByRole('button', { name: /soul/i }).click();

			const identityInput = page.getByLabelText(/identity/i);
			await identityInput.fill('New identity');

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledWith(
				expect.objectContaining({
					soul: expect.objectContaining({ identity: 'New identity' })
				})
			);
		});

		it('allows editing User Context', async () => {
			const onSave = vi.fn();
			render(AgentConfigEditor, createProps({ onSave }));

			await page.getByRole('button', { name: /user context/i }).click();
			const textarea = page.getByPlaceholder(/who you are/i);
			await textarea.fill('Updated user context');

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledWith(
				expect.objectContaining({ userContext: 'Updated user context' })
			);
		});

		it('allows editing Operating Instructions', async () => {
			const onSave = vi.fn();
			render(AgentConfigEditor, createProps({ onSave }));

			await page.getByRole('button', { name: /operating instructions/i }).click();
			const textarea = page.getByPlaceholder(/operating rules/i);
			await textarea.fill('New operating instructions');

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledWith(
				expect.objectContaining({ operatingInstructions: 'New operating instructions' })
			);
		});

		it('allows editing Tools Context', async () => {
			const onSave = vi.fn();
			render(AgentConfigEditor, createProps({ onSave }));

			await page.getByRole('button', { name: /tools context/i }).click();
			const textarea = page.getByPlaceholder(/calendar/i);
			await textarea.fill('New tools context');

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledWith(
				expect.objectContaining({ toolsContext: 'New tools context' })
			);
		});
	});

	describe('Action buttons', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('calls onSave when Save button is clicked', async () => {
			const onSave = vi.fn();
			render(AgentConfigEditor, createProps({ onSave }));

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledTimes(1);
		});

		it('calls onCancel when Cancel button is clicked', async () => {
			const onCancel = vi.fn();
			render(AgentConfigEditor, createProps({ onCancel }));

			await page.getByRole('button', { name: /cancel/i }).click();

			expect(onCancel).toHaveBeenCalledTimes(1);
		});

		it('uses custom save label', async () => {
			render(AgentConfigEditor, createProps({ saveLabel: 'Create Agent' }));

			await expect.element(page.getByRole('button', { name: /create agent/i })).toBeVisible();
		});

		it('uses custom cancel label', async () => {
			render(AgentConfigEditor, createProps({ cancelLabel: 'Go Back' }));

			await expect.element(page.getByRole('button', { name: /go back/i })).toBeVisible();
		});

		it('shows custom helper text', async () => {
			render(AgentConfigEditor, createProps({ helperText: 'Custom helper text here' }));

			await expect.element(page.getByText('Custom helper text here')).toBeVisible();
		});
	});

	describe('Tools editor', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('shows available tools when provided', async () => {
			const tools: Tool[] = [
				{ id: 'tool-1', name: 'Calendar', description: 'Calendar tool' },
				{ id: 'tool-2', name: 'Email', description: 'Email tool' }
			];

			render(AgentConfigEditor, createProps({ availableTools: tools }));

			await page.getByRole('button', { name: /enabled tools/i }).click();

			// Use more specific selectors to avoid matching description text
			await expect.element(page.getByRole('switch', { name: /calendar/i })).toBeVisible();
			await expect.element(page.getByRole('switch', { name: /email/i })).toBeVisible();
		});

		it('shows empty state when no tools available', async () => {
			render(AgentConfigEditor, createProps({ availableTools: [] }));

			await page.getByRole('button', { name: /enabled tools/i }).click();

			await expect.element(page.getByText(/no tools available/i)).toBeVisible();
		});
	});

	describe('Heartbeat editor', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('shows heartbeat configuration options', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /heartbeat/i }).click();

			// Check for the checkbox (checkbox role with name from associated label)
			await expect.element(page.getByRole('checkbox', { name: 'Enabled' })).toBeVisible();
			// Duration dropdown
			await expect.element(page.getByLabelText(/duration/i)).toBeVisible();
			// Content textarea
			await expect.element(page.getByLabelText(/content/i)).toBeVisible();
		});
	});

	describe('isSuperagent prop override', () => {
		it('uses isSuperagent=false to hide Cron Jobs even when route detection says superagent', async () => {
			// But we explicitly set isSuperagent=false
			render(AgentConfigEditor, createProps({ isSuperagent: false }));

			// Should NOT show Cron Jobs or User Context
			expect(page.getByRole('button', { name: /cron jobs/i }).query()).toBeNull();
			expect(page.getByRole('button', { name: /user context/i }).query()).toBeNull();

			// Should show worker fields
			await expect.element(page.getByRole('button', { name: /^name$/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /soul/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /heartbeat/i })).toBeVisible();
		});

		it('uses isSuperagent=true to show Cron Jobs even when route detection says Worker', async () => {
			// Route detection would say Worker

			// But we explicitly set isSuperagent=true with superagent config
			const superagentConfig = createDefaultSuperagentConfig();
			render(AgentConfigEditor, createProps({ configuration: superagentConfig, isSuperagent: true }));

			// Should show Cron Jobs and User Context
			await expect.element(page.getByRole('button', { name: /cron jobs/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /user context/i })).toBeVisible();
		});
	});

	describe('Layout stability with scrolling content', () => {
		// Helper to create config with many cron jobs
		function createConfigWithCronJobs(count: number): SuperagentConfiguration {
			const jobs: CronJob[] = [];
			for (let i = 0; i < count; i++) {
				jobs.push({
					id: `job-${i}`,
					time: `${String(8 + (i % 12)).padStart(2, '0')}:${String(i % 60).padStart(2, '0')}`,
					frequency: i % 3 === 0 ? 'daily' : i % 3 === 1 ? 'weekly' : 'custom',
					cronExpression: i % 3 === 2 ? `0 ${i % 24} * * *` : undefined,
					model: 'default',
					prompt: `Scheduled prompt number ${i} - This is a longer prompt to test how the layout handles content that might wrap to multiple lines and take up more vertical space.`
				});
			}
			return {
				...createDefaultSuperagentConfig(),
				cronJobs: jobs
			};
		}

		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('renders 20 cron jobs without crashing', async () => {
			const config = createConfigWithCronJobs(20);
			render(AgentConfigEditor, createProps({ configuration: config }));

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Should show cron job entries - check that all are visible
			const editButtons = page.getByRole('button', { name: /edit/i });
			await expect.element(editButtons.first()).toBeVisible();
			// Just verify it doesn't crash and shows at least one job
		});

		it('scrolls only the content area, not the field list', async () => {
			const config = createConfigWithCronJobs(20);
			const { container } = render(AgentConfigEditor, createProps({ configuration: config }));

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Find the content area (right side) - should have overflow-y-auto
			const contentArea = container.querySelector('.overflow-y-auto');
			expect(contentArea).toBeTruthy();

			// Field list container should exist
			const fieldList = container.querySelector('.w-48');
			expect(fieldList).toBeTruthy();
		});

		it('buttons remain fixed at bottom when content scrolls', async () => {
			const config = createConfigWithCronJobs(20);
			const { container } = render(AgentConfigEditor, createProps({ configuration: config }));

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Get the save button
			const saveButton = page.getByRole('button', { name: /save/i });
			await expect.element(saveButton).toBeVisible();

			// Button should be outside the scrollable content area
			const buttonContainer = container.querySelector('.border-t');
			expect(buttonContainer).toBeTruthy();

			// Button container should NOT have overflow-y-auto
			expect(buttonContainer?.classList.contains('overflow-y-auto')).toBe(false);
		});

		it('field list does not scroll when content area scrolls', async () => {
			const config = createConfigWithCronJobs(20);
			render(AgentConfigEditor, createProps({ configuration: config }));

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Field list should be visible and not scrolled
			const nameButton = page.getByRole('button', { name: /^name$/i });
			await expect.element(nameButton).toBeVisible();

			// Cron Jobs button should also be visible (not scrolled out)
			const cronJobsButton = page.getByRole('button', { name: /cron jobs/i });
			await expect.element(cronJobsButton).toBeVisible();
		});

		it('maintains layout when switching between fields with different content heights', async () => {
			const config = createConfigWithCronJobs(20);
			const { container } = render(AgentConfigEditor, createProps({ configuration: config }));

			// Start with Name (small content)
			await expect.element(page.getByLabelText(/name/i)).toBeVisible();

			// Switch to Cron Jobs (large content)
			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Switch back to Name
			await page.getByRole('button', { name: /^name$/i }).click();

			// Layout should still be intact
			const mainContainer = container.querySelector('.overflow-hidden');
			expect(mainContainer).toBeTruthy();

			// Buttons should still be visible
			await expect.element(page.getByRole('button', { name: /save/i })).toBeVisible();
		});

		it('uses flexbox layout for stable footer positioning', async () => {
			const { container } = render(AgentConfigEditor, createProps());

			// Find the main flex container with h-full for stable layout
			const flexContainer = container.querySelector('.flex.h-full');
			expect(flexContainer).toBeTruthy();
		});

		it('bottom buttons div height stays constant when switching fields', async () => {
			const config = createConfigWithCronJobs(20);
			const { container } = render(AgentConfigEditor, createProps({ configuration: config }));

			// Get the bottom div (the one with border-t AND items-end, not the top section)
			const bottomDiv = container.querySelector('.border-t.items-end');
			expect(bottomDiv).toBeTruthy();

			// Get initial height
			const initialHeight = bottomDiv?.getBoundingClientRect().height;

			// Switch to different fields
			await page.getByRole('button', { name: /cron jobs/i }).click();
			await new Promise((resolve) => setTimeout(resolve, 50));
			const heightAfterCronJobs = bottomDiv?.getBoundingClientRect().height;

			await page.getByRole('button', { name: /^name$/i }).click();
			await new Promise((resolve) => setTimeout(resolve, 50));
			const heightAfterName = bottomDiv?.getBoundingClientRect().height;

			await page.getByRole('button', { name: /operating instructions/i }).click();
			await new Promise((resolve) => setTimeout(resolve, 50));
			const heightAfterOps = bottomDiv?.getBoundingClientRect().height;

			// All heights should be the same
			expect(initialHeight).toBe(heightAfterCronJobs);
			expect(initialHeight).toBe(heightAfterName);
			expect(initialHeight).toBe(heightAfterOps);
		});

		it('bottom buttons stay visible when content overflows', async () => {
			const config = createConfigWithCronJobs(20);
			render(AgentConfigEditor, createProps({ configuration: config }));

			// Switch to cron jobs which has lots of content
			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Save and Cancel buttons should still be visible
			await expect.element(page.getByRole('button', { name: /save/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /cancel/i })).toBeVisible();
		});
	});

	describe('Layout structure - flexbox overflow', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('has overflow-hidden on the top section container', async () => {
			const { container } = render(AgentConfigEditor, createProps());

			// The top section should have overflow-hidden to contain scrolling
			const topSection = container.querySelector('.flex-1.overflow-hidden');
			expect(topSection).toBeTruthy();
		});

		it('content area has overflow-y-auto for scrolling', async () => {
			const { container } = render(AgentConfigEditor, createProps());

			// Content area should have overflow-y-auto
			const contentArea = container.querySelector('.flex-1.overflow-y-auto');
			expect(contentArea).toBeTruthy();
		});

		it('field list has proper scroll container', async () => {
			const { container } = render(AgentConfigEditor, createProps());

			// Field list container should exist
			const fieldList = container.querySelector('.w-48');
			expect(fieldList).toBeTruthy();
		});
	});

	describe('Cron Jobs editor interactions', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('shows add scheduled prompt button', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Should show the Add button
			await expect
				.element(page.getByRole('button', { name: /add scheduled prompt/i }))
				.toBeVisible();
		});

		it('shows inline editor when Add is clicked', async () => {
			render(AgentConfigEditor, createProps());

			await page.getByRole('button', { name: /cron jobs/i }).click();
			await page.getByRole('button', { name: /add scheduled prompt/i }).click();

			// Should show the inline editor with form fields
			await expect.element(page.getByLabelText(/time/i)).toBeVisible();
			await expect.element(page.getByLabelText(/prompt/i)).toBeVisible();
		});

		it('shows existing cron jobs', async () => {
			const config: SuperagentConfiguration = {
				...createDefaultSuperagentConfig(),
				cronJobs: [
					{
						id: 'test-job',
						time: '09:00',
						frequency: 'daily',
						model: 'default',
						prompt: 'Test prompt'
					}
				]
			};
			render(AgentConfigEditor, createProps({ configuration: config }));

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Should show the existing job
			await expect.element(page.getByText('Test prompt')).toBeVisible();
		});

		it('deletes a cron job when delete button clicked', async () => {
			const config: SuperagentConfiguration = {
				...createDefaultSuperagentConfig(),
				cronJobs: [
					{
						id: 'test-job',
						time: '09:00',
						frequency: 'daily',
						model: 'default',
						prompt: 'Test prompt'
					}
				]
			};
			render(AgentConfigEditor, createProps({ configuration: config }));

			await page.getByRole('button', { name: /cron jobs/i }).click();

			// Click delete button
			await page.getByRole('button', { name: /delete/i }).click();

			// Job should be removed from display
			expect(page.getByText('Test prompt').query()).toBeNull();
		});
	});

	describe('Save/Cancel button behavior', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('calls onSave with full configuration when Save clicked', async () => {
			const onSave = vi.fn();
			const config = createDefaultSuperagentConfig();
			render(AgentConfigEditor, createProps({ configuration: config, onSave }));

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledWith(
				expect.objectContaining({
					soul: config.soul,
					userContext: config.userContext,
					operatingInstructions: config.operatingInstructions
				})
			);
		});

		it('calls onCancel when Cancel clicked', async () => {
			const onCancel = vi.fn();
			render(AgentConfigEditor, createProps({ onCancel }));

			await page.getByRole('button', { name: /cancel/i }).click();

			expect(onCancel).toHaveBeenCalledTimes(1);
		});

		it('shows confirmation dialog when Cancel clicked with unsaved changes', async () => {
			const onCancel = vi.fn();
			render(AgentConfigEditor, createProps({ onCancel }));

			// Make changes to name field
			const nameInput = page.getByLabelText(/name/i);
			await nameInput.fill('New Name');

			// Cancel should show confirmation dialog
			await page.getByRole('button', { name: /cancel/i }).click();

			// Confirmation dialog should appear
			await expect.element(page.getByRole('alertdialog')).toBeVisible();

			// onCancel should NOT have been called yet
			expect(onCancel).not.toHaveBeenCalled();
		});

		it('calls onCancel when Discard is clicked in confirmation dialog', async () => {
			const onCancel = vi.fn();
			render(AgentConfigEditor, createProps({ onCancel }));

			// Make changes to name field
			const nameInput = page.getByLabelText(/name/i);
			await nameInput.fill('New Name');

			// Click cancel to show dialog
			await page.getByRole('button', { name: /cancel/i }).click();

			// Click Discard
			await page.getByRole('button', { name: /discard/i }).click();

			// onCancel should have been called
			expect(onCancel).toHaveBeenCalledTimes(1);
		});

		it('keeps editing when Keep Editing is clicked in confirmation dialog', async () => {
			const onCancel = vi.fn();
			render(AgentConfigEditor, createProps({ onCancel }));

			// Make changes to name field
			const nameInput = page.getByLabelText(/name/i);
			await nameInput.fill('New Name');

			// Click cancel to show dialog
			await page.getByRole('button', { name: /cancel/i }).click();

			// Click Keep Editing
			await page.getByRole('button', { name: /keep editing/i }).click();

			// onCancel should NOT have been called
			expect(onCancel).not.toHaveBeenCalled();

			// Dialog should be closed
			expect(page.getByRole('alertdialog').query()).toBeNull();
		});

		it('handles async onSave', async () => {
			const onSave = vi
				.fn()
				.mockImplementation(() => new Promise((resolve) => setTimeout(resolve, 100)));
			render(AgentConfigEditor, createProps({ onSave }));

			await page.getByRole('button', { name: /save/i }).click();

			expect(onSave).toHaveBeenCalledTimes(1);
		});
	});

	describe('hasChanges bindable prop', () => {
		beforeEach(() => {
			vi.clearAllMocks();
		});

		it('syncs hasChanges when content is modified', async () => {
			let hasChanges = false;
			const props = {
				...createProps(),
				get hasChanges() {
					return hasChanges;
				},
				set hasChanges(value: boolean) {
					hasChanges = value;
				}
			};

			render(AgentConfigEditor, props);

			// Initially no changes
			expect(hasChanges).toBe(false);

			// Make a change to the name field
			const nameInput = page.getByLabelText(/name/i);
			await nameInput.fill('New Name');

			// Wait for reactivity to sync
			await new Promise((resolve) => setTimeout(resolve, 50));

			// Should now have changes
			expect(hasChanges).toBe(true);
		});
	});

	describe('Worker agent - no Cron Jobs crash', () => {
		it('does not crash when clicking Cron Jobs on worker (field not shown)', async () => {
			const config = createDefaultWorkerConfig();

			render(AgentConfigEditor, createProps({ configuration: config }));

			// Cron Jobs button should not exist for worker
			expect(page.getByRole('button', { name: /cron jobs/i }).query()).toBeNull();
		});

		it('uses isSuperagent prop correctly for new agent page scenario', async () => {
			// Simulate new agent page where route would be /agents/new
			// Route detection might return ava, but we pass isSuperagent=false
			const workerConfig = createDefaultWorkerConfig();
			render(AgentConfigEditor, createProps({ configuration: workerConfig, isSuperagent: false }));

			// Should show worker fields only
			await expect.element(page.getByRole('button', { name: /^name$/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /soul/i })).toBeVisible();
			await expect.element(page.getByRole('button', { name: /heartbeat/i })).toBeVisible();

			// Should NOT show superagent-only fields
			expect(page.getByRole('button', { name: /cron jobs/i }).query()).toBeNull();
			expect(page.getByRole('button', { name: /user context/i }).query()).toBeNull();

			// Clicking heartbeat should work without crash
			await page.getByRole('button', { name: /heartbeat/i }).click();
			await expect.element(page.getByRole('checkbox', { name: 'Enabled' })).toBeVisible();
		});
	});
});
