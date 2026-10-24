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
		it('derives label from name when label is missing', () => {
			const result = normalizeAgent({ name: 'my-support-bot', icon: bot })
			expect(result.label).toBe('My Support Bot')
		})

		it('preserves existing label', () => {
			const result = normalizeAgent({
				name: 'support',
				label: 'Help Desk',
				icon: bot
			})
			expect(result.label).toBe('Help Desk')
		})

		it('preserves name and icon', () => {
			const result = normalizeAgent({ name: 'analyst', icon: chartColumn })
			expect(result.name).toBe('analyst')
			expect(result.icon).toBe(chartColumn)
		})
	})

	describe('setAgents', () => {
		it('stores normalized agents', () => {
			setAgents([{ name: 'support-bot', icon: bot }])
			const stored = agents
			expect(stored).toHaveLength(1)
			expect(stored[0].name).toBe('support-bot')
			expect(stored[0].label).toBe('Support Bot')
			expect(agents).toEqual(stored)
		})

		it('replaces previous agents on setAgents', () => {
			setAgents([{ name: 'a', icon: bot }])
			setAgents([{ name: 'b', icon: bot }])
			expect(agents).toHaveLength(1)
			expect(agents[0].name).toBe('b')
		})
	})

	describe('addAgent', () => {
		it('appends a normalized agent', () => {
			setAgents([{ name: 'existing', icon: bot }])
			addAgent({ name: 'new-agent', icon: bot })
			const stored = agents
			expect(stored).toHaveLength(2)
			expect(stored[1].name).toBe('new-agent')
			expect(stored[1].label).toBe('New Agent')
		})

		it('works without prior setAgents', () => {
			addAgent({ name: 'orphan', icon: bot })
			expect(agents).toHaveLength(1)
			expect(agents[0].name).toBe('orphan')
		})
	})
})
