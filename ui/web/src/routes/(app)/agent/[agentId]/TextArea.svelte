<script lang="ts">
	import { ArrowUp, CircleStop, Plus } from '@lucide/svelte'
	import { Button } from '#components/ui/button'
	import { Textarea } from '#components/ui/textarea'

	let {
		onsubmit,
		oninterrupt,
		working = false
	}: {
		/** Resolves when the gateway accepts the prompt; a rejection
		 * (or a lost connection) keeps the draft in place. */
		onsubmit?: (text: string) => Promise<void>
		oninterrupt?: () => void
		working?: boolean
	} = $props()

	let input = $state('')
	let submitting = $state(false)
	let submitError = $state<string | null>(null)
	let isSendButtonDisabled = $derived(input.trim() === '' || submitting)

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey) {
			handleSubmit(event)
		}
	}

	async function handleSubmit(e: KeyboardEvent | MouseEvent) {
		e.preventDefault()
		const text = input.trim()
		if (!text || submitting) {
			return
		}
		submitting = true
		submitError = null
		try {
			await onsubmit?.(text)
			// Accepted: only now does the draft leave the composer —
			// whatever the user typed while waiting stays theirs.
			if (input.trim() === text) {
				input = ''
			}
		} catch (err) {
			submitError = err instanceof Error ? err.message : 'submission failed'
		} finally {
			submitting = false
		}
	}

	function handleStop(e: MouseEvent) {
		e.preventDefault()
		oninterrupt?.()
	}

	function handleAttach() {
		// TODO: Implement file attachment (images, documents)
		console.log('Attach file')
	}
</script>

<!-- Horizontal layout: + button | textarea | stop + send buttons -->
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
	{#if working}
		<Button
			size="icon"
			variant="ghost"
			aria-label="Stop"
			class="mb-[2.5px] size-9 shrink-0 cursor-pointer rounded-full"
			onclick={handleStop}
		>
			<CircleStop
				class="[&_circle]:fill-destructive [&_circle]:stroke-destructive [&_rect]:fill-primary-foreground [&_rect]:stroke-primary-foreground size-9 animate-pulse"
			/>
		</Button>
	{:else}
		<Button
			size="icon"
			aria-label={submitting ? 'Sending' : 'Send'}
			class="mb-[2.5px] size-9 shrink-0 cursor-pointer rounded-full"
			disabled={isSendButtonDisabled}
			onclick={handleSubmit}
		>
			<ArrowUp class="size-5" />
		</Button>
	{/if}
</div>
{#if submitError}
	<p class="text-destructive mx-auto mt-1 w-fit text-xs">
		{submitError}
	</p>
{/if}
