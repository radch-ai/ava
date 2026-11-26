<script lang="ts">
	import { ArrowUp, CircleStop } from '@lucide/svelte'
	import { Button } from '#components/ui/button'
	import * as Command from '#components/ui/command'
	import * as Popover from '#components/ui/popover'
	import { Textarea } from '#components/ui/textarea'

	/** Every completable command. Keep in step with the gateway's
	 * own classification. */
	const COMMANDS = [
		{ name: '/new', helper: 'Start a fresh conversation' },
		{ name: '/reset', helper: 'Alias of /new — same fresh session' },
		{ name: '/shake', helper: 'Elide old tool outputs from the model context' },
		{ name: '/compact', helper: 'Summarize this thread and continue fresh' }
	]

	let {
		onsubmit,
		oninterrupt,
		working = false,
		compacting = false,
		blocked = false
	}: {
		/** Resolves when the gateway accepted the prompt — or left the
		 * outcome unconfirmed, which resolves too: the queue snapshot is
		 * then the truth and the draft has done its work. Only a real
		 * rejection throws, and only then does the draft stay. */
		onsubmit?: (text: string) => Promise<void>
		oninterrupt?: () => void
		working?: boolean
		/** Auto-compaction in flight: the composer shows its chip while
		 * the checker's summarization runs ahead of the claim. */
		compacting?: boolean
		/** Submissions are blocked while the lane is not synchronized:
		 * the gateway would reject them anyway; the composer says why. */
		blocked?: boolean
	} = $props()

	let input = $state('')
	let submitting = $state(false)
	let submitError = $state<string | null>(null)
	// The draft an Escape dismissed completion for: any other draft
	// completes again, which is why the value is compared, not a flag.
	let dismissedFor = $state<string | null>(null)
	let textareaRef = $state<HTMLTextAreaElement | null>(null)
	let isSendButtonDisabled = $derived(
		input.trim() === '' || submitting || blocked
	)

	/** The rows the draft still completes, first match on top. A
	 * fully typed command has nothing left to complete, so Enter
	 * submits it. */
	const suggestions = $derived.by(() => {
		if (dismissedFor === input) return []
		if (!input.startsWith('/') || /\s/.test(input)) return []
		return COMMANDS.filter(
			(command) => command.name.startsWith(input) && command.name !== input
		)
	})

	// The keyboard's row: -1 means "nothing chosen yet", which selects
	// the first row for the popup and for Enter alike.
	let cursor = $state(-1)
	const activeCommand = $derived(
		suggestions[Math.min(cursor, suggestions.length - 1)]
	)

	function acceptSuggestion(name: string) {
		input = name
		cursor = -1
	}

	function handleKeydown(event: KeyboardEvent) {
		// An IME's Enter commits the composition, not the draft.
		if (event.isComposing) return
		if (event.key === 'ArrowDown' && suggestions.length > 0) {
			// The caret stays put; the selection moves instead. From the
			// unselected state the first press lands on the first row.
			event.preventDefault()
			cursor = Math.min(cursor + 1, suggestions.length - 1)
			return
		}
		if (event.key === 'ArrowUp' && suggestions.length > 0) {
			event.preventDefault()
			cursor = Math.max(cursor - 1, 0)
			return
		}
		if (
			(event.key === 'Enter' || event.key === 'Tab') &&
			suggestions.length > 0 &&
			!event.shiftKey
		) {
			event.preventDefault()
			acceptSuggestion((activeCommand ?? suggestions[0]).name)
			return
		}
		if (event.key === 'Escape' && suggestions.length > 0) {
			dismissedFor = input
			cursor = -1
			return
		}
		if (event.key === 'Enter' && !event.shiftKey) {
			handleSubmit(event)
		}
	}

	async function handleSubmit(e: KeyboardEvent | MouseEvent) {
		e.preventDefault()
		const text = input.trim()
		if (!text || submitting || blocked) {
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
</script>

<!-- Horizontal layout: textarea | stop + send buttons -->
<div
	class="bg-overlay-dark/70 relative flex items-end rounded-3xl transition-colors"
>
	<!-- The completion popover is portal-rendered directly above the
	textarea and as wide as it — nothing to align against the buttons.
	Selection lives in the Command primitive: one bound value styles
	and marks the row that keyboard, hover, and completion all use,
	and focus never leaves the textarea. The list keeps a fixed
	maximum height and scrolls past it. -->
	<Popover.Root open={suggestions.length > 0}>
		<Textarea
			bind:value={input}
			bind:ref={textareaRef}
			onkeydown={handleKeydown}
			placeholder="Enter your query!"
			class="max-h-[200px] min-h-[40px] flex-1 resize-none !border-0 !bg-transparent px-2 py-[10px] !text-lg !leading-5 focus-visible:!border-transparent focus-visible:!ring-0"
			rows={1}
		/>
		<Popover.Content
			customAnchor={textareaRef}
			side="top"
			class="w-[var(--bits-floating-anchor-width)] gap-0 p-1"
			onOpenAutoFocus={(event) => event.preventDefault()}
		>
			<Command.Root
				shouldFilter={false}
				label="Command suggestions"
				class="p-0"
				value={activeCommand?.name ?? ''}
			>
				<Command.List class="max-h-48">
					{#each suggestions as suggestion, index (suggestion.name)}
						<Command.Item
							value={suggestion.name}
							onSelect={() => acceptSuggestion(suggestion.name)}
							onpointermove={() => (cursor = index)}
							onmousedown={(event) => event.preventDefault()}
							class="data-selected:bg-accent data-selected:text-foreground flex w-full cursor-pointer items-center justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm"
						>
							<span class="font-mono">{suggestion.name}</span>
							<span class="text-muted-foreground truncate text-xs"
								>{suggestion.helper}</span
							>
						</Command.Item>
					{/each}
				</Command.List>
			</Command.Root>
		</Popover.Content>
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
	</Popover.Root>
</div>

{#if compacting}
	<p class="text-muted-foreground mx-auto mt-1 w-fit text-xs">
		Auto-compacting…
	</p>
{/if}
{#if submitError}
	<p class="text-destructive mx-auto mt-1 w-fit text-xs">
		{submitError}
	</p>
{/if}
