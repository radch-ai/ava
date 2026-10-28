import { describe, expect, it } from 'vitest'
import { deriveAgentId, isValidAgentName } from '#lib/mocks/agent-id'

describe('isValidAgentName', () => {
	it('accepts single words', () => {
		expect(isValidAgentName('Research')).toBe(true)
	})

	it('accepts digits and mixed case', () => {
		expect(isValidAgentName('agent2')).toBe(true)
		expect(isValidAgentName('Team 02')).toBe(true)
	})

	it('accepts multiple single spaces between words', () => {
		expect(isValidAgentName('Research Team Two')).toBe(true)
	})

	it('trims surrounding whitespace before validating', () => {
		expect(isValidAgentName('  Bot  ')).toBe(true)
	})

	it('rejects double spaces', () => {
		expect(isValidAgentName('Double  Space')).toBe(false)
	})

	it('rejects symbols, underscores and hyphens', () => {
		expect(isValidAgentName('under_score')).toBe(false)
		expect(isValidAgentName('hyphen-name')).toBe(false)
		expect(isValidAgentName('bot!')).toBe(false)
	})

	it('rejects empty and whitespace-only names', () => {
		expect(isValidAgentName('')).toBe(false)
		expect(isValidAgentName('   ')).toBe(false)
	})

	it('rejects non-ASCII letters (alphanumeric means ASCII)', () => {
		expect(isValidAgentName('Böt')).toBe(false)
	})
})

describe('deriveAgentId', () => {
	it('lowercases single words', () => {
		expect(deriveAgentId('Research')).toBe('research')
	})

	it('collapses case so only one agent per spelling can exist', () => {
		expect(deriveAgentId('RESEARCH')).toBe('research')
	})

	it('converts single spaces to hyphens', () => {
		expect(deriveAgentId('Research Team')).toBe('research-team')
	})

	it('trims surrounding whitespace first', () => {
		expect(deriveAgentId('  Padded  ')).toBe('padded')
	})

	it('defensively strips symbols and collapses repeats from invalid input', () => {
		expect(deriveAgentId('under_score')).toBe('underscore')
		expect(deriveAgentId('Double  Space')).toBe('double-space')
		expect(deriveAgentId('!!!')).toBe('')
	})

	it('never produces an id starting with underscore', () => {
		// Names cannot contain underscores, so derivation can never emit one.
		for (const name of ['_x', 'a_b', 'C D', 'z']) {
			expect(deriveAgentId(name).startsWith('_')).toBe(false)
		}
	})
})
