<script lang="ts">
	import { Textarea } from '$components/ui/textarea';
	import { SOUL_FIELD_CONFIGS, type SoulConfig } from '$lib/types/agent';

	interface Props {
		soul: SoulConfig;
	}

	let { soul = $bindable() }: Props = $props();
</script>

<div class="space-y-4">
	<div>
		<label for="soul-personality" class="mb-2 block text-sm font-medium"
			>{SOUL_FIELD_CONFIGS.personality.label}</label
		>
		<p class="mb-2 text-sm text-muted-foreground">
			{SOUL_FIELD_CONFIGS.personality.description}
		</p>
		<Textarea
			id="soul-personality"
			value={soul.personality}
			oninput={(e) => (soul.personality = e.currentTarget.value)}
			placeholder={SOUL_FIELD_CONFIGS.personality.placeholder}
			class="min-h-[150px]"
		/>
	</div>
	<div>
		<label for="soul-identity" class="mb-2 block text-sm font-medium"
			>{SOUL_FIELD_CONFIGS.identity.label}</label
		>
		<p class="mb-2 text-sm text-muted-foreground">
			{SOUL_FIELD_CONFIGS.identity.description}
		</p>
		<Textarea
			id="soul-identity"
			value={soul.identity}
			oninput={(e) => (soul.identity = e.currentTarget.value)}
			placeholder={SOUL_FIELD_CONFIGS.identity.placeholder}
			class="min-h-[150px]"
		/>
	</div>
</div>
