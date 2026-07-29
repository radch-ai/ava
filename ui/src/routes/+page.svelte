<script lang="ts">
	import { Button } from '$components/ui/button'
	import { commands, unwrapCommand } from '$lib/command'

	let name = $state('')
	let greetMsg = $state('')

	async function greet(event: Event) {
		event.preventDefault()
		greetMsg = unwrapCommand(await commands.greet(name))
	}
</script>

<main class="flex min-h-svh flex-col items-center justify-center gap-6 p-8">
	<h1 class="text-2xl font-semibold tracking-tight">Welcome to ava</h1>

	<form class="flex gap-2" onsubmit={greet}>
		<input
			id="greet-input"
			class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-64 rounded-md border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
			placeholder="Enter a name..."
			bind:value={name}
		>
		<Button type="submit">Greet</Button>
	</form>

	{#if greetMsg}
		<p class="text-muted-foreground text-sm">{greetMsg}</p>
	{/if}
</main>
