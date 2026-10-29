<script lang="ts">
	/**
	 * Browser-level protection: shows the native browser dialog on
	 * refresh/close via the beforeunload event — not our custom dialog.
	 *
	 * Level 3 of unsaved-changes protection. Suppressed while the owning
	 * surface performs an intentional leave (bypassing).
	 */
	let {
		hasChanges,
		bypassing
	}: {
		hasChanges: boolean
		bypassing: boolean
	} = $props()

	function handleBeforeUnload(event: BeforeUnloadEvent) {
		if (hasChanges && !bypassing) {
			event.preventDefault()
		}
	}
</script>

<svelte:window on:beforeunload={handleBeforeUnload} />
