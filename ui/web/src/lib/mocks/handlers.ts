/**
 * Mock API handlers
 *
 * MSW handlers implementing the backend contract consumed by
 * `src/lib/services`. Mounted by the vite dev/preview servers when
 * `VITE_USE_MOCKS=true` and reused by unit tests via `msw/node`.
 */

import { type HttpHandler, HttpResponse, http } from 'msw'
import type { AgentIconName } from '../../components/agent-icons/index.ts'
import { SUPERAGENT_AGENT_ID } from '../config.ts'
import type { CursorPage } from '../services/utils/cursor.ts'
import {
	type AgentConfig,
	type AgentConfigWithIdentity,
	type AgentIdentity,
	type CronJob,
	createDefaultHeartbeatConfig,
	type ErrorResponse,
	type GetAgentsResponse,
	type HeartbeatConfig,
	SUPERAGENT_AGENT
} from '../types/agent.ts'
import { deriveAgentId, isValidAgentName } from './agent-id.ts'
import type { MockStore } from './data.ts'

interface CursorItem {
	id: string
}

/**
 * Slice a newest-first list into a cursor page. The cursor is the id of
 * the last item of the previous page; the next page starts below it.
 */
function paginate<T extends CursorItem>(
	items: T[],
	limit: number,
	cursor: string | null
): CursorPage<T> {
	const start = cursor ? items.findIndex((item) => item.id === cursor) + 1 : 0
	const slice = items.slice(start, start + limit)
	return {
		items: slice,
		nextCursor: start + limit < items.length ? (slice.at(-1)?.id ?? null) : null
	}
}

/**
 * Apply the row rule the backend enforces: cronExpression is carried
 * only while the frequency is 'custom'; any other frequency clears it.
 */
function normalizeCronJob(job: CronJob): CronJob {
	return {
		...job,
		cronExpression: job.frequency === 'custom' ? job.cronExpression : undefined
	}
}

/**
 * Build handlers over the given store. Handlers close over the store, so
 * each consumer gets isolated state.
 */
