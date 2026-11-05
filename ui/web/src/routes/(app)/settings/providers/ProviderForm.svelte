<script lang="ts">
	import { Button } from '#components/ui/button'
	import { Input } from '#components/ui/input'
	import { Label } from '#components/ui/label'
	import {
		createProvider,
		deleteProvider,
		updateProvider
	} from '#lib/services/providers'
	import type { Provider } from '#lib/types/provider'

	let {
		provider,
		onsaved,
		oncancel
	}: {
		/** Undefined = creating a new provider. */
		provider?: Provider
		onsaved: () => void
		oncancel: () => void
	} = $props()

	let name = $state(provider?.name ?? '')
	let baseUrl = $state(provider?.baseUrl ?? '')
	// Blank key keeps the stored one — the server never echoes keys,
	// only hasKey.
	let apiKey = $state('')
	// One box per model: the model id is all a model is.
	let models = $state<string[]>([...(provider?.models ?? [])])
	let error = $state<string | null>(null)
	let saving = $state(false)

	function addModel(): void {
		models.push('')
	}

	function removeModel(index: number): void {
		models.splice(index, 1)
	}

	async function save(): Promise<void> {
		error = null
		saving = true
		try {
			if (provider) {
				await updateProvider(provider.id, {
					name,
					baseUrl,
					apiKey: apiKey === '' ? undefined : apiKey,
					models
				})
			} else {
				// Create carries the key as-is: '' = keyless.
				await createProvider({ name, baseUrl, apiKey })
			}
			onsaved()
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save provider'
		} finally {
			saving = false
		}
	}

	async function remove(): Promise<void> {
		if (!provider) return
		error = null
		saving = true
		try {
			await deleteProvider(provider.id)
			onsaved()
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to delete provider'
			saving = false
		}
	}
</script>

<form
	class="bg-card space-y-4 rounded-lg border p-4"
	onsubmit={(e) => {
		e.preventDefault()
		void save()
	}}
>
	<div class="grid gap-3 sm:grid-cols-2">
		<div class="space-y-1.5">
			<Label for="{provider?.id ?? 'new'}-name">Name</Label>
			<Input
				id="{provider?.id ?? 'new'}-name"
				bind:value={name}
				placeholder="OpenRouter"
				required
			/>
		</div>
		<div class="space-y-1.5">
			<Label for="{provider?.id ?? 'new'}-url">Base URL</Label>
			<Input
				id="{provider?.id ?? 'new'}-url"
				bind:value={baseUrl}
				placeholder="https://openrouter.ai/api/v1"
				required
			/>
		</div>
		<div class="space-y-1.5 sm:col-span-2">
			<Label for="{provider?.id ?? 'new'}-key">API key</Label>
			<Input
				id="{provider?.id ?? 'new'}-key"
				type="password"
				bind:value={apiKey}
				placeholder={provider?.hasKey
					? 'Leave empty to keep the saved key'
					: 'Paste API key (optional)'}
			/>
		</div>
	</div>

	<div class="space-y-2">
		<div class="flex items-center justify-between border-t pt-3">
			<h3 class="text-sm font-medium">Models</h3>
			{#if provider}
				<Button size="sm" variant="outline" type="button" onclick={addModel}>
					Add model
				</Button>
			{/if}
		</div>
		{#if !provider}
			<p class="text-muted-foreground text-sm">
				Save the provider first, then add its models.
			</p>
		{:else if models.length === 0}
			<p class="text-muted-foreground text-sm">
				No models — agents can only pick this provider once it has models.
			</p>
		{/if}
		{#each models as model, index (index)}
			<div class="flex items-center gap-2">
				<div class="flex-1">
					<Input
						bind:value={models[index]}
						placeholder="provider/model, e.g. openai/gpt-4o"
						aria-label="Model"
						required
					/>
				</div>
				<Button
					size="icon"
					variant="ghost"
					type="button"
					aria-label="Remove model"
					onclick={() => removeModel(index)}
				>
					×
				</Button>
			</div>
		{/each}
	</div>

	{#if error}
		<p class="text-destructive text-sm">{error}</p>
	{/if}

	<div class="flex items-center justify-end gap-2 border-t pt-3">
		{#if provider}
			<Button
				variant="destructive"
				type="button"
				disabled={saving}
				onclick={() => void remove()}
			>
				Delete
			</Button>
		{/if}
		<Button variant="ghost" type="button" onclick={oncancel}>Cancel</Button>
		<Button type="submit" disabled={saving}>
			{saving ? 'Saving…' : 'Save'}
		</Button>
	</div>
</form>
