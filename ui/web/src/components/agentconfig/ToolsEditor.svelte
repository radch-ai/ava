<script lang="ts">
	import { Switch } from '$components/ui/switch';
	import { Label } from '$components/ui/label';
	import type { Tool } from '$lib/types/tool';

	interface Props {
		/** All available tools */
		tools: Tool[];
		/** IDs of enabled tools */
		enabledToolIds: string[];
	}

	let { tools, enabledToolIds = $bindable() }: Props = $props();

	function toggleTool(toolId: string) {
		if (enabledToolIds.includes(toolId)) {
			enabledToolIds = enabledToolIds.filter((id) => id !== toolId);
		} else {
			enabledToolIds = [...enabledToolIds, toolId];
		}
	}

	function isToolEnabled(toolId: string): boolean {
		return enabledToolIds.includes(toolId);
	}
</script>

<div class="space-y-4">
	{#if tools.length === 0}
		<p class="text-sm text-muted-foreground">
			No tools available. Add tools to your account first.
		</p>
	{:else}
		<div class="space-y-3">
			{#each tools as tool (tool.id)}
				<div
					class="flex items-center justify-between rounded-lg border bg-muted/30 p-3"
					class:ring-2={isToolEnabled(tool.id)}
					class:ring-primary={isToolEnabled(tool.id)}
				>
					<div class="flex-1 pr-4">
						<Label for="tool-{tool.id}" class="font-medium">
							{tool.name}
						</Label>
						{#if tool.description}
							<p class="text-sm text-muted-foreground">{tool.description}</p>
						{/if}
					</div>
					<Switch
						id="tool-{tool.id}"
						checked={isToolEnabled(tool.id)}
						onCheckedChange={() => toggleTool(tool.id)}
						aria-label="Enable {tool.name}"
					/>
				</div>
			{/each}
		</div>
	{/if}
</div>
