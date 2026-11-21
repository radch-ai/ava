<script lang="ts">
	import { untrack } from 'svelte'
	import { renderMarkdown } from '#lib/services/agent/markdown'

	let { content }: { content: string } = $props()

	// The first render is synchronous per mount, so a stored row never
	// flashes empty. Every later change coalesces through a fixed
	// 100 ms deadline: a streaming answer re-parses at most ten times a
	// second, and the deadline renders the newest content — interim
	// chunks never postpone it. A source equal to the last one parsed
	// needs no work at all.
	let html = $state(untrack(() => renderMarkdown(content)))
	let lastSource = untrack(() => content)
	let timer: ReturnType<typeof setTimeout> | undefined
	let lastRenderAt = untrack(() => Date.now())
	const RENDER_INTERVAL_MS = 100

	function renderLatest() {
		timer = undefined
		lastRenderAt = Date.now()
		html = renderMarkdown(content)
		lastSource = content
	}

	$effect(() => {
		const text = content
		if (text === lastSource) {
			// Back at the rendered source: a pending parse of an
			// intermediate source would only flash it.
			if (timer !== undefined) {
				clearTimeout(timer)
				timer = undefined
			}
			return
		}
		const wait = lastRenderAt + RENDER_INTERVAL_MS - Date.now()
		if (wait <= 0) {
			renderLatest()
			return
		}
		if (timer === undefined) timer = setTimeout(renderLatest, wait)
	})

	$effect(() => () => {
		if (timer !== undefined) clearTimeout(timer)
	})
</script>

<div class="text-foreground mb-4 w-full text-[15px] leading-relaxed">
	<!-- biome-ignore lint/nursery/noSvelteAtHtmlTags: model output is sanitized with dompurify inside renderMarkdown -->
	{@html html}
</div>
