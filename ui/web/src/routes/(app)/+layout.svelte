<script lang="ts">
	import LoaderCircle from '@lucide/svelte/icons/loader-circle'
	import type { Snippet } from 'svelte'
	import AppSidebar from '#components/AppSidebar.svelte'
	import { SidebarProvider, useSidebar } from '#components/ui/sidebar'
	import { authStore } from '#lib/auth'
	import type { LayoutData } from './$types'

	let { data, children }: { data: LayoutData; children: Snippet } = $props()

	// Reactive, not just at mount: a mid-session logout or 401 flips state and
	// must send the user back to the login surface.
	$effect(() => {
		if (!authStore.isAuthenticated) {
			authStore.login()
		}
	})
</script>

{#if !authStore.isAuthenticated}
	<div class="flex h-svh items-center justify-center">
		<div class="space-y-4 text-center">
			<LoaderCircle class="text-primary mx-auto h-8 w-8 animate-spin" />
			<p class="text-muted-foreground">Redirecting to login...</p>
		</div>
	</div>
{:else}
	<SidebarProvider>
		{@const sidebar = useSidebar()}
		<div class="flex h-svh w-full">
			<AppSidebar />
			<!-- Backdrop overlay for mobile - closes sidebar when clicked -->
			{#if sidebar.isMobile && sidebar.open}
				<style>
					.mobile-sidebar-open [data-slot="sidebar-gap"] {
						width: var(--sidebar-width-icon);
					}
				</style>
				<button
					type="button"
					class="fixed inset-0 z-[5] bg-black/50 backdrop-blur-sm transition-opacity"
					onclick={() => sidebar.setOpen(false)}
					aria-label="Close sidebar"
				></button>
			{/if}

			<main class="flex-1">
				{#if data.agentsError}
					<div class="mx-auto max-w-[var(--content-max-width)] px-6 pt-4">
						<p
							class="bg-destructive/10 text-destructive rounded-lg p-4 text-center"
						>
							{data.agentsError}
						</p>
					</div>
				{/if}
				{@render children()}
			</main>
		</div>
	</SidebarProvider>
{/if}
