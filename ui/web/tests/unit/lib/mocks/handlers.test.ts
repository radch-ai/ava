import { setupServer } from 'msw/node'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { createSeedData } from '#lib/mocks/data'
import { createHandlers } from '#lib/mocks/handlers'

const server = setupServer(...createHandlers(createSeedData()))

const BASE = 'http://localhost:8080/api'

beforeAll(() => {
	server.listen({ onUnhandledFrame: 'error' })
})

afterAll(() => {
	server.close()
})

describe('mock API handlers', () => {
	it('lists agents with ids, superagent first', async () => {
		const res = await fetch(`${BASE}/agents`)
		expect(res.status).toBe(200)
		const body = (await res.json()) as {
			agents: { id: string; name: string; label: string; icon: string }[]
		}
		expect(body.agents.map((agent) => agent.id)).toEqual([
			'_superagent',
			'research',
			'scribe',
			'sentinel'
		])
		expect(body.agents.map((agent) => agent.name)).toEqual([
			'Ava',
			'Research',
			'Scribe',
			'Sentinel'
		])
		for (const agent of body.agents) {
			expect(typeof agent.label).toBe('string')
			expect(typeof agent.icon).toBe('string')
		}
	})

	it('pages the superagent thread newest-first until the cursor is exhausted', async () => {
		const first = (await (
			await fetch(`${BASE}/agent/_superagent/thread?limit=10`)
		).json()) as {
			items: { id: string }[]
			nextCursor: string | null
		}
		expect(first.items.map((message) => message.id)).toEqual([
			'msg-24',
			'msg-23',
			'msg-22',
			'msg-21',
			'msg-20',
			'msg-19',
			'msg-18',
			'msg-17',
			'msg-16',
			'msg-15'
		])
		expect(first.nextCursor).toBe('msg-15')

		const second = (await (
			await fetch(`${BASE}/agent/_superagent/thread?limit=10&cursor=msg-15`)
		).json()) as {
			items: { id: string }[]
			nextCursor: string | null
		}
		expect(second.items.map((message) => message.id)).toEqual([
			'msg-14',
			'msg-13',
			'msg-12',
			'msg-11',
			'msg-10',
			'msg-9',
			'msg-8',
			'msg-7',
			'msg-6',
			'msg-5'
		])
		expect(second.nextCursor).toBe('msg-5')

		const third = (await (
			await fetch(`${BASE}/agent/_superagent/thread?limit=10&cursor=msg-5`)
		).json()) as {
			items: { id: string }[]
			nextCursor: string | null
		}
		expect(third.items.map((message) => message.id)).toEqual([
			'msg-4',
			'msg-3',
			'msg-2',
			'msg-1'
		])
		expect(third.nextCursor).toBeNull()
	})

	it('404s the thread endpoint for agents without a thread', async () => {
		const res = await fetch(`${BASE}/agent/research/thread`)
		expect(res.status).toBe(404)
	})

	it('rejects an unknown thread cursor with 400', async () => {
		const res = await fetch(`${BASE}/agent/_superagent/thread?cursor=missing`)
		expect(res.status).toBe(400)
	})

	it('returns an activity feed for a known agent and an empty page for unknown agents', async () => {
		const known = (await (
			await fetch(`${BASE}/agent/research/activity?limit=5`)
		).json()) as {
			items: { id: string; status: string }[]
			nextCursor: string | null
		}
		expect(known.items).toHaveLength(5)
		expect(known.nextCursor).toBeTypeOf('string')
		for (const item of known.items) {
			expect(['running', 'completed', 'failed']).toContain(item.status)
		}

		const superagent = (await (
			await fetch(`${BASE}/agent/_superagent/activity?limit=3`)
		).json()) as {
			items: { id: string }[]
		}
		expect(superagent.items).toHaveLength(3)

		const unknown = await (await fetch(`${BASE}/agent/ghost/activity`)).json()
		expect(unknown).toEqual({ items: [], nextCursor: null })
	})

	it('serves agent configuration and 404s for unknown agents', async () => {
		const res = await fetch(`${BASE}/agent/scribe`)
		expect(res.status).toBe(200)
		const config = (await res.json()) as {
			name: string
			soul: { personality: string; identity: string }
			toolsEnabled: string[]
		}
		expect(config.name).toBe('Scribe')
		expect(typeof config.soul.personality).toBe('string')
		expect(Array.isArray(config.toolsEnabled)).toBe(true)

		const superagentRes = await fetch(`${BASE}/agent/_superagent`)
		expect(superagentRes.status).toBe(200)
		const superagentConfig = (await superagentRes.json()) as { name: string }
		expect(superagentConfig.name).toBe('Ava')

		const missing = await fetch(`${BASE}/agent/ghost`)
		expect(missing.status).toBe(404)
		const body = (await missing.json()) as { message: string }
		expect(body.message).toContain('not found')
	})

	it('merges PATCHed configuration fields and persists them', async () => {
		const res = await fetch(`${BASE}/agent/scribe`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				operatingInstructions: 'Updated instructions',
				soul: { identity: 'New identity' }
			})
		})
		expect(res.status).toBe(200)
		const updated = (await res.json()) as {
			operatingInstructions: string
			soul: { personality: string; identity: string }
		}
		expect(updated.operatingInstructions).toBe('Updated instructions')
		expect(updated.soul.identity).toBe('New identity')
		expect(typeof updated.soul.personality).toBe('string')

		const reread = (await (await fetch(`${BASE}/agent/scribe`)).json()) as {
			operatingInstructions: string
		}
		expect(reread.operatingInstructions).toBe('Updated instructions')
	})

	it("updates the agents list entry when PATCH changes the agent's name or icon", async () => {
		const res = await fetch(`${BASE}/agent/scribe`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ name: 'Scribe Two', icon: 'bot' })
		})
		expect(res.status).toBe(200)

		const list = (await (await fetch(`${BASE}/agents`)).json()) as {
			agents: { id: string; name: string; label: string; icon: string }[]
		}
		const entry = list.agents.find((agent) => agent.id === 'scribe')
		expect(entry?.name).toBe('Scribe Two')
		expect(entry?.label).toBe('Scribe Two')
		expect(entry?.icon).toBe('bot')
	})

	it('creates agents via POST /agent and returns the derived id', async () => {
		const created = await fetch(`${BASE}/agent`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				name: 'Research Team',
				icon: 'bot',
				soul: { personality: '', identity: '' },
				operatingInstructions: '',
				toolsContext: '',
				toolsEnabled: []
			})
		})
		expect(created.status).toBe(201)
		expect(await created.json()).toEqual({
			id: 'research-team',
			name: 'Research Team'
		})

		const list = (await (await fetch(`${BASE}/agents`)).json()) as {
			agents: { id: string; name: string; label: string; icon: string }[]
		}
		const entry = list.agents.find((agent) => agent.id === 'research-team')
		expect(entry?.name).toBe('Research Team')
		expect(typeof entry?.label).toBe('string')

		const config = (await (
			await fetch(`${BASE}/agent/research-team`)
		).json()) as { name: string }
		expect(config.name).toBe('Research Team')
	})

	it('rejects duplicate derived ids with 409 and no auto-suffix', async () => {
		const payload = JSON.stringify({
			name: 'Research',
			icon: 'bot',
			soul: { personality: '', identity: '' },
			operatingInstructions: '',
			toolsContext: '',
			toolsEnabled: []
		})

		const duplicate = await fetch(`${BASE}/agent`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: payload
		})
		expect(duplicate.status).toBe(409)
		const body = (await duplicate.json()) as { message: string }
		expect(body.message).toBe('An agent named "Research" already exists')

		const caseVariant = await fetch(`${BASE}/agent`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				name: 'RESEARCH',
				icon: 'bot',
				soul: { personality: '', identity: '' },
				operatingInstructions: '',
				toolsContext: '',
				toolsEnabled: []
			})
		})
		expect(caseVariant.status).toBe(409)
	})

	it('rejects invalid display names with 400', async () => {
		for (const name of ['bad_name', 'hyphen-name', 'double  space', '']) {
			const res = await fetch(`${BASE}/agent`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					name,
					icon: 'bot',
					soul: { personality: '', identity: '' },
					operatingInstructions: '',
					toolsContext: '',
					toolsEnabled: []
				})
			})
			expect(res.status).toBe(400)
			const body = (await res.json()) as { message: string }
			expect(body.message).toContain('Invalid agent name')
		}
	})

	describe('auth endpoints', () => {
		it('exchanges the seeded token for a session cookie and user', async () => {
			const res = await fetch(`${BASE}/auth/exchange`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ token: 'ava-mock-token' })
			})
			expect(res.status).toBe(200)
			expect(res.headers.get('set-cookie')).toContain('ava_session=')
			expect(res.headers.get('set-cookie')).toContain('HttpOnly')
			expect(await res.json()).toEqual({ user: { sub: 'local' } })
		})

		it('rejects a wrong token with 401', async () => {
			const res = await fetch(`${BASE}/auth/exchange`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ token: 'nope' })
			})
			expect(res.status).toBe(401)
		})

		it('serves /auth/me only with a session cookie', async () => {
			const anon = await fetch(`${BASE}/auth/me`)
			expect(anon.status).toBe(401)

			const authed = await fetch(`${BASE}/auth/me`, {
				headers: { cookie: 'ava_session=mock-session' }
			})
			expect(authed.status).toBe(200)
			expect(await authed.json()).toEqual({ user: { sub: 'local' } })
		})

		it('expires the session cookie on logout', async () => {
			const res = await fetch(`${BASE}/auth/logout`, { method: 'POST' })
			expect(res.status).toBe(204)
			expect(res.headers.get('set-cookie')).toContain('ava_session=;')
		})
	})
})
