<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { resolve } from '$app/paths';
	import { ArrowLeftToLine, ArrowRightFromLine, LogOut, Plus } from 'lucide-svelte';
	import * as Sidebar from '$components/ui/sidebar';
	import { Button } from '$components/ui/button';
	import * as Tooltip from '$components/ui/tooltip';
	import { Icon } from '$lib/icons';
	import { APP_NAME } from '$lib/config';
	import { SUPERAGENT_AGENT } from '$lib/types/agent';
	import { authStore } from '$lib/auth';
	import { agents } from '$lib/user';

	const sidebar = Sidebar.useSidebar();

	// Persist sidebar state to localStorage (only store when collapsed)
	const SIDEBAR_STATE_KEY = `${APP_NAME.toLowerCase()}-sidebar-state`;

	function saveOrDeleteSidebarState(): void {
		if (typeof window === 'undefined') return;
		if (sidebar.open) {
			localStorage.removeItem(SIDEBAR_STATE_KEY);
		} else {
			localStorage.setItem(SIDEBAR_STATE_KEY, 'collapsed');
		}
	}

	// Initialize sidebar state from localStorage
	onMount(() => {
		const stored = localStorage.getItem(SIDEBAR_STATE_KEY);
		if (stored === 'collapsed' && sidebar.open) {
			sidebar.toggle();
		}
	});
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
					class="cursor-pointer rounded-md p-2.5 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
					onclick={() => {
						sidebar.toggle();
						saveOrDeleteSidebarState();
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
						isActive={$page.url.pathname === '/'}
						class="group-data-[collapsible=icon]:justify-center"
					>
						{#snippet child({ props })}
							<a href={resolve('/')} {...props}>
								<Icon
									name={SUPERAGENT_AGENT.icon}
									size={18}
									class="group-data-[collapsible=icon]:size-5"
								/>
								<span class="font-medium group-data-[collapsible=icon]:hidden"
									>{SUPERAGENT_AGENT.label}</span
								>
							</a>
						{/snippet}
					</Sidebar.MenuButton>
				</Sidebar.MenuItem>
			</Sidebar.Menu>
		</Sidebar.Group>
		<Sidebar.Separator />
		<Sidebar.Group>
			<Sidebar.GroupLabel>Worker Agents</Sidebar.GroupLabel>
			<Sidebar.GroupContent>
				<Sidebar.Menu>
					{#each agents as agent (agent.name)}
						<Sidebar.MenuItem>
							<Sidebar.MenuButton isActive={$page.url.pathname === `/agent/${agent.name}`}>
								{#snippet child({ props })}
									<a href={resolve(`/agent/${agent.name}`)} {...props}>
										<Icon
											name={agent.icon}
											size={18}
											class="group-data-[collapsible=icon]:size-5"
										/>
										<span class="group-data-[collapsible=icon]:hidden">{agent.label}</span>
									</a>
								{/snippet}
							</Sidebar.MenuButton>
						</Sidebar.MenuItem>
					{/each}
					<!-- New Agent Button -->
					<Sidebar.MenuItem class="mt-2">
						<Button
							size="sm"
							href={resolve('/agent/new')}
							class="w-full justify-start gap-2 group-data-[collapsible=icon]:justify-center {$page
								.url.pathname === '/agent/new'
								? ''
								: 'opacity-50 hover:opacity-100'}"
						>
							<Plus class="size-4" />
							<span class="group-data-[collapsible=icon]:hidden">New Agent</span>
						</Button>
					</Sidebar.MenuItem>
				</Sidebar.Menu>
			</Sidebar.GroupContent>
		</Sidebar.Group>
	</Sidebar.Content>
	<Sidebar.Separator />
	<Sidebar.Footer>
		<button
			type="button"
			aria-label="Logout"
			onclick={() => void authStore.logout()}
			class="flex w-full cursor-pointer items-center gap-2 rounded-lg p-2 text-sm group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-1.5 hover:bg-sidebar-accent hover:!ring-1 hover:!ring-ring"
		>
			<LogOut class="size-4 shrink-0" />
			<span class="font-semibold group-data-[collapsible=icon]:hidden">Logout</span>
		</button>
	</Sidebar.Footer>
</Sidebar.Root>
