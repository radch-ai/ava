<script lang="ts">
	import { Pencil, Plus, X } from 'lucide-svelte'
	import ModelSelector from '$components/ModelSelector.svelte'
	import { Button } from '$components/ui/button'
	import { Input } from '$components/ui/input'
	import * as Select from '$components/ui/select'
	import { Textarea } from '$components/ui/textarea'
	import { defaultModel } from '$lib/models'
	import type { CronJob } from '$lib/types/agent'

	type Frequency = CronJob['frequency']

	interface Props {
		jobs: CronJob[]
	}

	let { jobs = $bindable([]) }: Props = $props()

	// Editor state
	let editingId = $state<string | null>(null)
	let isAddingNew = $state(false)

	// Form state with defaults
	const DEFAULT_TIME = '09:00'
	const DEFAULT_FREQUENCY: Frequency = 'daily'

	let editTime = $state(DEFAULT_TIME)
	let editFrequency = $state<Frequency>(DEFAULT_FREQUENCY)
	let editCronExpression = $state('')
	let editModel = $state(defaultModel)
	let editPrompt = $state('')

	// Reset form to defaults
	function resetForm() {
		editTime = DEFAULT_TIME
		editFrequency = DEFAULT_FREQUENCY
		editCronExpression = ''
		editModel = defaultModel
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
		editModel = job.model || defaultModel
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
			id: editingId ?? crypto.randomUUID(),
			time: editTime,
			frequency: editFrequency,
			cronExpression:
				editFrequency === 'custom' ? editCronExpression.trim() : undefined,
			model: editModel,
			prompt: editPrompt.trim()
		}
	}

	// Save job
	function saveJob() {
		if (!editPrompt.trim()) return
		if (editFrequency === 'custom' && !editCronExpression.trim()) return

		const job = buildCronJob()
		if (isAddingNew) {
			jobs = [...jobs, job]
		} else {
			jobs = jobs.map((j) => (j.id === job.id ? job : j))
		}
		cancelEdit()
	}

	// Delete job
	function handleDelete(id: string) {
		jobs = jobs.filter((j) => j.id !== id)
	}

	// Derived state
	let canSave = $derived(
		editPrompt.trim().length > 0 &&
			(editFrequency !== 'custom' || editCronExpression.trim().length > 0)
	)
	let isEditing = $derived(isAddingNew || editingId !== null)
</script>

<div class="space-y-3">
	{#if !isEditing}
		<p class="text-muted-foreground text-sm">
			Add scheduled prompts that will be sent automatically at specified times.
		</p>
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
							class="bg-primary/10 text-primary rounded px-1.5 py-0.5 text-xs font-medium"
						>
							{job.frequency === 'custom'
								? (job.cronExpression ?? 'custom')
								: job.frequency}
						</span>
						<span class="flex-1 truncate">{job.prompt}</span>
						<div class="flex gap-1">
							<button
								type="button"
								class="hover:bg-accent/10 rounded p-1"
								onclick={() => startEdit(job)}
								aria-label="Edit"
							>
								<Pencil class="size-4" />
							</button>
							<button
								type="button"
								class="hover:bg-destructive/10 rounded p-1"
								onclick={() => handleDelete(job.id)}
								aria-label="Delete"
							>
								<X class="size-4" />
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}

		<!-- Add button -->
		<Button variant="outline" size="sm" onclick={startAdd}>
			<Plus class="mr-1 size-4" />
			Add Scheduled Prompt
		</Button>
	{:else}
		<!-- Inline editor -->
		<div class="bg-muted/20 space-y-3 rounded-lg border p-4">
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label for="cron-time" class="mb-1.5 block text-sm font-medium"
						>Time</label
					>
					<Input
						id="cron-time"
						type="time"
						value={editTime}
						oninput={(e) => (editTime = e.currentTarget.value)}
						class="font-mono"
						aria-label="Time"
					/>
				</div>
				<div>
					<label for="cron-frequency" class="mb-1.5 block text-sm font-medium"
						>Frequency</label
					>
					<Select.Root
						type="single"
						value={editFrequency}
						onValueChange={(v) => (editFrequency = v as typeof editFrequency)}
					>
						<Select.Trigger id="cron-frequency" class="w-full">
							{editFrequency.charAt(0).toUpperCase() + editFrequency.slice(1)}
						</Select.Trigger>
						<Select.Content>
							<Select.Item value="daily">Daily</Select.Item>
							<Select.Item value="weekly">Weekly</Select.Item>
							<Select.Item value="custom">Custom</Select.Item>
						</Select.Content>
					</Select.Root>
				</div>
			</div>

			<div>
				<label for="cron-model" class="mb-1.5 block text-sm font-medium"
					>Model</label
				>
				<ModelSelector bind:value={editModel} />
			</div>

			{#if editFrequency === 'custom'}
				<div>
					<label for="cron-expression" class="mb-1.5 block text-sm font-medium"
						>Cron Expression</label
					>
					<Input
						id="cron-expression"
						value={editCronExpression}
						oninput={(e) => (editCronExpression = e.currentTarget.value)}
						placeholder="* * * * *"
						class="font-mono"
						aria-label="Cron Expression"
					/>
					<p class="text-muted-foreground mt-1 text-xs">
						Format: minute hour day-of-month month day-of-week (e.g.,
						<code class="font-mono">0 9 * * 1-5</code>
						for weekdays at 9am)
					</p>
				</div>
			{/if}

			<div>
				<label for="cron-prompt" class="mb-1.5 block text-sm font-medium"
					>Prompt</label
				>
				<Textarea
					id="cron-prompt"
					value={editPrompt}
					oninput={(e) => (editPrompt = e.currentTarget.value)}
					placeholder="What should I remind you about?"
					rows={3}
					class="resize-none"
					aria-label="Prompt"
				/>
			</div>

			<div class="flex justify-end gap-2">
				<Button variant="outline" size="sm" onclick={cancelEdit}>Cancel</Button>
				<Button size="sm" onclick={saveJob} disabled={!canSave}>Save</Button>
			</div>
		</div>
	{/if}
</div>
