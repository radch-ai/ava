<script lang="ts">
	import { ArrowUp, Plus } from '@lucide/svelte'
	import { Button } from '#components/ui/button'
	import { Textarea } from '#components/ui/textarea'

	let { onsubmit }: { onsubmit?: (text: string) => void } = $props()

	let input = $state('')
	let isSendButtonDisabled = $derived(input.trim() === '')

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey) {
			handleSubmit(event)
		}
	}

	function handleSubmit(e: KeyboardEvent | MouseEvent) {
		e.preventDefault()
		if (input.trim()) {
			onsubmit?.(input.trim())
			input = ''
		}
	}

	function handleAttach() {
		// TODO: Implement file attachment (images, documents)
		console.log('Attach file')
	}
</script>

<!-- Horizontal layout: + button | textarea | send button -->
<div class="bg-overlay-dark/70 flex items-end rounded-3xl transition-colors">
	<Button
		size="icon"
		variant="ghost"
		class="mb-[2.5px] size-9 shrink-0 cursor-pointer rounded-full"
		onclick={handleAttach}
	>
		<Plus class="size-5" />
	</Button>
	<Textarea
		bind:value={input}
		onkeydown={handleKeydown}
		placeholder="Enter your query!"
		class="max-h-[200px] min-h-[40px] flex-1 resize-none !border-0 !bg-transparent px-2 py-[10px] !text-lg !leading-5 focus-visible:!border-transparent focus-visible:!ring-0"
		rows={1}
	/>
	<Button
		size="icon"
		class="mb-[2.5px] size-9 shrink-0 cursor-pointer rounded-full"
		disabled={isSendButtonDisabled}
		onclick={handleSubmit}
	>
		<ArrowUp class="size-5" />
	</Button>
</div>
