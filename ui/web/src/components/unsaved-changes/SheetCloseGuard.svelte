<script module lang="ts">
	/**
	 * Handlers Sheet.Content wires to its escape/outside-close hooks —
	 * named so the sheet types its ref instead of deriving from the
	 * component function.
	 */
	export interface SheetCloseGuardExports {
		onEscapeKeydown: (event: KeyboardEvent) => void
		onInteractOutside: (event: PointerEvent) => void
	}
</script>

<script lang="ts">
	import UnsavedChangesDialog from './UnsavedChangesDialog.svelte'

	/**
	 * Sheet-level protection — the fourth level of unsaved-changes
	 * protection, mounted only by sheet surfaces (never a full-page
	 * editor like /agent/new): Escape and click-outside are prevented
	 * while there are unsaved changes and show the confirmation dialog
	 * instead.
	 *
	 * The sheet's own open/close handler (Sheet.Root onOpenChange) still
	 * refuses to close while dirty as a backstop.
	 */
	let {
		hasChanges,
		onDiscard
	}: {
		hasChanges: boolean
		/** Close the sheet after the user confirms discarding. */
		onDiscard: () => void
	} = $props()

	let showCloseDialog = $state(false)

	export function onEscapeKeydown(event: KeyboardEvent) {
		if (hasChanges) {
			event.preventDefault()
			showCloseDialog = true
		}
	}

	export function onInteractOutside(event: PointerEvent) {
		if (hasChanges) {
			event.preventDefault()
			showCloseDialog = true
		}
	}

	function handleKeepEditing() {
		showCloseDialog = false
	}

	function handleDiscard() {
		showCloseDialog = false
		onDiscard()
	}
</script>

<UnsavedChangesDialog
	bind:open={showCloseDialog}
	onDiscard={handleDiscard}
	onKeepEditing={handleKeepEditing}
/>
