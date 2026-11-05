<script lang="ts">
	import { KeyRound, Pencil } from '@lucide/svelte'
	import { Button } from '#components/ui/button'
	import { modelsStore } from '#lib/models'
	import { invalidateAll } from '$app/navigation'
	import type { PageData } from './$types'
	import ProviderForm from './ProviderForm.svelte'

	let { data }: { data: PageData } = $props()

	// One form open at a time: null = list only, 'new' = blank form,
	// otherwise the provider id being edited.
	let editing = $state<'new' | string | null>(null)

	// Every mutation ends with invalidateAll: the load refetches the
	// catalog, rows re-derive from fresh data, and the selector store
	// picks up the new list.
	function onsaved(): void {
		editing = null
		void modelsStore.refresh()
		void invalidateAll()
	}
</script>

<div class="mx-auto max-w-[var(--content-max-width)] space-y-6 px-6 py-8">
	<header class="flex items-center justify-between gap-4 border-b pb-4">
		<p class="text-muted-foreground text-sm">
			Providers are the connections your agents pick models from.
		</p>
		{#if editing !== 'new'}
			<Button
				onclick={() => {
					editing = 'new'
				}}
			>
				Add provider
			</Button>
		{/if}
	</header>

	{#if data.providersError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">
			{data.providersError}
		</p>
	{/if}

	{#if editing === 'new'}
		<ProviderForm {onsaved} oncancel={() => (editing = null)} />
	{/if}

	<section class="divide-y rounded-lg border">
		{#each data.providers as provider (provider.id)}
			{#if editing === provider.id}
				<ProviderForm {provider} {onsaved} oncancel={() => (editing = null)} />
			{:else}
				<div
					class="hover:bg-muted/40 flex items-center justify-between gap-4 p-4 transition-colors"
				>
					<div class="min-w-0 flex-1 space-y-1.5">
						<div class="flex items-center gap-2">
							<h3 class="font-semibold">{provider.name}</h3>
							<span
								class="flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium
									{provider.hasKey
									? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'
									: 'bg-muted text-muted-foreground'}"
							>
								{#if provider.hasKey}
									<KeyRound class="size-3" />
								{/if}
								{provider.hasKey ? 'Key set' : 'No key'}
							</span>
						</div>
						<p class="text-muted-foreground truncate text-sm">
							{provider.baseUrl}
						</p>
						{#if provider.models.length > 0}
							<div class="flex flex-wrap gap-1.5 pt-0.5">
								{#each provider.models as model (model)}
									<span
										class="bg-muted text-muted-foreground rounded-full px-2 py-0.5 font-mono text-xs"
									>
										{model}
									</span>
								{/each}
							</div>
						{:else}
							<p class="text-muted-foreground text-xs">No models yet</p>
						{/if}
					</div>
					<Button
						size="sm"
						variant="outline"
						onclick={() => {
							editing = provider.id
						}}
					>
						<Pencil class="size-3.5" />
						Edit
					</Button>
				</div>
			{/if}
		{:else}
			<p class="text-muted-foreground p-4 text-sm">
				No providers yet — add one above.
			</p>
		{/each}
	</section>
</div>
