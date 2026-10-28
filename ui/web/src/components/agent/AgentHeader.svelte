<script lang="ts">
	import { Settings } from '@lucide/svelte'
	import AgentIcon from '#components/agent-icons/index.svelte'
	import { AgentConfigSheet } from '#components/agentconfig'
	import { Button, buttonVariants } from '#components/ui/button'
	import * as Tooltip from '#components/ui/tooltip'
	import { SUPERAGENT_AGENT_ID } from '#lib/config'
	import { getAgentConfiguration } from '#lib/services/agent'
	import type { Agent, AgentConfiguration } from '#lib/types/agent'
	import type { Tool } from '#lib/types/tool'
	import { invalidate } from '$app/navigation'

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
				configuration = await getAgentConfiguration(agent.id)
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
		<AgentIcon name={agent.icon} size={16} />
		<span>{agent.label}</span>
	</div>
	<div class="flex items-center gap-6">
		<div class="flex items-center gap-2">
			{#if agent.id === SUPERAGENT_AGENT_ID && activeTab && onTabChange}
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
		agentId={agent.id}
		onSaved={(config) => {
			configuration = config
			// Refresh the agents list so renames/icon changes reach the sidebar.
			void invalidate('app:agents')
		}}
	/>
{/if}
