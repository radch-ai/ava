import { describe, expect, it } from 'vitest'
import type { AgentIconName } from '#components/agent-icons/index.ts'
import { normalizeAgent } from '#lib/agents'

const bot: AgentIconName = 'bot'
const chartColumn: AgentIconName = 'chart-column'

describe('normalizeAgent', () => {
	it('keeps id and derives label from name when label is missing', () => {
		const result = normalizeAgent({
			id: 'support-bot',
			name: 'support-bot',
			icon: bot
		})
		expect(result.id).toBe('support-bot')
		expect(result.label).toBe('Support Bot')
	})

	it('preserves existing label', () => {
		const result = normalizeAgent({
			id: 'support',
			name: 'support',
			label: 'Help Desk',
			icon: bot
		})
		expect(result.label).toBe('Help Desk')
	})

	it('preserves name and icon', () => {
		const result = normalizeAgent({
			id: 'analyst',
			name: 'Analyst',
			icon: chartColumn
		})
		expect(result.name).toBe('Analyst')
		expect(result.icon).toBe(chartColumn)
	})
})
