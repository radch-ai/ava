<script module lang="ts">
	/**
	 * bind:this surface of the guards — named so consumers type refs to it
	 * instead of deriving them from the component function.
	 */
	export interface UnsavedChangesGuardsExports {
		/** Run a programmatic leave (confirmed cancel, save-and-goto) without the guards intercepting it. */
		bypassNavigation: (action: () => void | Promise<void>) => Promise<void>
	}
</script>

<script lang="ts">
	import BrowserGuard from './BrowserGuard.svelte'
	import SpaNavGuard from './SpaNavGuard.svelte'

	/**
	 * Base of unsaved-changes protection for any editing surface: mounts
	 * the two route-level guards and owns the shared bypass flag so
	 * intentional leaves suppress both at once.
	 *
	 * The full picture across all levels:
	 *
	 * 1. CANCEL BUTTON: the editor component owns this (e.g.
	 *    AgentConfigEditor shows UnsavedChangesDialog when there are
	 *    changes).
	 * 2. INTERNAL NAVIGATION (clicking links, back/forward): SpaNavGuard
	 *    intercepts with beforeNavigate and shows our dialog.
	 * 3. BROWSER REFRESH/CLOSE: BrowserGuard uses the browser's native
	 *    dialog via the beforeunload event (not our custom dialog).
	 *
	 * Sheet surfaces add a fourth level (SheetCloseGuard): escape and
	 * click-outside are prevented while dirty and show our dialog.
	 */
	let {
		hasChanges,
		onDiscard
	}: {
		hasChanges: boolean
		onDiscard?: () => void
	} = $props()

	let isBypassing = $state(false)

	export async function bypassNavigation(
		action: () => void | Promise<void>
	): Promise<void> {
		isBypassing = true
		try {
			await action()
		} finally {
			isBypassing = false
		}
	}
</script>

<BrowserGuard {hasChanges} bypassing={isBypassing} />
<SpaNavGuard {hasChanges} bypassing={isBypassing} {onDiscard} />
