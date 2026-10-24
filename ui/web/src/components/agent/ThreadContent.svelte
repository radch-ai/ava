<script lang="ts">
	interface ThreadItem {
		id: string;
		role: 'user' | 'assistant';
		content: string;
	}

	interface Props {
		threadItems: ThreadItem[];
	}

	let { threadItems }: Props = $props();
</script>

{#if threadItems.length > 0}
	{#each threadItems as message (message.id)}
		{#if message.role === 'user'}
			<div
				class="ml-auto w-fit max-w-[75%] rounded-2xl rounded-br-[4px] bg-overlay-dark/70 px-5 py-3 text-[15px] leading-relaxed text-foreground/80 shadow-sm hover:bg-overlay-dark/90 hover:text-foreground"
			>
				{message.content}
			</div>
		{:else}
			<div class="mr-auto mb-4 w-full text-[15px] leading-relaxed text-foreground/90">
				{message.content}
			</div>
		{/if}
	{/each}
{/if}
