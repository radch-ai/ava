<script lang="ts">
	import { Settings } from 'lucide-svelte'
	import { AgentConfigSheet } from '$components/agentconfig'
	import { Button, buttonVariants } from '$components/ui/button'
	import * as Tooltip from '$components/ui/tooltip'
	import { SUPERAGENT_NAME } from '$lib/config'
	import { Icon } from '$lib/icons'
	import { getAgentConfiguration } from '$lib/services/agent'
	import type { Agent, AgentConfiguration } from '$lib/types/agent'
	import type { Tool } from '$lib/types/tool'

	let {
		agent,
		activeTab,
		onTabChange
	}: {
		agent: Agent
		activeTab?: 'conversation' | 'activity'
		onTabChange?: (tab: 'conversation' | 'activity') => void
	} = $props()

	let configSheetOpen = $state(false)
	let configuration = $state<AgentConfiguration>()
	let isLoading = $state(false)

	function handleTabChange(tab: 'conversation' | 'activity') {
		// Only call onTabChange if switching to a different tab (idempotency)
		if (activeTab !== tab && onTabChange) {
			onTabChange(tab)
		}
	}

	// TODO: Fetch available tools from API
	const availableTools: Tool[] = []

	async function openSettings() {
		if (!configuration && !isLoading) {
			isLoading = true
			try {
				configuration = await getAgentConfiguration(agent.name)
			} catch {
				// API unreachable - ky logs to console
			} finally {
				isLoading = false
			}
		}
		if (configuration) {
			configSheetOpen = true
		}
	}
</script>

<header class="flex w-full items-center justify-between p-2">
	<div class="flex items-center gap-2 text-sm">
		<Icon name={agent.icon} size={16} />
		<span>{agent.label}</span>
	</div>
	<div class="flex items-center gap-6">
		<div class="flex items-center gap-2">
			{#if agent.name === SUPERAGENT_NAME.toLowerCase() &&
				activeTab &&
				onTabChange}
				<!-- Tabs for superagent -->
				<div role="tablist" class="ml-4 flex gap-4">
					{#each ['conversation', 'activity'] as tab (`tab-${tab}`)}
						<Button
							variant="link"
							size="sm"
							role="tab"
							tabindex={activeTab === tab ? 0 : -1}
							aria-selected={activeTab === tab}
							aria-controls="{tab}-panel"
							disabled={activeTab === tab}
							class={activeTab === tab
								? 'disabled:text-foreground disabled:opacity-100'
								: 'text-muted-foreground hover:text-primary cursor-pointer'}
							onclick={() =>
								handleTabChange(tab as 'conversation' | 'activity')}
						>
							{tab.charAt(0).toUpperCase() + tab.slice(1)}
						</Button>
					{/each}
				</div>
			{/if}
		</div>
		<Tooltip.Root>
			<Tooltip.Trigger
				class={`${buttonVariants({ variant: 'ghost', size: 'icon-sm' })} cursor-pointer`}
				onclick={openSettings}
				disabled={isLoading}
				aria-label="Open agent configuration"
			>
				<Settings />
			</Tooltip.Trigger>
			<Tooltip.Content>Agent Config</Tooltip.Content>
		</Tooltip.Root>
	</div>
</header>

{#if configuration}
	<AgentConfigSheet
		bind:isSheetOpened={configSheetOpen}
		closeSheet={() => (configSheetOpen = false)}
		{configuration}
		{availableTools}
		agentName={agent.name}
		onSaved={(config) => (configuration = config)}
	/>
{/if}
