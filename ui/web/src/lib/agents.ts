import type { AgentIconName } from '#components/agent-icons/index.ts'
import type { Agent } from '#lib/types/agent'

/** Agent as returned by the agents API; `label` is optional display text. */
export type RawAgent = {
	id: string
	name: string
	label?: string
	icon: AgentIconName
}

export function normalizeAgent(raw: RawAgent): Agent {
	return {
		id: raw.id,
		name: raw.name,
		label:
			raw.label ??
			raw.name.replace(/-/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase()),
		icon: raw.icon
	}
}
