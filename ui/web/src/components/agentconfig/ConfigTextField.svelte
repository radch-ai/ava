<script lang="ts">
	import { Textarea } from '$components/ui/textarea';
	import type { FieldConfig } from '$lib/types/agent';

	interface Props {
		value: string;
		config: FieldConfig;
		onchange: (value: string) => void;
	}

	let { value, config, onchange }: Props = $props();
</script>

<div class="space-y-2">
	<p class="text-sm text-muted-foreground">{config.description}</p>
	<Textarea
		{value}
		oninput={(e) => onchange(e.currentTarget.value)}
		placeholder={config.placeholder}
		class="min-h-[300px]"
	/>
</div>
