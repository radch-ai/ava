import { setupServer } from 'msw/node'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { createSeedData } from '#lib/mocks/data'
import { createHandlers } from '#lib/mocks/handlers'
import type { ModelEntry } from '#lib/models'

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

	// fetchThread reads one page of the mock thread contract.
	async function fetchThread(query = '') {
		return (await (
			await fetch(`${BASE}/agent/_superagent/thread${query}`)
		).json()) as {
			items: { id: string; role: string }[]
			nextCursor: string | null
			threadId: string
		}
	}

	it('pages a thread in the backend’s wire shape', async () => {
		// Shapes only: the paging semantics (keyset walk, limit rules)
		// are the backend endpoint tests' job. The mock must mirror
		// what the client parses.
		const first = await fetchThread('?limit=10')
		expect(first.threadId).toBe('thread-2')
		expect(first.items.map((item) => item.id)).toEqual([
			'msg-15',
			'msg-16',
			'msg-17',
			'msg-18',
			'msg-19',
			'msg-20',
			'msg-21',
			'msg-22',
			'msg-23',
			'msg-24'
		])
		expect(first.nextCursor).toBe('thread-2:msg-15')

		// An exhausted thread leads with its divider row — keyed by the
		// thread id — and points the cursor at the predecessor.
		const second = await fetchThread('?limit=10&cursor=thread-2:msg-15')
		expect(second.items[0]).toMatchObject({
			id: 'thread-2',
			role: 'divider'
		})
		expect(second.items.at(-1)?.id).toBe('msg-14')
		expect(second.nextCursor).toBe('thread-1:')

		// The chain start answers null: nothing precedes it.
		const third = await fetchThread('?limit=10&cursor=thread-1:')
		expect(third.threadId).toBe('thread-1')
		expect(third.nextCursor).toBeNull()
	})

	it('serves known agents their own empty thread and 404s unknown agents', async () => {
		const empty = (await (
			await fetch(`${BASE}/agent/research/thread`)
		).json()) as {
			items: { id: string }[]
			nextCursor: string | null
			threadId: string
		}
		expect(empty.items).toEqual([])
		expect(empty.nextCursor).toBeNull()
		expect(empty.threadId).toBe('research-t1')
		const missing = await fetch(`${BASE}/agent/ghost/thread`)
		expect(missing.status).toBe(404)
	})

	it('rejects cursors that name no readable row with 400', async () => {
		for (const cursor of ['missing', 'ghost-thread:', 'thread-2:nope']) {
			const res = await fetch(
				`${BASE}/agent/_superagent/thread?cursor=${cursor}`
			)
			expect(res.status).toBe(400)
		}
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
			heartbeat: {
				model?: ModelEntry | null
				enabled: boolean
				duration: string
				content: string
			} | null
		}
		expect(detail.id).toBe('scribe')
		expect(detail.icon).toBe('pencil')
		expect(detail.name).toBe('Scribe')
		expect(typeof detail.soul.personality).toBe('string')
		// The body is flat: heartbeat rides beside the config fields —
		// a full config even without a saved row. Scribe has no row:
		// disabled with empty fields and default routing (no model);
		// only the superagent is seeded.
		expect(detail.heartbeat).toEqual({
			model: undefined,
			enabled: false,
			duration: '1hr',
			content: ''
		})

		const superagentRes = await fetch(`${BASE}/agent/_superagent/config`)
		expect(superagentRes.status).toBe(200)
		const superagentDetail = (await superagentRes.json()) as {
			name: string
			heartbeat: {
				model?: ModelEntry | null
				enabled: boolean
				duration: string
			}
			cronJobs?: unknown[]
		}
		expect(superagentDetail.name).toBe('Ava')
		// Seeded enabled on default routing: no model set.
		expect(superagentDetail.heartbeat.model).toBeUndefined()
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
			body: JSON.stringify({
				model: null,
				enabled: false,
				duration: '2hr',
				content: 'Rewritten check'
			})
		})
		expect(res.status).toBe(200)
		const detail = (await res.json()) as {
			heartbeat: {
				model: { id: string; providerId: string } | null
				enabled: boolean
				duration: string
				content: string
			}
		}
		expect(detail.heartbeat).toEqual({
			model: null,
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
				model: null,
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
				model: null,
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
					model: null,
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
				toolsContext: ''
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
				toolsContext: ''
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
					toolsContext: ''
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
					toolsContext: ''
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

	describe('provider catalog endpoints', () => {
		it('answers the merged catalog: starters keyless, custom models only', async () => {
			const body = (await (await fetch(`${BASE}/providers`)).json()) as {
				providers: Array<{
					id: string
					name: string
					hasKey: boolean
					apiKey?: string
					models: Array<{ id: string; contextWindow?: number }>
				}>
			}
			expect(body.providers.map((p) => p.name)).toEqual([
				'Anthropic',
				'Gemini',
				'Grok',
				'OpenAI'
			])
			// Rows carry custom ids only — system models live behind
			// the models endpoints.
			for (const provider of body.providers) {
				expect(provider.models).toEqual([])
				expect(provider).not.toHaveProperty('apiKey')
				expect(provider.hasKey).toBe(false)
			}

			const models = (await (await fetch(`${BASE}/models`)).json()) as {
				groups: Array<{
					providerName: string
					models: Array<{ id: string; providerId: string }>
				}>
			}
			expect(
				models.groups.find((g) => g.providerName === 'OpenAI')?.models
			).toEqual([
				{ id: 'openai/gpt-3.5-turbo', providerId: 'openai' },
				{ id: 'openai/gpt-4', providerId: 'openai' }
			])

			// The settings expand serves one starter's system model ids.
			const openai = (
				await (await fetch(`${BASE}/providers/openai/models`)).json()
			).systemModels as string[]
			expect(openai).toEqual(['openai/gpt-3.5-turbo', 'openai/gpt-4'])
		})

		it('creates providers via POST with an opaque id and validates input', async () => {
			const created = await fetch(`${BASE}/providers`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					name: 'Mistral',
					baseUrl: 'https://api.mistral.ai/v1',
					apiKey: 'sk-m'
				})
			})
			expect(created.status).toBe(201)
			const { id } = (await created.json()) as { id: string }
			// The id is server-generated and opaque — nothing derives
			// from the display name.
			expect(id).not.toBe('mistral')
			expect(id.length).toBeGreaterThan(0)

			const noName = await fetch(`${BASE}/providers`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ name: '  ', baseUrl: 'https://x.dev' })
			})
			expect(noName.status).toBe(400)

			const badUrl = await fetch(`${BASE}/providers`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ name: 'No URL' })
			})
			expect(badUrl.status).toBe(400)

			// The created row masks its key in GET responses.
			const listed = (await (await fetch(`${BASE}/providers`)).json()) as {
				providers: Array<{ id: string; hasKey: boolean }>
			}
			expect(listed.providers.find((p) => p.id === id)?.hasKey).toBe(true)
		})

		it('replaces the model list on PATCH and keeps a blank apiKey', async () => {
			const created = await fetch(`${BASE}/providers`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					name: 'Mistral',
					baseUrl: 'https://api.mistral.ai/v1',
					apiKey: 'sk-m'
				})
			})
			const { id } = (await created.json()) as { id: string }

			const res = await fetch(`${BASE}/providers/${id}`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					models: [
						{ id: 'mistral/mistral-large' },
						{ id: 'mistral/mistral-small' }
					]
				})
			})
			expect(res.status).toBe(200)
			const provider = (await res.json()) as {
				models: Array<{ id: string; contextWindow?: number }>
				hasKey: boolean
			}
			expect(provider.models).toEqual([
				{ id: 'mistral/mistral-large' },
				{ id: 'mistral/mistral-small' }
			])
			// No apiKey in the patch: the stored key survives.
			expect(provider.hasKey).toBe(true)

			const empty = await fetch(`${BASE}/providers/${id}`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					models: [{ id: 'mistral/mistral-large' }, { id: '' }]
				})
			})
			expect(empty.status).toBe(400)

			const duplicate = await fetch(`${BASE}/providers/${id}`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					models: [
						{ id: 'mistral/mistral-large' },
						{ id: 'mistral/mistral-large' }
					]
				})
			})
			expect(duplicate.status).toBe(400)

			const missing = await fetch(`${BASE}/providers/ghost`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ name: 'Ghost' })
			})
			expect(missing.status).toBe(404)
		})

		it('refuses to claim a model id another provider owns', async () => {
			// A custom id belongs to the provider that typed it first.
			const first = await fetch(`${BASE}/providers`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ name: 'One', baseUrl: 'https://one.dev/v1' })
			})
			const { id: one } = (await first.json()) as { id: string }
			await fetch(`${BASE}/providers/${one}`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ models: [{ id: 'one/m1' }] })
			})
			const second = await fetch(`${BASE}/providers`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ name: 'Two', baseUrl: 'https://two.dev/v1' })
			})
			const { id: two } = (await second.json()) as { id: string }
			const claim = await fetch(`${BASE}/providers/${two}`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ models: [{ id: 'one/m1' }] })
			})
			expect(claim.status).toBe(409)
			expect(await claim.json()).toEqual({
				message: 'Model "one/m1" already belongs to another provider'
			})

			// The rejected replace saved nothing.
			const listed = (await (await fetch(`${BASE}/providers`)).json()) as {
				providers: Array<{
					id: string
					models: Array<{ id: string; contextWindow?: number }>
				}>
			}
			expect(listed.providers.find((p) => p.id === two)?.models).toEqual([])
		})

		it('keeps stored model strings when the model or provider goes away', async () => {
			const created = await fetch(`${BASE}/providers`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({
					name: 'Pin Target',
					baseUrl: 'https://api.mistral.ai/v1'
				})
			})
			const { id } = (await created.json()) as { id: string }
			await fetch(`${BASE}/providers/${id}`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ models: [{ id: 'pinned/mistral-large' }] })
			})

			// A cron job on the superagent pins pinned/mistral-large.
			const pinned = (await (
				await fetch(`${BASE}/agent/_superagent/cron-jobs`, {
					method: 'POST',
					headers: { 'content-type': 'application/json' },
					body: JSON.stringify({
						time: '09:00',
						frequency: 'daily',
						model: 'pinned/mistral-large',
						prompt: 'digest'
					})
				})
			).json()) as { id: string }

			// Dropping the model leaves the stored string verbatim —
			// staleness is caught at call time, not by a sweep.
			const clear = await fetch(`${BASE}/providers/${id}`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ models: [] })
			})
			expect(clear.status).toBe(200)
			const afterDrop = (await (
				await fetch(`${BASE}/agent/_superagent/cron-jobs`)
			).json()) as { cronJobs: Array<{ id: string; model: string }> }
			expect(
				afterDrop.cronJobs.find((job) => job.id === pinned.id)?.model
			).toBe('pinned/mistral-large')

			const deleted = await fetch(`${BASE}/providers/${id}`, {
				method: 'DELETE'
			})
			expect(deleted.status).toBe(200)
			expect(await deleted.json()).toEqual({ message: 'Provider deleted' })
			const afterDelete = (await (
				await fetch(`${BASE}/agent/_superagent/cron-jobs`)
			).json()) as { cronJobs: Array<{ id: string; model: string }> }
			expect(
				afterDelete.cronJobs.find((job) => job.id === pinned.id)?.model
			).toBe('pinned/mistral-large')
			const gone = await fetch(`${BASE}/providers/${id}`, {
				method: 'DELETE'
			})
			expect(gone.status).toBe(404)
		})
	})

	describe('agent tool endpoints', () => {
		it('answers the seeded view: inherited tools, global switch shown', async () => {
			const body = (await (
				await fetch(`${BASE}/agent/research/tools`)
			).json()) as {
				tools: Array<{
					id: string
					globalEnabled: boolean
					locallyDisabled: boolean
				}>
				servers: Array<{
					serverId: string
					name: string
					agentEnabled: boolean
					serverEnabled: boolean
					catalogState: string
				}>
			}
			// The view row carries display fields too; the switches are
			// the contract under test. Research inherited everything: the
			// built-in is globally on, the seeded MCP tools are off, and no
			// row holds an opt-out.
			expect(
				body.tools.map(({ id, globalEnabled, locallyDisabled }) => ({
					id,
					globalEnabled,
					locallyDisabled
				}))
			).toEqual([
				{
					id: 'duckduckgo_search',
					globalEnabled: true,
					locallyDisabled: false
				},
				{
					id: 'notes-mcp:search_notes',
					globalEnabled: false,
					locallyDisabled: false
				},
				{
					id: 'notes-mcp:fetch_note',
					globalEnabled: false,
					locallyDisabled: false
				}
			])
			// Every server is listed with its gate state (absent = off) and
			// the global blockers the editor renders.
			expect(body.servers).toEqual([
				{
					serverId: 'notes-mcp',
					name: 'Notes MCP',
					agentEnabled: false,
					serverEnabled: true,
					catalogState: 'active'
				}
			])

			const unknownAgent = await fetch(`${BASE}/agent/ghost/tools`)
			expect(unknownAgent.status).toBe(404)
		})

		it('writes and clears the opt-out and validates the request', async () => {
			const off = await fetch(
				`${BASE}/agent/research/tools/duckduckgo_search`,
				{
					method: 'PATCH',
					headers: { 'content-type': 'application/json' },
					body: JSON.stringify({ enabled: false })
				}
			)
			expect(off.status).toBe(200)
			// The toggle answers the full saved listing, same as GET.
			const offBody = (await off.json()) as {
				tools: Array<{ id: string; locallyDisabled: boolean }>
			}
			expect(
				offBody.tools.find((t) => t.id === 'duckduckgo_search')?.locallyDisabled
			).toBe(true)

			const listed = (await (
				await fetch(`${BASE}/agent/research/tools`)
			).json()) as { tools: Array<{ id: string; locallyDisabled: boolean }> }
			expect(listed.tools[0].locallyDisabled).toBe(true)

			const backOn = await fetch(
				`${BASE}/agent/research/tools/duckduckgo_search`,
				{
					method: 'PATCH',
					headers: { 'content-type': 'application/json' },
					body: JSON.stringify({ enabled: true })
				}
			)
			const onBody = (await backOn.json()) as {
				tools: Array<{ id: string; locallyDisabled: boolean }>
			}
			expect(
				onBody.tools.find((t) => t.id === 'duckduckgo_search')?.locallyDisabled
			).toBe(false)

			const unknownTool = await fetch(`${BASE}/agent/research/tools/nope`, {
				method: 'PATCH',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ enabled: true })
			})
			expect(unknownTool.status).toBe(404)

			const missing = await fetch(
				`${BASE}/agent/research/tools/duckduckgo_search`,
				{
					method: 'PATCH',
					headers: { 'content-type': 'application/json' },
					body: JSON.stringify({})
				}
			)
			expect(missing.status).toBe(400)
		})

		it('lets a newly created agent inherit the built-ins', async () => {
			await fetch(`${BASE}/agent`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ name: 'Toolcheck', icon: 'bot' })
			})
			const body = (await (
				await fetch(`${BASE}/agent/toolcheck/tools`)
			).json()) as { tools: Array<{ locallyDisabled: boolean }> }
			// No per-agent row was written: the built-in is inherited.
			expect(body.tools[0].locallyDisabled).toBe(false)
		})
	})

	describe('mock agent MCP gate', () => {
		it('flips the gate only: the agent opt-out survives every toggle', async () => {
			const listing = async () =>
				(await (await fetch(`${BASE}/agent/research/tools`)).json()) as {
					tools: Array<{ id: string; locallyDisabled: boolean }>
					servers: Array<{ serverId: string; agentEnabled: boolean }>
				}
			const gate = (enabled: boolean) =>
				fetch(`${BASE}/agent/research/mcp/notes-mcp`, {
					method: 'PATCH',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ enabled })
				})

			const before = await listing()
			expect(
				before.servers.find((row) => row.serverId === 'notes-mcp')?.agentEnabled
			).toBe(false)

			// The gate opens: no per-tool row is written either way.
			const first = await gate(true)
			expect(first.status).toBe(200)
			const after = await listing()
			expect(
				after.servers.find((row) => row.serverId === 'notes-mcp')?.agentEnabled
			).toBe(true)
			for (const id of ['notes-mcp:search_notes', 'notes-mcp:fetch_note']) {
				expect(
					after.tools.find((tool) => tool.id === id)?.locallyDisabled
				).toBe(false)
			}

			// Opt out of one tool, off -> on: the opt-out survives.
			const optOut = await fetch(
				`${BASE}/agent/research/tools/notes-mcp:fetch_note`,
				{
					method: 'PATCH',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ enabled: false })
				}
			)
			expect(optOut.status).toBe(200)
			expect((await gate(false)).status).toBe(200)
			expect((await gate(true)).status).toBe(200)
			const preserved = await listing()
			expect(
				preserved.tools.find((tool) => tool.id === 'notes-mcp:fetch_note')
					?.locallyDisabled
			).toBe(true)
			expect(
				preserved.tools.find((tool) => tool.id === 'notes-mcp:search_notes')
					?.locallyDisabled
			).toBe(false)
		})

		it('rejects an unknown server and a missing switch', async () => {
			const unknown = await fetch(`${BASE}/agent/research/mcp/ghost`, {
				method: 'PATCH',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ enabled: true })
			})
			expect(unknown.status).toBe(404)

			const missing = await fetch(`${BASE}/agent/research/mcp/notes-mcp`, {
				method: 'PATCH',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({})
			})
			expect(missing.status).toBe(400)
		})
	})
})

