<script lang="ts">
	import {
		ArrowLeftToLine,
		ArrowRightFromLine,
		LogOut,
		Plus
	} from '@lucide/svelte'
	import { onMount } from 'svelte'
	import AgentIcon from '#components/agent-icons/index.svelte'
	import { Button } from '#components/ui/button'
	import * as Sidebar from '#components/ui/sidebar'
	import * as Tooltip from '#components/ui/tooltip'
	import { authStore } from '#lib/auth'
	import { APP_NAME } from '#lib/config'
	import { type Agent, SUPERAGENT_AGENT } from '#lib/types/agent'
	import { resolve } from '$app/paths'
	import { page } from '$app/state'

	// The list only carries user-created agents; the superagent entry
	// below comes from the client constant.
	let { agents }: { agents: Agent[] } = $props()

	const sidebar = Sidebar.useSidebar()

	// Persist sidebar state to localStorage (only store when collapsed)
	const SIDEBAR_STATE_KEY = `${APP_NAME.toLowerCase()}-sidebar-state`

	function saveOrDeleteSidebarState(): void {
		if (typeof window === 'undefined') return
		if (sidebar.open) {
			localStorage.removeItem(SIDEBAR_STATE_KEY)
		} else {
			localStorage.setItem(SIDEBAR_STATE_KEY, 'collapsed')
		}
	}

	// Initialize sidebar state from localStorage
	onMount(() => {
		const stored = localStorage.getItem(SIDEBAR_STATE_KEY)
		if (stored === 'collapsed' && sidebar.open) {
			sidebar.toggle()
		}
	})
</script>

<Sidebar.Root collapsible="icon">
	<Sidebar.Header
		class="flex flex-row items-center px-4 py-4 {sidebar.open
			? 'justify-between'
			: 'justify-center'}"
	>
		{#if sidebar.open}
			<h1 class="text-xl font-bold">{APP_NAME}</h1>
		{/if}
		<Tooltip.Root>
			<Tooltip.Trigger>
				<button
					type="button"
					class="hover:bg-sidebar-accent hover:text-sidebar-accent-foreground cursor-pointer rounded-md p-2.5 transition-colors"
					onclick={() => {
						sidebar.toggle()
						saveOrDeleteSidebarState()
					}}
					aria-label={sidebar.open ? 'Collapse sidebar' : 'Expand sidebar'}
				>
					{#if sidebar.open}
						<ArrowLeftToLine class="size-5" />
					{:else}
						<ArrowRightFromLine class="size-5" />
					{/if}
				</button>
			</Tooltip.Trigger>
			<Tooltip.Content>
				{sidebar.open ? 'Collapse sidebar' : 'Expand sidebar'}
			</Tooltip.Content>
		</Tooltip.Root>
	</Sidebar.Header>
	<Sidebar.Content>
		<Sidebar.Separator />
		<Sidebar.Group>
			<Sidebar.Menu>
				<Sidebar.MenuItem>
					<Sidebar.MenuButton
						size="lg"
						isActive={page.route.id === '/(app)/agent/[agentId]' &&
							page.params.agentId === SUPERAGENT_AGENT.id}
						class="group-data-[collapsible=icon]:justify-center"
					>
						{#snippet child({ props })}
							<a
								href={resolve('/(app)/agent/[agentId]', {
									agentId: SUPERAGENT_AGENT.id
								})}
								{...props}
							>
								<AgentIcon
									name={SUPERAGENT_AGENT.icon}
									size={18}
									class="group-data-[collapsible=icon]:size-5"
								/>
								<span class="font-medium group-data-[collapsible=icon]:hidden"
									>Chat with {SUPERAGENT_AGENT.name}</span
								>
							</a>
						{/snippet}
					</Sidebar.MenuButton>
				</Sidebar.MenuItem>
			</Sidebar.Menu>
		</Sidebar.Group>
		<Sidebar.Separator />
		<Sidebar.Group>
			<Sidebar.GroupLabel>Specialized Agents</Sidebar.GroupLabel>
			<Sidebar.GroupContent>
				<Sidebar.Menu>
					{#each agents as agent (agent.id)}
						<Sidebar.MenuItem>
							<Sidebar.MenuButton
								isActive={page.route.id === '/(app)/agent/[agentId]' &&
									page.params.agentId === agent.id}
							>
								{#snippet child({ props })}
									<a
										href={resolve('/(app)/agent/[agentId]', {
											agentId: agent.id
										})}
										{...props}
									>
										<AgentIcon
											name={agent.icon}
											size={18}
											class="group-data-[collapsible=icon]:size-5"
										/>
										<span class="group-data-[collapsible=icon]:hidden"
											>{agent.name}</span
										>
									</a>
								{/snippet}
							</Sidebar.MenuButton>
						</Sidebar.MenuItem>
					{/each}
					<!-- New Agent Button -->
					<Sidebar.MenuItem class="mt-2">
						<Button
							size="sm"
							href={resolve('/(app)/agent/new')}
							class="w-full justify-start gap-2 group-data-[collapsible=icon]:justify-center {page
								.route.id === '/(app)/agent/new'
								? ''
								: 'opacity-50 hover:opacity-100'}"
						>
							<Plus class="size-4" />
							<span class="group-data-[collapsible=icon]:hidden"
								>New Agent</span
							>
						</Button>
					</Sidebar.MenuItem>
				</Sidebar.Menu>
			</Sidebar.GroupContent>
		</Sidebar.Group>
	</Sidebar.Content>
	<Sidebar.Separator />
	<Sidebar.Footer>
		<Sidebar.Menu>
			<Sidebar.MenuItem>
				<Sidebar.MenuButton
					size="sm"
					aria-label="Sign Out"
					class="cursor-pointer"
					onclick={() => void authStore.logout()}
				>
					<LogOut />
					<span class="font-medium group-data-[collapsible=icon]:hidden"
						>Sign Out</span
					>
				</Sidebar.MenuButton>
			</Sidebar.MenuItem>
		</Sidebar.Menu>
	</Sidebar.Footer>
</Sidebar.Root>
