<script lang="ts">
	import { page } from '$app/state'
</script>

<div class="flex flex-col items-center justify-center gap-2 pt-24 text-center">
	<h1 class="text-4xl font-semibold">{page.status}</h1>
	<p class="text-muted-foreground">{page.error?.message}</p>
</div>
