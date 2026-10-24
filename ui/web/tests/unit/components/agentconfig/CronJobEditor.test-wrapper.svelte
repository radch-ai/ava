<script lang="ts">
	import CronJobEditor from '$components/agentconfig/CronJobEditor.svelte'
	import type { CronJob } from '$lib/types/agent'

	interface Props {
		initialJobs?: CronJob[]
		onJobsChange?: (jobs: CronJob[]) => void
	}

	let { initialJobs = [], onJobsChange }: Props = $props()
	let jobs = $derived(Array.isArray(initialJobs) ? [...initialJobs] : [])

	// Notify test when jobs change
	$effect(() => {
		if (Array.isArray(jobs)) {
			onJobsChange?.([...jobs])
		}
	})
</script>

<CronJobEditor bind:jobs />
