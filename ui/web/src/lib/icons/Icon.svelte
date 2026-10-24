<script lang="ts">
	import { type IconName, WORKER_ICONS } from './worker-icons'

	interface Props {
		name?: IconName | string | null
		size?: number
		class?: string
	}

	let { name, size = 24, class: className }: Props = $props()

	let safeName: IconName = $derived(
		name && name in WORKER_ICONS ? (name as IconName) : 'bot'
	)
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="2"
	stroke-linecap="round"
	stroke-linejoin="round"
	class={className}
	aria-hidden="true"
>
	{#each WORKER_ICONS[safeName] as [tag, attrs], i (`icon-${safeName}-${i}`)}
		<svelte:element this={tag} {...attrs} />
	{/each}
</svg>
