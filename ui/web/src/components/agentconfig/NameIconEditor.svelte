<script lang="ts">
	import { Input } from '#components/ui/input'
	import type { IconName } from '#lib/icons'
	import { type AgentConfiguration, FIELD_CONFIGS } from '#lib/types/agent'
	import IconPicker from './IconPicker.svelte'

	interface Props {
		config: AgentConfiguration
	}

	let { config = $bindable() }: Props = $props()

	function handleIconSelect(icon: IconName) {
		config.icon = icon
	}
</script>

<div class="flex flex-col gap-4">
	<div>
		<label for="agent-name" class="mb-2 block text-sm font-medium"
			>{FIELD_CONFIGS.name.label}</label
		>
		<Input
			id="agent-name"
			value={config.name}
			oninput={(e) => (config.name = e.currentTarget.value)}
			placeholder={FIELD_CONFIGS.name.placeholder}
		/>
	</div>
	<div>
		<label for="agent-icon" class="mb-2 block text-sm font-medium">Icon</label>
		<IconPicker
			id="agent-icon"
			value={(config.icon ?? 'bot') as IconName}
			onselect={handleIconSelect}
		/>
	</div>
</div>
