import type { Agent } from '$lib/types/agent';
import type { IconName } from '$lib/icons';

type RawAgent = { name: string; label?: string; icon: IconName };

export function normalizeAgent(raw: RawAgent): Agent {
	return {
		name: raw.name,
		label: raw.label ?? raw.name.replace(/-/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase()),
		icon: raw.icon
	};
}

export const agents = $state<Agent[]>([]);

export function setAgents(raw: RawAgent[]): void {
	agents.length = 0;
	agents.push(...raw.map(normalizeAgent));
}

export function addAgent(raw: RawAgent): void {
	agents.push(normalizeAgent(raw));
}

export function clearAgents(): void {
	agents.length = 0;
}
