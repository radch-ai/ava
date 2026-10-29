<script lang="ts">
	import { type BeforeNavigate, beforeNavigate, goto } from '$app/navigation'
	import { resolve } from '$app/paths'
	import type { PathnameWithSearchOrHash } from '$app/types'
	import UnsavedChangesDialog from './UnsavedChangesDialog.svelte'

	/**
	 * SvelteKit-level protection: intercepts internal app navigation
	 * (clicking links, back/forward) with beforeNavigate, cancels it and
	 * shows our confirmation dialog. Discard runs the surface's cleanup
	 * and resumes the cancelled navigation; keep editing stays put.
	 *
	 * 'leave' type = browser refresh/close: that unloads the document, so
	 * it belongs to BrowserGuard's native beforeunload dialog, not here.
	 *
	 * Level 2 of unsaved-changes protection. Suppressed while the owning
	 * surface performs an intentional leave (bypassing), and while this
	 * guard resumes the discarded navigation itself (isResuming) — the
	 * edit is still dirty at that moment, so it must not re-intercept.
	 */
	let {
		hasChanges,
		bypassing,
		onDiscard
	}: {
		hasChanges: boolean
		bypassing: boolean
		/** Surface cleanup before the cancelled navigation resumes (e.g. close the sheet). */
		onDiscard?: () => void
	} = $props()

	let showDialog = $state(false)
	let isResuming = $state(false)
	let pending: BeforeNavigate | null = $state(null)

	beforeNavigate((navigation) => {
		if (
			hasChanges &&
			!bypassing &&
			!isResuming &&
			navigation.type !== 'leave'
		) {
			navigation.cancel()
			pending = navigation
			showDialog = true
		}
	})

	function handleKeepEditing() {
		showDialog = false
		pending = null
	}

	async function handleDiscard() {
		showDialog = false
		const navigation = pending
		pending = null
		onDiscard?.()
		if (!navigation?.to?.url) return
		isResuming = true
		try {
			await goto(
				resolve(
					(navigation.to.url.pathname +
						navigation.to.url.search) as PathnameWithSearchOrHash
				)
			)
		} finally {
			isResuming = false
		}
	}
</script>

<UnsavedChangesDialog
	bind:open={showDialog}
	onDiscard={handleDiscard}
	onKeepEditing={handleKeepEditing}
/>