export function createHandlers(store: MockStore): HttpHandler[] {
	// AgentConfigWithIdentity. heartbeat is the saved row or the
	// disabled default; name/icon come from the list entry (the
	// constant covers the never-listed superagent).
	const buildDetail = (id: string): AgentConfigWithIdentity | null => {
		const config = store.configurations.get(id)
		if (!config) return null
		const { name, icon } =
			store.agents.find((agent) => agent.id === id) ?? SUPERAGENT_AGENT
		return {
			id,
			name,
			icon,
			...config,
			heartbeat: store.heartbeats.get(id) ?? createDefaultHeartbeatConfig()
		}
	}
	return [
		// The list mirrors the backend query: it never includes the system
		// superagent. Per-id endpoints (config, thread) resolve it separately.

		http.get('*/api/agents', () =>
			HttpResponse.json<GetAgentsResponse>({ agents: store.agents })
		),

		http.post('*/api/auth/exchange', async ({ request }) => {
			const { token } = (await request.json()) as { token: string }
			if (token !== store.token) {
				return HttpResponse.json<ErrorResponse>(
					{ message: 'Invalid token' },
					{ status: 401 }
				)
			}
			return new HttpResponse(null, {
				status: 200,
				headers: {
					'Set-Cookie':
						'ava_session=mock-session; Path=/; HttpOnly; SameSite=Strict'
				}
			})
		}),

		http.get('*/api/auth/me', ({ request }) =>
			request.headers.get('cookie')?.includes('ava_session=')
				? new HttpResponse(null, { status: 200 })
				: HttpResponse.json<ErrorResponse>(
						{ message: 'No session' },
						{ status: 401 }
					)
		),

		http.post(
			'*/api/auth/logout',
			() =>
				new HttpResponse(null, {
					status: 204,
					headers: {
						'Set-Cookie':
							'ava_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Strict'
					}
				})
		),

		// Per-agent thread. Only the superagent has a thread today; other
		// agents 404 until threads are enabled for them.
		http.get('*/api/agent/:id/thread', ({ request, params }) => {
			if (String(params.id) !== SUPERAGENT_AGENT_ID) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${String(params.id)}" has no thread` },
					{ status: 404 }
				)
			}
			const url = new URL(request.url)
			const cursor = url.searchParams.get('cursor')
			if (cursor && !store.thread.some((message) => message.id === cursor)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: 'Invalid cursor' },
					{ status: 400 }
				)
			}
			return HttpResponse.json(
				paginate(
					store.thread,
					Math.max(1, Number(url.searchParams.get('limit')) || 20),
					cursor
				)
			)
		}),

		http.get('*/api/agent/:id/activity', ({ request, params }) => {
			const url = new URL(request.url)
			const items = store.activity.get(String(params.id)) ?? []
			const cursor = url.searchParams.get('cursor')
			if (cursor && !items.some((item) => item.id === cursor)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: 'Invalid cursor' },
					{ status: 400 }
				)
			}
			return HttpResponse.json(
				paginate(
					items,
					Math.max(1, Number(url.searchParams.get('limit')) || 20),
					cursor
				)
			)
		}),

		http.get('*/api/agent/:id/config', ({ params }) => {
			const id = String(params.id)
			const detail = buildDetail(id)
			if (!detail) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			return HttpResponse.json(detail)
		}),

		http.patch('*/api/agent/:id/config', async ({ request, params }) => {
			const id = String(params.id)
			const current = store.configurations.get(id)
			if (!current) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			// name/icon update the list entry, text updates the config;
			// heartbeat has its own endpoint.
			const {
				name,
				icon,
				soul,
				heartbeat: _hb,
				...configText
			} = (await request.json()) as Partial<AgentConfig> &
				Partial<Pick<AgentIdentity, 'name' | 'icon'>>
			const next: Omit<AgentConfig, 'heartbeat'> = {
				...current,
				...configText,
				...(soul !== undefined && { soul })
			}
			store.configurations.set(id, next)
			// Keep the list entry in sync so refetches reflect renames/icon
			// changes, mirroring the real backend's list behavior.
			store.agents = store.agents.map((agent) =>
				agent.id === id
					? {
							...agent,
							...(name !== undefined && { name }),
							...(icon !== undefined && { icon: icon as AgentIconName })
						}
					: agent
			)
			return HttpResponse.json(buildDetail(id))
		}),

		// The heartbeat row: one per agent, written whole.
		http.patch('*/api/agent/:id/heartbeat', async ({ request, params }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const heartbeat = (await request.json()) as HeartbeatConfig
			store.heartbeats.set(id, heartbeat)
			return HttpResponse.json(buildDetail(id))
		}),

		// Cron jobs: per-row endpoints, fetched lazily — the list only
		// loads when the Cron Jobs panel opens. cronExpression survives
		// only while the frequency is 'custom', the same row rule the
		// backend enforces.
		http.get('*/api/agent/:id/cron-jobs', ({ params }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			return HttpResponse.json({ cronJobs: store.cronJobs.get(id) ?? [] })
		}),

		http.post('*/api/agent/:id/cron-jobs', async ({ request, params }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const body = (await request.json()) as CronJob
			const stored = normalizeCronJob({ ...body, id: crypto.randomUUID() })
			const jobs = store.cronJobs.get(id) ?? []
			store.cronJobs.set(id, [...jobs, stored])
			return HttpResponse.json(stored, { status: 201 })
		}),

		http.patch(
			'*/api/agent/:id/cron-jobs/:jobId',
			async ({ request, params }) => {
				const id = String(params.id)
				if (!store.configurations.has(id)) {
					return HttpResponse.json<ErrorResponse>(
						{ message: `Agent "${id}" not found` },
						{ status: 404 }
					)
				}
				const jobId = String(params.jobId)
				const jobs = store.cronJobs.get(id) ?? []
				if (!jobs.some((job) => job.id === jobId)) {
					return HttpResponse.json<ErrorResponse>(
						{ message: `Cron job "${jobId}" not found` },
						{ status: 404 }
					)
				}
				const job = (await request.json()) as CronJob
				const stored = normalizeCronJob({ ...job, id: jobId })
				store.cronJobs.set(
					id,
					jobs.map((existing) => (existing.id === jobId ? stored : existing))
				)
				return HttpResponse.json(stored)
			}
		),

		http.delete('*/api/agent/:id/cron-jobs/:jobId', ({ params }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const jobId = String(params.jobId)
			const jobs = store.cronJobs.get(id) ?? []
			if (!jobs.some((job) => job.id === jobId)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Cron job "${jobId}" not found` },
					{ status: 404 }
				)
			}
			store.cronJobs.set(
				id,
				jobs.filter((job) => job.id !== jobId)
			)
			return new HttpResponse(null, { status: 204 })
		}),

		// Create: the server owns id derivation. Invalid names are rejected
		// with 400, colliding derived ids with 409 — never auto-suffixed.
		http.post('*/api/agent', async ({ request }) => {
			const body = (await request.json()) as Partial<
				Pick<AgentIdentity, 'name' | 'icon'>
			> &
				AgentConfig
			const name = body.name ?? ''
			if (!isValidAgentName(name)) {
				return HttpResponse.json<ErrorResponse>(
					{
						message:
							'Invalid agent name: use letters, digits and single spaces only'
					},
					{ status: 400 }
				)
			}

			const id = deriveAgentId(name)
			if (
				store.configurations.has(id) ||
				store.agents.some((agent) => agent.id === id)
			) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `An agent named "${name}" already exists` },
					{ status: 409 }
				)
			}

			// Create ignores heartbeat/cronJobs like the backend; they
			// have their own endpoints.
			store.configurations.set(id, {
				soul: body.soul,
				operatingInstructions: body.operatingInstructions,
				toolsContext: body.toolsContext,
				toolsEnabled: body.toolsEnabled ?? []
			})
			const agent: AgentIdentity = {
				id,
				name,
				icon: body.icon as AgentIconName
			}
			store.agents.push(agent)
			return HttpResponse.json({ id }, { status: 201 })
		})
	]
}
