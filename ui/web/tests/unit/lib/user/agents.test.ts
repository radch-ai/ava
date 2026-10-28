import { beforeEach, describe, expect, it } from 'vitest'
import type { IconName } from '#lib/icons'
import {
	addAgent,
	agents,
	clearAgents,
	normalizeAgent,
	setAgents
} from '#lib/user'

const bot: IconName = 'bot'
const chartColumn: IconName = 'chart-column'

describe('shared agents state', () => {
	beforeEach(() => {
		clearAgents()
	})

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

	describe('setAgents', () => {
		it('stores normalized agents keyed by id', () => {
			setAgents([{ id: 'support-bot', name: 'support-bot', icon: bot }])
			const stored = agents
			expect(stored).toHaveLength(1)
			expect(stored[0].id).toBe('support-bot')
			expect(stored[0].name).toBe('support-bot')
			expect(stored[0].label).toBe('Support Bot')
			expect(agents).toEqual(stored)
		})

		it('keeps the superagent first when the list provides it first', () => {
			setAgents([
				{ id: '_superagent', name: 'Ava', label: 'Chat with Ava', icon: bot },
				{ id: 'research', name: 'Research', icon: chartColumn }
			])
			expect(agents[0].id).toBe('_superagent')
			expect(agents[1].id).toBe('research')
		})

		it('replaces previous agents on setAgents', () => {
			setAgents([{ id: 'a', name: 'a', icon: bot }])
			setAgents([{ id: 'b', name: 'b', icon: bot }])
			expect(agents).toHaveLength(1)
			expect(agents[0].id).toBe('b')
		})
	})

	describe('addAgent', () => {
		it('appends a normalized agent with its id', () => {
			setAgents([{ id: 'existing', name: 'existing', icon: bot }])
			addAgent({ id: 'new-agent', name: 'New Agent', icon: bot })
			const stored = agents
			expect(stored).toHaveLength(2)
			expect(stored[1].id).toBe('new-agent')
			expect(stored[1].name).toBe('New Agent')
			expect(stored[1].label).toBe('New Agent')
		})

		it('works without prior setAgents', () => {
			addAgent({ id: 'orphan', name: 'Orphan', icon: bot })
			expect(agents).toHaveLength(1)
			expect(agents[0].id).toBe('orphan')
		})
	})
})
