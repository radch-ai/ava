import { describe, expect, it, vi } from 'vitest'

vi.mock('$app/paths', () => ({
	resolve: vi.fn((path: string, params?: Record<string, string>) => {
		const result = path.replace(/^\/?\([^)]*\)\//, '/')
		return params?.agentId
			? result.replace('[agentId]', params.agentId)
			: result
	})
}))

import { load } from '../../../../src/routes/(app)/+page'

describe('root page load', () => {
	it('redirects / to the superagent agent page', () => {
		let rejection: unknown
		try {
			load({} as never)
			throw new Error('expected load to redirect')
		} catch (reason) {
			rejection = reason
		}
		const redirect = rejection as { status: number; location: string }
		expect(redirect.status).toBe(307)
		expect(redirect.location).toBe('/agent/_superagent')
	})
})
