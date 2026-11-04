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
			heartbeat: store.heartbeats.get(id) ?? {
				model: '',
				enabled: false,
				duration: '1hr',
				content: ''
			},
			cronJobs: store.cronJobs.get(id) ?? []
		}
	}
	return [
		// The list mirrors the backend query: it never includes the system
		// superagent. Per-id endpoints (config, thread) resolve it separately.

		http.get('*/api/agents', () => HttpResponse.json({ agents: store.agents })),

		http.post('*/api/auth/exchange', async ({ request }) => {
			const { token } = (await request.json()) as { token: string }
			if (token !== store.token) {
				return HttpResponse.json({ message: 'Invalid token' }, { status: 401 })
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
				: HttpResponse.json({ message: 'No session' }, { status: 401 })
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
				return HttpResponse.json(
					{ message: `Agent "${String(params.id)}" has no thread` },
					{ status: 404 }
				)
			}
			const url = new URL(request.url)
			const cursor = url.searchParams.get('cursor')
			if (cursor && !store.thread.some((message) => message.id === cursor)) {
				return HttpResponse.json({ message: 'Invalid cursor' }, { status: 400 })
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
				return HttpResponse.json({ message: 'Invalid cursor' }, { status: 400 })
			}
			return HttpResponse.json(
				paginate(
					items,
					Math.max(1, Number(url.searchParams.get('limit')) || 20),
					cursor
				)
			)
		}),

		http.get('*/api/agent/:id', ({ params }) => {
			const id = String(params.id)
			const detail = buildDetail(id)
			if (!detail) {
				return HttpResponse.json(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			return HttpResponse.json(detail)
		}),

		http.patch('*/api/agent/:id', async ({ request, params }) => {
			const id = String(params.id)
			const current = store.configurations.get(id)
			if (!current) {
				return HttpResponse.json(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			// name/icon update the list entry, text updates the config;
			// heartbeat/cronJobs have their own endpoints.
			const {
				name,
				icon,
				soul,
				heartbeat: _hb,
				cronJobs: _cron,
				...configText
			} = (await request.json()) as Partial<AgentConfig> & {
				name?: string
				icon?: string
			}
			const next: Omit<AgentConfig, 'heartbeat' | 'cronJobs'> = {
				...current,
				...configText,
				soul: { ...current.soul, ...soul }
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
				return HttpResponse.json(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const heartbeat = (await request.json()) as HeartbeatConfig
			store.heartbeats.set(id, heartbeat)
			return HttpResponse.json(buildDetail(id))
		}),

		// The cron list: replaced wholesale on each save.
		http.patch('*/api/agent/:id/cron-jobs', async ({ request, params }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const { cronJobs } = (await request.json()) as { cronJobs: CronJob[] }
			store.cronJobs.set(id, cronJobs)
			return HttpResponse.json(buildDetail(id))
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
				return HttpResponse.json(
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
				return HttpResponse.json(
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
