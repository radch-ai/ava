/**
 * Mock API handlers
 *
 * MSW handlers implementing the backend contract consumed by
 * `src/lib/services`. Mounted by the vite dev/preview servers when
 * `VITE_USE_MOCKS=true` and reused by unit tests via `msw/node`.
 */

import { type HttpHandler, HttpResponse, http } from 'msw'
import type { IconName } from '../icons'
import type { CursorPage } from '../services/utils/cursor'
import type { Agent, AgentConfiguration } from '../types/agent'
import type { MockStore } from './data'

interface CursorItem {
	id: string
}

/**
 * Slice a newest-first list into a cursor page. The cursor is the id of the
 * last item of the previous page; the next page starts below it.
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
	return [
		http.get('*/api/user/agents', () =>
			HttpResponse.json({ agents: store.agents })
		),

		http.post('*/api/auth/exchange', async ({ request }) => {
			const { token } = (await request.json()) as { token: string }
			if (token !== store.token) {
				return HttpResponse.json({ message: 'Invalid token' }, { status: 401 })
			}
			return HttpResponse.json(
				{ user: store.user },
				{
					headers: {
						'Set-Cookie':
							'ava_session=mock-session; Path=/; HttpOnly; SameSite=Strict'
					}
				}
			)
		}),

		http.get('*/api/auth/me', ({ request }) =>
			request.headers.get('cookie')?.includes('ava_session=')
				? HttpResponse.json({ user: store.user })
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

		http.get('*/api/user/superagent-thread', ({ request }) => {
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

		http.get('*/api/user/agent/:name/activity', ({ request, params }) => {
			const url = new URL(request.url)
			const items = store.activity.get(String(params.name)) ?? []
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

		http.get('*/api/user/agent/:name', ({ params }) => {
			const name = String(params.name)
			const configuration = store.configurations.get(name)
			if (!configuration) {
				return HttpResponse.json(
					{ message: `Agent "${name}" not found` },
					{ status: 404 }
				)
			}
			return HttpResponse.json(configuration)
		}),

		http.patch('*/api/user/agent/:name', async ({ request, params }) => {
			const name = String(params.name)
			const current = store.configurations.get(name)
			if (!current) {
				return HttpResponse.json(
					{ message: `Agent "${name}" not found` },
					{ status: 404 }
				)
			}
			const patch = (await request.json()) as Partial<AgentConfiguration>
			const next: AgentConfiguration = {
				...current,
				...patch,
				soul: { ...current.soul, ...patch.soul }
			}
			store.configurations.set(name, next)
			return HttpResponse.json(next)
		}),

		http.post('*/api/user/agent/:name', async ({ request, params }) => {
			const name = String(params.name)
			if (store.configurations.has(name)) {
				return HttpResponse.json(
					{ message: `Agent "${name}" already exists` },
					{ status: 409 }
				)
			}
			const configuration = (await request.json()) as AgentConfiguration
			store.configurations.set(name, configuration)
			const agent: Agent = {
				name,
				label: configuration.name || name,
				icon: (configuration.icon ?? 'bot') as IconName
			}
			store.agents.push(agent)
			return HttpResponse.json({ name }, { status: 201 })
		})
	]
}
