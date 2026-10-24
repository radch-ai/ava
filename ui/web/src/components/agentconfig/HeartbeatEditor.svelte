<script lang="ts">
	import { Textarea } from '$components/ui/textarea';
	import { FIELD_CONFIGS } from '$lib/types/agent';
	import type { HeartbeatConfig } from '$lib/types/agent';
	import ModelSelector from '$components/ModelSelector.svelte';

	interface Props {
		heartbeat: HeartbeatConfig;
	}

	let { heartbeat = $bindable() }: Props = $props();
</script>

<div class="space-y-4">
	<p class="text-sm text-muted-foreground">{FIELD_CONFIGS.heartbeat.description}</p>
	<div>
		<label for="heartbeat-enabled" class="mb-2 block text-sm font-medium">Enabled</label>
		<input
			id="heartbeat-enabled"
			type="checkbox"
			checked={heartbeat.enabled}
			onchange={(e) => (heartbeat.enabled = e.currentTarget.checked)}
		/>
	</div>
	<div>
		<label for="heartbeat-model" class="mb-2 block text-sm font-medium">Model</label>
		<ModelSelector bind:value={heartbeat.model} />
	</div>
	<div>
		<label for="heartbeat-duration" class="mb-2 block text-sm font-medium">Duration</label>
		<select
			id="heartbeat-duration"
			class="w-full rounded-md border bg-background px-3 py-2 text-sm"
			value={heartbeat.duration}
			onchange={(e) => (heartbeat.duration = e.currentTarget.value as HeartbeatConfig['duration'])}
		>
			<option value="30m">30 minutes</option>
			<option value="1hr">1 hour</option>
			<option value="2hr">2 hours</option>
		</select>
	</div>
	<div>
		<label for="heartbeat-content" class="mb-2 block text-sm font-medium">Content</label>
		<Textarea
			id="heartbeat-content"
			value={heartbeat.content}
			oninput={(e) => (heartbeat.content = e.currentTarget.value)}
			placeholder="Check for new leads&#10;Review yesterday's follow-ups"
			class="min-h-[200px]"
		/>
	</div>
</div>
