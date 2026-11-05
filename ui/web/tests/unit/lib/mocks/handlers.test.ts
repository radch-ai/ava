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
	it('lists agents without the system superagent entry', async () => {
		const res = await fetch(`${BASE}/agents`)
		expect(res.status).toBe(200)
		const body = (await res.json()) as {
			agents: { id: string; name: string; icon: string }[]
		}
		// The superagent is a client-side constant; the list only carries
		// user-created agents.
		expect(body.agents.map((agent) => agent.id)).toEqual([
			'research',
			'scribe',
			'sentinel'
		])
		for (const agent of body.agents) {
			expect(typeof agent.name).toBe('string')
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
		const res = await fetch(`${BASE}/agent/scribe/config`)
		expect(res.status).toBe(200)
		const detail = (await res.json()) as {
			id: string
			name: string
			icon: string
			soul: { personality: string; identity: string }
			toolsEnabled: string[]
			heartbeat: {
				model: string
				enabled: boolean
				duration: string
				content: string
			} | null
		}
		expect(detail.id).toBe('scribe')
		expect(detail.icon).toBe('pencil')
		expect(detail.name).toBe('Scribe')
		expect(typeof detail.soul.personality).toBe('string')
		expect(Array.isArray(detail.toolsEnabled)).toBe(true)
		// The body is flat: heartbeat rides beside the config fields —
		// a full config even without a saved row. Scribe has no row:
		// disabled with empty fields and the default routing ('auto');
		// only the superagent is seeded.
		expect(detail.heartbeat).toEqual({
			model: 'auto',
			enabled: false,
			duration: '1hr',
			content: ''
		})

		const superagentRes = await fetch(`${BASE}/agent/_superagent/config`)
		expect(superagentRes.status).toBe(200)
		const superagentDetail = (await superagentRes.json()) as {
			name: string
			heartbeat: { model: string; enabled: boolean; duration: string }
			cronJobs?: unknown[]
		}
		expect(superagentDetail.name).toBe('Ava')
		expect(superagentDetail.heartbeat.model).toBe('auto')
		expect(superagentDetail.heartbeat.enabled).toBe(true)
		expect(superagentDetail.heartbeat.duration).toBe('30m')
		// Cron jobs are fetched lazily from their own endpoint - the
		// detail body never carries them.
		expect(superagentDetail.cronJobs).toBeUndefined()

		const missing = await fetch(`${BASE}/agent/ghost/config`)
		expect(missing.status).toBe(404)
		const body = (await missing.json()) as { message: string }
		expect(body.message).toContain('not found')
	})

	it('merges PATCHed configuration fields and persists them', async () => {
		const res = await fetch(`${BASE}/agent/scribe/config`, {
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
		// Shallow merge: a present soul replaces the whole object — the
		// client always sends both sub-fields.
		expect(updated.soul.identity).toBe('New identity')
		expect(updated.soul.personality).toBeUndefined()

		const reread = (await (
			await fetch(`${BASE}/agent/scribe/config`)
		).json()) as {
			operatingInstructions: string
		}
		expect(reread.operatingInstructions).toBe('Updated instructions')
	})

	it('saves heartbeat rows on their own endpoint, not the config patch', async () => {
		const res = await fetch(`${BASE}/agent/scribe/heartbeat`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				model: 'auto',
				enabled: false,
				duration: '2hr',
				content: 'Rewritten check'
			})
		})
		expect(res.status).toBe(200)
		const detail = (await res.json()) as {
			heartbeat: {
				model: string
				enabled: boolean
				duration: string
				content: string
			}
		}
		expect(detail.heartbeat).toEqual({
			model: 'auto',
			enabled: false,
			duration: '2hr',
			content: 'Rewritten check'
		})

		// The row persists; the config patch never carries it.
		const reread = (await (
			await fetch(`${BASE}/agent/scribe/config`)
		).json()) as {
			heartbeat: { content: string }
		}
		expect(reread.heartbeat.content).toBe('Rewritten check')

		const missing = await fetch(`${BASE}/agent/ghost/heartbeat`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				model: 'auto',
				enabled: false,
				duration: '30m',
				content: ''
			})
		})
		expect(missing.status).toBe(404)
	})

	it('serves per-row cron endpoints on their own lazy list', async () => {
		// The list starts empty and is fetched on demand.
		const empty = await fetch(`${BASE}/agent/_superagent/cron-jobs`)
		expect(empty.status).toBe(200)
		const emptyBody = (await empty.json()) as { cronJobs: unknown[] }
		expect(emptyBody.cronJobs).toEqual([])

		// Create: the server mints the id.
		const created = await fetch(`${BASE}/agent/_superagent/cron-jobs`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				time: '09:00',
				frequency: 'custom',
				cronExpression: '0 9 * * *',
				model: 'auto',
				prompt: 'Morning digest'
			})
		})
		expect(created.status).toBe(201)
		const stored = (await created.json()) as {
			id: string
			cronExpression?: string
		}
		expect(stored.id).not.toBe('')
		expect(stored.cronExpression).toBe('0 9 * * *')

		// Full-job edit by id: switching to daily drops the expression.
		const patched = await fetch(
			`${BASE}/agent/_superagent/cron-jobs/${stored.id}`,
			{
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					time: '10:30',
					frequency: 'daily',
					model: 'auto',
					prompt: 'Morning digest'
				})
			}
		)
		expect(patched.status).toBe(200)
		const updated = (await patched.json()) as {
			time: string
			cronExpression?: string
		}
		expect(updated.time).toBe('10:30')
		expect(updated.cronExpression).toBeUndefined()

		// Guards: unknown job and unknown agent 404.
		const missingJob = await fetch(
			`${BASE}/agent/_superagent/cron-jobs/ghost`,
			{ method: 'PATCH', body: JSON.stringify({}) }
		)
		expect(missingJob.status).toBe(404)
		const missingAgent = await fetch(`${BASE}/agent/ghost/cron-jobs`)
		expect(missingAgent.status).toBe(404)

		// Delete by id is a 204; a second delete 404s.
		const removed = await fetch(
			`${BASE}/agent/_superagent/cron-jobs/${stored.id}`,
			{ method: 'DELETE' }
		)
		expect(removed.status).toBe(204)
		const again = await fetch(
			`${BASE}/agent/_superagent/cron-jobs/${stored.id}`,
			{ method: 'DELETE' }
		)
		expect(again.status).toBe(404)
	})
	it("updates the agents list entry when PATCH changes the agent's name or icon", async () => {
		const res = await fetch(`${BASE}/agent/scribe/config`, {
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
		expect(await created.json()).toEqual({ id: 'research-team' })

		const list = (await (await fetch(`${BASE}/agents`)).json()) as {
			agents: { id: string; name: string; label: string; icon: string }[]
		}
		const entry = list.agents.find((agent) => agent.id === 'research-team')
		expect(entry?.name).toBe('Research Team')

		const config = (await (
			await fetch(`${BASE}/agent/research-team/config`)
		).json()) as { name: string }
		expect(config.name).toBe('Research Team')
	})

	it('rejects duplicate derived ids with 409 and no auto-suffix', async () => {
		const payload = JSON.stringify({
			name: 'Research',
			icon: 'bot',
			config: {
				soul: { personality: '', identity: '' },
				operatingInstructions: '',
				toolsContext: '',
				toolsEnabled: []
			}
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
				config: {
					soul: { personality: '', identity: '' },
					operatingInstructions: '',
					toolsContext: '',
					toolsEnabled: []
				}
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
		it('exchanges the seeded token for a session cookie', async () => {
			const res = await fetch(`${BASE}/auth/exchange`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ token: 'ava-mock-token' })
			})
			expect(res.status).toBe(200)
			expect(res.headers.get('set-cookie')).toContain('ava_session=')
			expect(res.headers.get('set-cookie')).toContain('HttpOnly')
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
		})

		it('expires the session cookie on logout', async () => {
			const res = await fetch(`${BASE}/auth/logout`, { method: 'POST' })
			expect(res.status).toBe(204)
			expect(res.headers.get('set-cookie')).toContain('ava_session=;')
		})
	})
})
