<script lang="ts">
	import { Pencil, Plus, X } from '@lucide/svelte'
	import ModelSelector from '#components/ModelSelector.svelte'
	import { Button } from '#components/ui/button'
	import { Input } from '#components/ui/input'
	import * as Select from '#components/ui/select'
	import { Textarea } from '#components/ui/textarea'
	import type { ModelSelection } from '#lib/models'
	import {
		createCachedCronJob,
		cronJobsFor,
		deleteCachedCronJob,
		loadCronJobs,
		updateCachedCronJob
	} from '#lib/services/agent/cron-jobs.svelte'
	import type { CronJob } from '#lib/types/agent'

	type Frequency = CronJob['frequency']

	// Undefined agentId = the agent is not created yet (the new-agent
	// page): nothing to fetch or save, only the placeholder shows.
	interface Props {
		agentId?: string
	}

	let { agentId }: Props = $props()

	// The list lives in the cron cache; fetch happens here, on first
	// panel open, not with the agent detail.
	let jobs = $derived(cronJobsFor(agentId ?? '') ?? [])
	let isLoading = $state(false)
	let loadError = $state<string | null>(null)
	let actionError = $state<string | null>(null)
	let busy = $state(false)

	$effect(() => {
		const id = agentId
		if (!id || cronJobsFor(id)) return
		isLoading = true
		loadError = null
		loadCronJobs(id)
			.catch((error: unknown) => {
				loadError = error instanceof Error ? error.message : 'Failed to load'
			})
			.finally(() => {
				isLoading = false
			})
	})

	// Editor state
	let editingId = $state<string | null>(null)
	let isAddingNew = $state(false)

	// Form state with defaults
	const DEFAULT_TIME = '09:00'
	const DEFAULT_FREQUENCY: Frequency = 'daily'

	let editTime = $state(DEFAULT_TIME)
	let editFrequency = $state<Frequency>(DEFAULT_FREQUENCY)
	let editCronExpression = $state('')
	let editSelection = $state<ModelSelection>(null)
	let editPrompt = $state('')

	// Reset form to defaults
	function resetForm() {
		editTime = DEFAULT_TIME
		editFrequency = DEFAULT_FREQUENCY
		editCronExpression = ''
		editSelection = null
		editPrompt = ''
	}

	// Start adding new job
	function startAdd() {
		editingId = null
		isAddingNew = true
		resetForm()
	}

	// Start editing existing job
	function startEdit(job: CronJob) {
		editingId = job.id
		isAddingNew = false
		editTime = job.time
		editFrequency = job.frequency
		editCronExpression = job.cronExpression ?? ''
		editSelection = job.model ?? null
		editPrompt = job.prompt
	}

	// Cancel editing
	function cancelEdit() {
		editingId = null
		isAddingNew = false
		resetForm()
	}

	// Build cron job from current form state
	function buildCronJob(): CronJob {
		return {
			id: editingId ?? '',
			time: editTime,
			frequency: editFrequency,
			cronExpression:
				editFrequency === 'custom' ? editCronExpression.trim() : undefined,
			model: editSelection ?? undefined,
			prompt: editPrompt.trim()
		}
	}

	// Save the job straight to its endpoint; the cache updates from
	// the stored row the call returns.
	async function saveJob() {
		if (!agentId || busy) return
		const job = buildCronJob()
		busy = true
		actionError = null
		try {
			if (isAddingNew) {
				await createCachedCronJob(agentId, job)
			} else {
				await updateCachedCronJob(agentId, job)
			}
			cancelEdit()
		} catch (error) {
			actionError = error instanceof Error ? error.message : 'Failed to save'
		} finally {
			busy = false
		}
	}

	// Delete straight to the endpoint
	async function handleDelete(id: string) {
		if (!agentId || busy) return
		busy = true
		actionError = null
		try {
			await deleteCachedCronJob(agentId, id)
		} catch (error) {
			actionError = error instanceof Error ? error.message : 'Failed to delete'
		} finally {
			busy = false
		}
	}

	// Derived state
	let canSave = $derived(
		!busy &&
			editPrompt.trim().length > 0 &&
			(editFrequency !== 'custom' || editCronExpression.trim().length > 0)
	)
	let isEditing = $derived(isAddingNew || editingId !== null)
