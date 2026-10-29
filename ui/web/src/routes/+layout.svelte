<script lang="ts">
	import { APP_NAME } from '#lib/config'
	import '#app.css'

	let { children } = $props()
</script>

<svelte:head>
	<title>{APP_NAME}</title>
</svelte:head>

{@render children()}
