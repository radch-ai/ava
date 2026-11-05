<script lang="ts">
	import { Pencil } from '@lucide/svelte'
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
	<header class="flex items-start justify-between gap-4 border-b pb-4">
		<p class="text-muted-foreground max-w-2xl text-sm">
			You can add <strong>API keys</strong> for services like
			<strong>OpenAI, Anthropic, Gemini</strong>
			and other providers. Provide the <strong>base URL</strong> for the API and
			<strong>specify the models</strong>
			available for use. Agents pick from this catalog when choosing a model.
		</p>
		{#if editing !== 'new'}
			<Button
				class="h-fit"
				onclick={() => {
					editing = 'new'
				}}
			>
				Add New API / Models
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

	<section>
		{#each data.providers as provider (provider.id)}
			{#if editing === provider.id}
				<ProviderForm {provider} {onsaved} oncancel={() => (editing = null)} />
			{:else}
				<div class="flex items-start justify-between gap-4 border-b p-2">
					<div class="flex-1">
						<h3 class="font-semibold">{provider.name}</h3>
						<p class="text-muted-foreground text-sm">
							Base URL: {provider.baseUrl}
							{provider.hasKey ? '' : '· no API key set'}
						</p>
						<h4 class="pt-1 text-sm font-semibold">Models:</h4>
						<ul>
							{#each provider.models as model (model)}
								<li class="text-muted-foreground text-sm">{model}</li>
							{:else}
								<li class="text-muted-foreground text-sm">None yet</li>
							{/each}
						</ul>
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
			<p class="text-muted-foreground py-4 text-sm">
				No providers yet — add one above.
			</p>
		{/each}
	</section>
</div>