</script>

<div class="space-y-3">
	{#if agentId === undefined}
		<p class="text-muted-foreground text-sm">
			Create Agent first and then edit from Agent Settings.
		</p>
	{:else if isLoading}
		<p class="text-muted-foreground text-sm">Loading scheduled prompts…</p>
	{:else if loadError}
		<p class="text-destructive text-sm">{loadError}</p>
	{:else if !isEditing}
		<p class="text-muted-foreground text-sm">
			Add scheduled prompts that will be sent automatically at specified times.
			Each prompt saves on its own.
		</p>
		{#if actionError}
			<p class="text-destructive text-sm">{actionError}</p>
		{/if}
		<!-- List of existing jobs -->
		{#if jobs.length === 0}
			<p class="text-muted-foreground text-sm">
				No scheduled prompts yet. Add one to get started.
			</p>
		{:else}
			<div class="space-y-2">
				{#each jobs as job (job.id)}
					<div
						class="bg-muted/30 flex items-center gap-3 rounded-lg border p-3 text-sm"
					>
						<span class="text-muted-foreground font-mono text-xs"
							>{job.time}</span
						>
						<span
							class="bg-primary/10 text-primary rounded-full px-2 py-0.5 text-xs capitalize"
						>
							{job.frequency}
						</span>
						<span class="flex-1 truncate">{job.prompt}</span>
						<div class="flex gap-1">
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label="Edit"
								disabled={busy}
								onclick={() => startEdit(job)}
							>
								<Pencil class="size-4" />
							</Button>
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label="Delete"
								disabled={busy}
								onclick={() => handleDelete(job.id)}
							>
								<X class="size-4" />
							</Button>
						</div>
					</div>
				{/each}
			</div>
		{/if}

		<!-- Add button -->
		<Button variant="outline" size="sm" disabled={busy} onclick={startAdd}>
			<Plus class="mr-1 size-4" />
			Add Scheduled Prompt
		</Button>
	{:else}
		<!-- Inline editor -->
		<div class="bg-muted/20 space-y-3 rounded-lg border p-4">
			{#if actionError}
				<p class="text-destructive text-sm">{actionError}</p>
			{/if}
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label for="cron-time" class="mb-2 block text-sm font-medium"
						>Time</label
					>
					<Input
						id="cron-time"
						type="time"
						bind:value={editTime}
						class="font-mono"
					/>
				</div>
				<div>
					<label class="mb-2 block text-sm font-medium" for="cron-frequency">
						Frequency
					</label>
					<Select.Root type="single" bind:value={editFrequency}>
						<Select.Trigger id="cron-frequency" class="w-full">
							{editFrequency}
						</Select.Trigger>
						<Select.Content>
							{#each ['daily', 'weekly', 'custom'] as freq (freq)}
								<Select.Item value={freq} label={freq} />
							{/each}
						</Select.Content>
					</Select.Root>
				</div>
			</div>

			<div>
				<label class="mb-2 block text-sm font-medium" for="cron-model">
					Model
				</label>
				<ModelSelector bind:value={editSelection} />
			</div>

			{#if editFrequency === 'custom'}
				<div>
					<label for="cron-expression" class="mb-2 block text-sm font-medium">
						Cron Expression
					</label>
					<Input
						id="cron-expression"
						type="text"
						placeholder="0 9 * * 1"
						bind:value={editCronExpression}
						class="font-mono"
					/>
				</div>
			{/if}

			<div>
				<label for="cron-prompt" class="mb-2 block text-sm font-medium">
					Prompt
				</label>
				<Textarea
					id="cron-prompt"
					bind:value={editPrompt}
					placeholder="What should the agent do?"
				/>
			</div>

			<div class="flex justify-end gap-2">
				<Button
					variant="outline"
					size="sm"
					onclick={cancelEdit}
					disabled={busy}
				>
					Cancel
				</Button>
				<Button size="sm" onclick={saveJob} disabled={!canSave}>Save</Button>
			</div>
		</div>
	{/if}
</div>
