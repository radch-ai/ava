<script lang="ts">
	import * as AlertDialog from '$components/ui/alert-dialog';

	interface Props {
		open: boolean;
		onDiscard: () => void;
		onKeepEditing: () => void;
	}

	let { open = $bindable(), onDiscard, onKeepEditing }: Props = $props();
</script>

<AlertDialog.Root bind:open>
	<AlertDialog.Portal>
		<AlertDialog.Overlay />
		<AlertDialog.Content
			onInteractOutside={(e) => e.preventDefault()}
			onEscapeKeydown={(e) => e.preventDefault()}
		>
			<AlertDialog.Header>
				<AlertDialog.Title>Discard unsaved changes?</AlertDialog.Title>
				<AlertDialog.Description>
					You have unsaved changes. If you discard them, your changes will be lost.
				</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel onclick={onKeepEditing}>Keep Editing</AlertDialog.Cancel>
				<AlertDialog.Action onclick={onDiscard}>Discard</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Portal>
</AlertDialog.Root>
