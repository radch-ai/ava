<script lang="ts">
	import AppSidebar from '$components/AppSidebar.svelte';
	import * as Sidebar from '$components/ui/sidebar';

	let open = $state(true);
</script>

<Sidebar.Provider bind:open>
	<AppSidebar />
</Sidebar.Provider>