// The mock mirrors the backend's policy rules so the UI exercises them
// without a server.
describe('mock API MCP policy', () => {
	// The definition is read through the per-tool endpoint; the catalog
	// list must not ship the stored JSON on every row.
	it('lists tools without their stored definitions', async () => {
		const body = (await (await fetch(`${BASE}/tools`)).json()) as {
			tools: Record<string, unknown>[]
		}
		expect(body.tools.length).toBeGreaterThan(0)
		for (const tool of body.tools) {
			expect(tool).not.toHaveProperty('definitionJson')
		}
	})

	it('masks userinfo in every rendered endpoint, like the backend', async () => {
		const created = await fetch(`${BASE}/mcp/servers`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				name: 'Userinfo Test',
				url: 'http://u:secret-pass@127.0.0.1:3333/mcp',
				allowPrivateNetwork: true,
				allowPlaintext: true
			})
		})
		expect(created.status).toBe(201)
		const { server: row } = (await created.json()) as {
			server: { id: string; url: string }
		}
		expect(row.url).toContain('%5Bredacted%5D')
		expect(row.url).not.toContain('secret-pass')

		const listed = (await (await fetch(`${BASE}/mcp/servers`)).json()) as {
			servers: { id: string; url?: string }[]
		}
		const view = listed.servers.find((s) => s.id === row.id)?.url ?? ''
		expect(view).toContain('%5Bredacted%5D')
		expect(view).not.toContain('secret-pass')

		// The definition endpoint rides the same helper; its no-userinfo
		// pass-through is pinned by the stored-definition test below.
	})

	it('clears the destination approvals when the URL changes', async () => {
		const created = await fetch(`${BASE}/mcp/servers`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				name: 'Approval Test',
				url: 'http://127.0.0.1:3333/mcp',
				allowPrivateNetwork: true,
				allowPlaintext: true
			})
		})
		expect(created.status).toBe(201)
		const { server: row } = (await created.json()) as {
			server: {
				id: string
				allowPrivateNetwork: boolean
				allowPlaintext: boolean
			}
		}
		expect(row.allowPrivateNetwork).toBe(true)

		const patched = await fetch(`${BASE}/mcp/servers/${row.id}`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ url: 'https://mcp.example.com/mcp' })
		})
		expect(patched.status).toBe(200)
		const after = (await patched.json()) as {
			server: { allowPrivateNetwork: boolean; allowPlaintext: boolean }
		}
		expect(after.server.allowPrivateNetwork).toBe(false)
		expect(after.server.allowPlaintext).toBe(false)
	})

	// Validate-then-mutate: a patch that fails validation must not
	// flip the switch — the 400 answers before any row state changes.
	it('leaves enabled untouched when a patch fails validation', async () => {
		const created = await fetch(`${BASE}/mcp/servers`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				name: 'Validate Flip',
				url: 'http://127.0.0.1:3333/mcp'
			})
		})
		expect(created.status).toBe(201)
		const { server: row } = (await created.json()) as {
			server: { id: string }
		}

		const rejected = await fetch(`${BASE}/mcp/servers/${row.id}`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ enabled: false, name: 'Bad_Name!' })
		})
		expect(rejected.status).toBe(400)

		const listing = (await (await fetch(`${BASE}/mcp/servers`)).json()) as {
			servers: { id: string; enabled: boolean }[]
		}
		expect(listing.servers.find((s) => s.id === row.id)?.enabled).toBe(true)
	})

	it('reaches a gated agent on a global enable, except where it opted out', async () => {
		// scribe uses the notes server and explicitly disabled fetch_note.
		await fetch(`${BASE}/tools/notes-mcp:fetch_note`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ enabled: true })
		})
		await fetch(`${BASE}/tools/notes-mcp:search_notes`, {
			method: 'PATCH',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ enabled: true })
		})
		const body = (await (await fetch(`${BASE}/agent/scribe/tools`)).json()) as {
			tools: Array<{
				id: string
				globalEnabled: boolean
				locallyDisabled: boolean
			}>
		}
		expect(
			body.tools.find((tool) => tool.id === 'notes-mcp:search_notes')
		).toMatchObject({ globalEnabled: true, locallyDisabled: false })
		expect(
			body.tools.find((tool) => tool.id === 'notes-mcp:fetch_note')
		).toMatchObject({ globalEnabled: true, locallyDisabled: true })
	})

	it('serves the stored definition and the authority behind it', async () => {
		const res = await fetch(
			`${BASE}/mcp/servers/notes-mcp/tools/notes-mcp:search_notes/definition`
		)
		expect(res.status).toBe(200)
		const body = (await res.json()) as {
			definitionJson: string
			endpoint: string
			hasCredentials: boolean
			allowPlaintext: boolean
		}
		expect(JSON.parse(body.definitionJson).name).toBe('search_notes')
		// The inspector needs the endpoint and whether the connection
		// carries credentials or rides plaintext.
		expect(body.endpoint).toBe('http://127.0.0.1:3333/mcp')
		expect(body.hasCredentials).toBe(true)
		expect(body.allowPlaintext).toBe(true)

		const missing = await fetch(
			`${BASE}/mcp/servers/notes-mcp/tools/duckduckgo_search/definition`
		)
		expect(missing.status).toBe(404)
	})

	it("answers the definition's 404s in the backend's order", async () => {
		const noServer = await fetch(
			`${BASE}/mcp/servers/ghost/tools/notes-mcp:search_notes/definition`
		)
		expect(noServer.status).toBe(404)
		expect(((await noServer.json()) as { message: string }).message).toBe(
			'MCP server "ghost" not found'
		)

		const noTool = await fetch(
			`${BASE}/mcp/servers/notes-mcp/tools/ghost:tool/definition`
		)
		expect(noTool.status).toBe(404)
		expect(((await noTool.json()) as { message: string }).message).toBe(
			'Tool "ghost:tool" not found'
		)

		const wrongOwner = await fetch(
			`${BASE}/mcp/servers/notes-mcp/tools/duckduckgo_search/definition`
		)
		expect(wrongOwner.status).toBe(404)
		expect(((await wrongOwner.json()) as { message: string }).message).toBe(
			'Tool "duckduckgo_search" not found on server "notes-mcp"'
		)
	})

	it('reports the cosmetic skips of a discovery pass', async () => {
		const created = await fetch(`${BASE}/mcp/servers`, {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({
				name: 'Skip Sentinel',
				url: 'http://127.0.0.1:3333/skip',
				allowPrivateNetwork: true,
				allowPlaintext: true
			})
		})
		expect(created.status).toBe(201)
		const body = (await created.json()) as {
			server: { id: string }
			skippedTools?: { name: string; reason: string }[]
		}
		expect(body.skippedTools).toHaveLength(1)
		expect(body.skippedTools?.[0].reason).toMatch(/exceeds 64 characters/)

		const refreshed = await fetch(
			`${BASE}/mcp/servers/${body.server.id}/refresh`,
			{ method: 'POST' }
		)
		const after = (await refreshed.json()) as {
			skippedTools?: { name: string; reason: string }[]
		}
		expect(after.skippedTools).toHaveLength(1)

		// A server without the sentinel reports no skips at all.
		const clean = await fetch(`${BASE}/mcp/servers/notes-mcp/refresh`, {
			method: 'POST'
		})
		const cleanBody = (await clean.json()) as { skippedTools?: unknown[] }
		expect(cleanBody.skippedTools).toBeUndefined()
	})
})
