<script lang="ts">
	import type { AgentIconName } from '#components/agent-icons/index.ts'
	import { Input } from '#components/ui/input'
	import { type AgentConfiguration, FIELD_CONFIGS } from '#lib/types/agent'
	import AgentIconPicker from './AgentIconPicker.svelte'

	interface Props {
		config: AgentConfiguration
	}

	let { config = $bindable() }: Props = $props()

	function handleIconSelect(icon: AgentIconName) {
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
		<AgentIconPicker
			id="agent-icon"
			value={(config.icon ?? 'bot') as AgentIconName}
			onselect={handleIconSelect}
		/>
	</div>
</div>
