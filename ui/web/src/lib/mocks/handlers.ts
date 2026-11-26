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
import type {
	CreateMcpServerRequest,
	McpDiscoveredTool,
	McpServer,
	McpSkippedTool,
	UpdateMcpServerRequest
} from '../types/mcp.ts'
import type {
	AgentMcpPreference,
	AgentTool,
	GetToolsResponse,
	ToggleAgentToolRequest,
	ToggleToolRequest,
	Tool
} from '../types/tool.ts'
import { deriveAgentId, isValidAgentName } from './agent-id.ts'
import {
	type McpServerRow,
	type MockStore,
	PROVIDER_SEED_DATA,
	type ProviderModelRow,
	type ProviderRow,
	type ToolRow
} from './data.ts'

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

	// The provider catalog answers like the backend: the stored apiKey
	// never leaves in a response (hasKey is the field); rows carry
	// custom model ids only.
	const providerView = (row: ProviderRow) => ({
		id: row.id,
		name: row.name,
		baseUrl: row.baseUrl,
		hasKey: row.apiKey !== '',
		models: [...row.models].sort((a, b) => a.id.localeCompare(b.id))
	})

	// System models for a row: the template's deterministic list for
	// a well-known base URL (mock stand-in for the live fetch), []
	// otherwise.
	const systemModelsFor = (row: Pick<ProviderRow, 'baseUrl'>) =>
		PROVIDER_SEED_DATA.find((seed) => seed.baseUrl === row.baseUrl)
			?.systemModels ?? []

	// The save-time connection check, deterministic in mock mode: the
	// sentinel key fails, everything else passes — keyed or keyless,
	// mirroring the backend's always-probe save.
	const connectionCheck = (apiKey: string) =>
		apiKey === 'bad-key'
			? HttpResponse.json(
					{
						message:
							'Connection check failed: endpoint rejected the request (401) — key missing or invalid'
					},
					{ status: 400 }
				)
			: null

	// --- MCP servers ----------------------------------------------

	// The backend hides userinfo in every endpoint it renders; the mock
	// mirrors it so a userinfo leak cannot hide behind mock mode.
	const maskedUrl = (raw: string): string => {
		try {
			const u = new URL(raw)
			if (u.username === '' && u.password === '') return raw
			u.username = '[redacted]'
			u.password = '[redacted]'
			return u.toString()
		} catch {
			return raw
		}
	}

	// Wire view of one server row: key names only, the tool count
	// derived from the catalog.
	const serverView = (row: McpServerRow): McpServer => ({
		id: row.id,
		name: row.name,
		...(row.url ? { url: maskedUrl(row.url) } : {}),
		headerKeys: row.headerKeys,
		...(row.timeoutMs ? { timeoutMs: row.timeoutMs } : {}),
		enabled: row.enabled,
		toolCount: store.tools.filter((tool) => tool.serverId === row.id).length,
		catalogState: row.catalogState,
		allowPrivateNetwork: row.allowPrivateNetwork,
		allowPlaintext: row.allowPlaintext,
		...(row.lastError ? { lastError: row.lastError } : {})
	})

	// The agent tool listing's server rows: id, display name, this
	// agent's gate and the global blockers — the panel's whole server
	// surface, so it never needs GET /api/mcp/servers.
	const agentServerView = (
		row: McpServerRow,
		gates: Map<string, boolean>
	): AgentMcpPreference => ({
		serverId: row.id,
		name: row.name,
		agentEnabled: gates.get(row.id) ?? false,
		serverEnabled: row.enabled,
		catalogState: row.catalogState
	})

	const discoveredFor = (slug: string): McpDiscoveredTool[] =>
		store.tools
			.filter((tool) => tool.serverId === slug)
			.map((tool) => ({
				id: tool.id,
				name: tool.name,
				description: tool.description
			}))

	// Cosmetic skips: a name the provider wire contract cannot carry is
	// reported with a reason, never renamed or silently dropped. The
	// "skip" URL sentinel makes one discovery pass produce one.
	const skippedFor = (url: string): McpSkippedTool[] =>
		url.includes('skip')
			? [
					{
						name: 'oversized_discovery_tool_name_that_exceeds_the_provider_limit',
						reason:
							'model name "oversized_discovery_tool_name_that_exceeds_the_provider_limit" exceeds 64 characters'
					}
				]
			: []

	// The one URL predicate: an absolute http(s) URL, answering with
	// the backend's own message. It deliberately stops at the scheme —
	// the mock admits userinfo so render-side masking has rows to mask
	// (the backend rejects credentials at validation) — and create and
	// patch are its only callers.
	const validateServerUrl = (url: string): string | null =>
		/^https?:\/\/.+/.test(url)
			? null
			: 'url must be an http(s) URL without embedded credentials'

	// The listing a save, refresh or install answers: the saved row, the
	// server's catalog rows, and any cosmetic skips.
	const listingResponse = (row: McpServerRow, status = 200) => {
		const skipped = skippedFor(row.url ?? '')
		return HttpResponse.json(
			{
				server: serverView(row),
				tools: discoveredFor(row.id),
				...(skipped.length ? { skippedTools: skipped } : {})
			},
			{ status }
		)
	}

	// A created mock server deterministically reports one echo tool;
	// a re-verified server re-reports it. Discovery installs it globally
	// off, like the backend's UpsertMcpTool.
	const seedServerTools = (slug: string): void => {
		store.tools.push({
			id: `${slug}:echo`,
			name: 'echo',
			description: 'Echoes its input text.',
			enabled: false,
			kind: 'mcp',
			serverId: slug,
			definitionJson: JSON.stringify({
				name: 'echo',
				description: 'Echoes its input text.',
				inputSchema: {
					type: 'object',
					properties: { text: { type: 'string' } },
					required: ['text']
				}
			})
		})
	}

	// Deleting a server's tools cascades every agent's opt-out.
	const dropServerTools = (slug: string): void => {
		const dropped = store.tools
			.filter((tool) => tool.serverId === slug)
			.map((tool) => tool.id)
		store.tools = store.tools.filter((tool) => tool.serverId !== slug)
		for (const disabled of store.agentDisabledTools.values())
			for (const id of dropped) disabled.delete(id)
	}

	// A re-sync keeps still-reported tools and their switches: the mock's
	// healthy listing is the fixed "echo" set, so reconcile upserts it and
	// prunes only rows the listing omits — the backend's UpsertMcpTool
	// ON CONFLICT / DeleteVanishedMcpTools semantics.
	const reconcileServerTools = (slug: string): void => {
		const kept = `${slug}:echo`
		const dropped = store.tools
			.filter((tool) => tool.serverId === slug && tool.id !== kept)
			.map((tool) => tool.id)
		store.tools = store.tools.filter((tool) => !dropped.includes(tool.id))
		for (const disabled of store.agentDisabledTools.values())
			for (const id of dropped) disabled.delete(id)
		if (!store.tools.some((tool) => tool.id === kept)) seedServerTools(slug)
	}

	// The catalog row as the wire carries it: the stored definition is
	// read through the per-tool endpoint, never in the list.
	const toolView = (row: ToolRow): Tool => ({
		id: row.id,
		name: row.name,
		description: row.description,
		enabled: row.enabled,
		kind: row.kind,
		...(row.serverId ? { serverId: row.serverId } : {})
	})

	// One agent's view of a tool: the global switch plus its own opt-out.
	const agentToolView = (
		tool: ToolRow,
		disabled: ReadonlySet<string>
	): AgentTool => ({
		id: tool.id,
		name: tool.name,
		description: tool.description,
		globalEnabled: tool.enabled,
		locallyDisabled: disabled.has(tool.id),
		kind: tool.kind,
		...(tool.serverId ? { serverId: tool.serverId } : {})
	})

	// The agent tool listing: this agent's opt-outs and gate states over
	// the whole catalog and server set.
	const agentListing = (id: string) => {
		const disabled = store.agentDisabledTools.get(id) ?? new Set<string>()
		const gates = store.agentMcpGates.get(id) ?? new Map<string, boolean>()
		return {
			tools: store.tools.map((tool) => agentToolView(tool, disabled)),
			servers: store.mcpServers.map((server) => agentServerView(server, gates))
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

		// Per-agent thread, matching the backend: one thread per page,
		// the no-cursor read starting at the active one, and the
		// cursor walking inside a thread and across its predecessor
		// boundary. Known agents serve their thread page (only the
		// superagent has messages today; others page empty), unknown
		// agents 404.
		http.get('*/api/agent/:id/thread', ({ request, params }) => {
			const url = new URL(request.url)
			const id = String(params.id)
			const known =
				id === SUPERAGENT_AGENT_ID ||
				store.agents.some((agent) => agent.id === id)
			if (!known) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const threads = store.threads.get(id) ?? []
			const active = threads.at(-1)
			// The backend clamps a page to 100 and floors it at 1.
			const limit = Math.min(
				100,
				Math.max(1, Number(url.searchParams.get('limit')) || 20)
			)
			const raw = url.searchParams.get('cursor')

			let thread = active
			let anchorId = ''
			if (raw) {
				const cut = raw.indexOf(':')
				if (cut < 0) {
					return HttpResponse.json<ErrorResponse>(
						{ message: 'Invalid cursor' },
						{ status: 400 }
					)
				}
				const threadId = raw.slice(0, cut)
				anchorId = raw.slice(cut + 1)
				thread = threads.find((candidate) => candidate.id === threadId)
				if (!thread) {
					return HttpResponse.json<ErrorResponse>(
						{ message: 'Invalid cursor' },
						{ status: 400 }
					)
				}
				if (
					anchorId &&
					!thread.messages.some((message) => message.id === anchorId)
				) {
					return HttpResponse.json<ErrorResponse>(
						{ message: 'Invalid cursor' },
						{ status: 400 }
					)
				}
			}
			if (!thread) {
				// Every agent owns a thread from creation, so a known
				// agent without a stored row still answers under that
				// agent's own thread identity — never an empty id.
				return HttpResponse.json({
					items: [],
					nextCursor: null,
					threadId: `${id}-t1`
				})
			}

			// Newest-first storage: the page starts below the anchor.
			const start = anchorId
				? thread.messages.findIndex((message) => message.id === anchorId) + 1
				: 0
			const window = thread.messages.slice(start, start + limit + 1)
			const hasOlder = window.length > limit
			const rows = hasOlder ? window.slice(0, limit) : window

			// The wire answers oldest→newest.
			const items = [...rows].reverse()
			let nextCursor: string | null = null
			if (hasOlder) {
				nextCursor = `${thread.id}:${rows[rows.length - 1].id}`
			} else if (thread.previousId) {
				// The thread is exhausted: its boundary leads the page,
				// then the predecessor's newest page follows.
				items.unshift({
					id: thread.id,
					role: 'divider',
					content: 'New session',
					createdAt: thread.createdAt
				} as (typeof items)[number])
				nextCursor = `${thread.previousId}:`
			}
			return HttpResponse.json({ items, nextCursor, threadId: thread.id })
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
				toolsContext: body.toolsContext
			})
			const agent: AgentIdentity = {
				id,
				name,
				icon: body.icon as AgentIconName
			}
			store.agents.push(agent)
			// A new agent inherits the catalog: no per-tool rows are
			// written, and its own empty thread is seeded like the backend
			// does in the creation transaction.
			store.agentDisabledTools.set(id, new Set())
			store.threads.set(id, [
				{
					id: `${id}-t1`,
					previousId: null,
					createdAt: new Date().toISOString(),
					messages: []
				}
			])
			return HttpResponse.json({ id }, { status: 201 })
		}),

		// The provider catalog. GET answers the merged view; writes:
		// opaque uuid ids (nothing derives from the name), custom
		// model ids as typed, and keyed saves pass a connection check
		// (the sentinel key 'bad-key' fails it deterministically).
		http.get('*/api/providers', () =>
			HttpResponse.json({
				providers: store.providers.map(providerView)
			})
		),

		// Merged catalog: starters' system lists plus custom ids.
		http.get('*/api/models', () => {
			const names: string[] = []
			const grouped: Record<
				string,
				Array<{ id: string; providerId: string }>
			> = {}
			for (const row of store.providers) {
				const seen = new Set<string>()
				const ids: string[] = []
				for (const id of [
					...row.models.map((m) => m.id),
					...systemModelsFor(row)
				]) {
					if (seen.has(id)) continue
					seen.add(id)
					ids.push(id)
				}
				if (ids.length === 0) continue
				const entries = ids
					.map((id) => ({ id, providerId: row.id }))
					.sort((a, b) => a.id.localeCompare(b.id))
				const existing = grouped[row.name]
				if (existing) {
					grouped[row.name] = [...existing, ...entries].sort((a, b) =>
						a.id.localeCompare(b.id)
					)
				} else {
					names.push(row.name)
					grouped[row.name] = entries
				}
			}
			return HttpResponse.json({
				groups: names.map((name) => ({
					providerName: name,
					models: grouped[name]
				}))
			})
		}),

		// One provider's system model ids, for the settings-page expand.
		http.get('*/api/providers/:id/models', ({ params }) => {
			const id = String(params.id)
			const row = store.providers.find((provider) => provider.id === id)
			if (!row) {
				return HttpResponse.json(
					{ message: `Provider "${id}" not found` },
					{ status: 404 }
				)
			}
			return HttpResponse.json({
				systemModels: systemModelsFor(row)
			})
		}),

		// The tool catalog: the whole list, then the global switch.
		http.get('*/api/tools', () =>
			HttpResponse.json<GetToolsResponse>({
				tools: store.tools.map(toolView)
			})
		),

		http.patch('*/api/tools/:id', async ({ params, request }) => {
			const id = String(params.id)
			const row = store.tools.find((tool) => tool.id === id)
			if (!row) {
				return HttpResponse.json(
					{ message: `Tool "${id}" not found` },
					{ status: 404 }
				)
			}
			const body = (await request.json()) as ToggleToolRequest
			if (typeof body.enabled !== 'boolean') {
				return HttpResponse.json(
					{ message: 'enabled is required' },
					{ status: 400 }
				)
			}
			row.enabled = body.enabled
			return HttpResponse.json(toolView(row))
		}),

		http.get('*/api/mcp/servers', () =>
			HttpResponse.json({ servers: store.mcpServers.map(serverView) })
		),

		http.post('*/api/mcp/servers', async ({ request }) => {
			const body = (await request.json()) as CreateMcpServerRequest
			if (!isValidAgentName(body.name ?? '')) {
				return HttpResponse.json(
					{
						message:
							'Invalid server name: use letters, digits and single spaces only'
					},
					{ status: 400 }
				)
			}
			const problem = validateServerUrl(body.url ?? '')
			if (problem)
				return HttpResponse.json({ message: problem }, { status: 400 })
			if (
				body.timeoutMs !== undefined &&
				(body.timeoutMs <= 0 || body.timeoutMs > 600_000)
			) {
				return HttpResponse.json(
					{ message: 'timeoutMs must be between 1 and 600000' },
					{ status: 400 }
				)
			}
			const slug = deriveAgentId(body.name)
			if (store.mcpServers.some((row) => row.id === slug)) {
				return HttpResponse.json(
					{ message: `An MCP server named "${body.name}" already exists` },
					{ status: 409 }
				)
			}
			const row: McpServerRow = {
				id: slug,
				name: body.name.trim(),
				url: body.url ?? '',
				headerKeys: Object.keys(body.headers ?? {}),
				...(body.timeoutMs !== undefined ? { timeoutMs: body.timeoutMs } : {}),
				enabled: true,
				catalogState: 'active',
				allowPrivateNetwork: body.allowPrivateNetwork === true,
				allowPlaintext: body.allowPlaintext === true
			}
			store.mcpServers.push(row)
			seedServerTools(slug)
			return listingResponse(row, 201)
		}),

		http.patch('*/api/mcp/servers/:id', async ({ params, request }) => {
			const id = String(params.id)
			const row = store.mcpServers.find((server) => server.id === id)
			if (!row) {
				return HttpResponse.json(
					{ message: `MCP server "${id}" not found` },
					{ status: 404 }
				)
			}
			const body = (await request.json()) as UpdateMcpServerRequest
			if (body.name !== undefined && !isValidAgentName(body.name)) {
				return HttpResponse.json(
					{
						message:
							'Invalid server name: use letters, digits and single spaces only'
					},
					{ status: 400 }
				)
			}
			if (
				body.timeoutMs !== undefined &&
				(body.timeoutMs <= 0 || body.timeoutMs > 600_000)
			) {
				return HttpResponse.json(
					{ message: 'timeoutMs must be between 1 and 600000' },
					{ status: 400 }
				)
			}
			const url = body.url !== undefined ? body.url : (row.url ?? '')
			const problem = validateServerUrl(url)
			if (problem)
				return HttpResponse.json({ message: problem }, { status: 400 })
			const urlChanged = body.url !== undefined && body.url !== row.url
			const connectionChanged =
				urlChanged || body.timeoutMs !== undefined || body.headers !== undefined

			// The global switch is a pure state flip: no connection
			// check, no config rewrite (mirrors the backend) — and it
			// lands only after every validation passed, so a rejected
			// patch leaves the row untouched.
			if (body.enabled !== undefined) {
				row.enabled = body.enabled
				if (!row.enabled) row.lastError = undefined
			}
			row.name = body.name !== undefined ? body.name.trim() : row.name
			// A destination change invalidates its approvals: the same
			// patch may re-approve them.
			if (urlChanged) {
				row.allowPrivateNetwork = false
				row.allowPlaintext = false
			}
			row.url = url
			row.timeoutMs =
				body.timeoutMs !== undefined ? body.timeoutMs : row.timeoutMs
			if (body.clearTimeoutMs) delete row.timeoutMs
			if (body.allowPrivateNetwork !== undefined)
				row.allowPrivateNetwork = body.allowPrivateNetwork
			if (body.allowPlaintext !== undefined)
				row.allowPlaintext = body.allowPlaintext
			row.headerKeys =
				body.headers !== undefined ? Object.keys(body.headers) : row.headerKeys
			if (connectionChanged) {
				reconcileServerTools(id)
				row.lastError = undefined
			}
			return listingResponse(row)
		}),

		http.delete('*/api/mcp/servers/:id', ({ params }) => {
			const id = String(params.id)
			const index = store.mcpServers.findIndex((server) => server.id === id)
			if (index === -1) {
				return HttpResponse.json(
					{ message: `MCP server "${id}" not found` },
					{ status: 404 }
				)
			}
			store.mcpServers.splice(index, 1)
			dropServerTools(id)
			return HttpResponse.json({ message: 'MCP server deleted' })
		}),

		http.post('*/api/mcp/servers/:id/refresh', ({ params }) => {
			const id = String(params.id)
			const row = store.mcpServers.find((server) => server.id === id)
			if (!row) {
				return HttpResponse.json(
					{ message: `MCP server "${id}" not found` },
					{ status: 404 }
				)
			}
			if (!row.enabled) {
				return HttpResponse.json(
					{ message: 'Server is disabled — enable it before refreshing' },
					{ status: 400 }
				)
			}
			// "flaky" simulates a server that verifies at install but is
			// down at refresh: the one path that exercises lastError and
			// the 502 branch in the UI.
			const flaky = `${row.url ?? ''}`.includes('flaky')
			if (flaky) {
				row.lastError = `mcp server ${id}: initialize: connection refused`
				return HttpResponse.json(
					{ message: `Connection check failed: ${row.lastError}` },
					{ status: 502 }
				)
			}
			// "paused" simulates a rejected generation: the catalog pauses
			// and a later healthy refresh unpauses it.
			if (
				`${row.url ?? ''}`.includes('paused') &&
				row.catalogState === 'active'
			) {
				row.catalogState = 'paused'
				row.lastError =
					'catalog_invalid_schema: tool "boom" input schema: depth > 32'
				return HttpResponse.json(
					{ message: `Connection check failed: ${row.lastError}` },
					{ status: 502 }
				)
			}
			row.catalogState = 'active'
			row.lastError = undefined
			return listingResponse(row)
		}),

		// The stored definition the settings page inspects, with the
		// connection facts beside it.
		http.get('*/api/mcp/servers/:id/tools/:toolId/definition', ({ params }) => {
			const id = String(params.id)
			const toolId = String(params.toolId)
			// The backend's read order: server, then the tool, then the
			// ownership pair — so the messages match it.
			const server = store.mcpServers.find((row) => row.id === id)
			if (!server) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `MCP server "${id}" not found` },
					{ status: 404 }
				)
			}
			const tool = store.tools.find((row) => row.id === toolId)
			if (!tool) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Tool "${toolId}" not found` },
					{ status: 404 }
				)
			}
			if (tool.serverId !== id) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Tool "${toolId}" not found on server "${id}"` },
					{ status: 404 }
				)
			}
			return HttpResponse.json({
				definitionJson: tool.definitionJson ?? '{}',
				endpoint: maskedUrl(server.url ?? ''),
				hasCredentials: (server.headerKeys ?? []).length > 0,
				allowPlaintext: server.allowPlaintext,
				allowPrivateNetwork: server.allowPrivateNetwork
			})
		}),

		// The agent's tool view: inheritance by default, an explicit
		// opt-out per tool, and every server's gate state.
		http.get('*/api/agent/:id/tools', ({ params }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			return HttpResponse.json(agentListing(id))
		}),

		// The agent's MCP gate: a pure flip — it never seeds or revokes a
		// per-tool choice.
		http.patch('*/api/agent/:id/mcp/:serverId', async ({ params, request }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const serverId = String(params.serverId)
			if (!store.mcpServers.some((server) => server.id === serverId)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `MCP server "${serverId}" not found` },
					{ status: 404 }
				)
			}
			const body = (await request.json()) as { enabled?: boolean }
			if (typeof body.enabled !== 'boolean') {
				return HttpResponse.json<ErrorResponse>(
					{ message: 'enabled is required' },
					{ status: 400 }
				)
			}
			const gates = store.agentMcpGates.get(id) ?? new Map<string, boolean>()
			gates.set(serverId, body.enabled)
			store.agentMcpGates.set(id, gates)
			return HttpResponse.json(agentListing(id))
		}),

		http.patch('*/api/agent/:id/tools/:toolId', async ({ params, request }) => {
			const id = String(params.id)
			if (!store.configurations.has(id)) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Agent "${id}" not found` },
					{ status: 404 }
				)
			}
			const toolId = String(params.toolId)
			const tool = store.tools.find((row) => row.id === toolId)
			if (!tool) {
				return HttpResponse.json<ErrorResponse>(
					{ message: `Tool "${toolId}" not found` },
					{ status: 404 }
				)
			}
			const body = (await request.json()) as ToggleAgentToolRequest
			if (typeof body.enabled !== 'boolean') {
				return HttpResponse.json<ErrorResponse>(
					{ message: 'enabled is required' },
					{ status: 400 }
				)
			}
			const disabled = store.agentDisabledTools.get(id) ?? new Set<string>()
			// Disabling records an opt-out; enabling clears it and the tool
			// is inherited again.
			if (body.enabled) disabled.delete(toolId)
			else disabled.add(toolId)
			store.agentDisabledTools.set(id, disabled)
			return HttpResponse.json(agentListing(id))
		}),

		http.post('*/api/providers', async ({ request }) => {
			const body = (await request.json()) as {
				name?: string
				baseUrl?: string
				apiKey?: string
			}
			const name = (body.name ?? '').trim()
			if (name === '') {
				return HttpResponse.json(
					{ message: 'Provider name is required' },
					{ status: 400 }
				)
			}
			const baseUrl = body.baseUrl ?? ''
			if (!/^https?:\/\/\S+/.test(baseUrl)) {
				return HttpResponse.json(
					{ message: 'Invalid baseUrl: must be an http(s) URL' },
					{ status: 400 }
				)
			}
			const failure = connectionCheck(body.apiKey ?? '')
			if (failure) return failure
			const row = {
				id: crypto.randomUUID(),
				name,
				baseUrl,
				apiKey: body.apiKey ?? '',
				models: [] as ProviderModelRow[]
			}
			store.providers.push(row)
			return HttpResponse.json({ id: row.id }, { status: 201 })
		}),

		http.patch('*/api/providers/:id', async ({ request, params }) => {
			const id = String(params.id)
			const row = store.providers.find((provider) => provider.id === id)
			if (!row) {
				return HttpResponse.json(
					{ message: `Provider "${id}" not found` },
					{ status: 404 }
				)
			}
			const body = (await request.json()) as {
				name?: string
				baseUrl?: string
				apiKey?: string
				models?: ProviderModelRow[]
			}
			if (body.name !== undefined && body.name.trim() === '') {
				return HttpResponse.json(
					{ message: 'Provider name is required' },
					{ status: 400 }
				)
			}
			if (body.baseUrl !== undefined && !/^https?:\/\/\S+/.test(body.baseUrl)) {
				return HttpResponse.json(
					{ message: 'Invalid baseUrl: must be an http(s) URL' },
					{ status: 400 }
				)
			}
			if (body.models !== undefined) {
				const kept = new Set<string>()
				for (const model of body.models) {
					if (model.id.trim() === '') {
						return HttpResponse.json(
							{ message: 'Model id cannot be empty' },
							{ status: 400 }
						)
					}
					if (kept.has(model.id)) {
						return HttpResponse.json(
							{ message: `Duplicate model id "${model.id}"` },
							{ status: 400 }
						)
					}
					kept.add(model.id)
					if (model.contextWindow && model.contextWindow > 10_000_000) {
						return HttpResponse.json(
							{ message: 'Context window must be at most 10000000 tokens' },
							{ status: 400 }
						)
					}
				}
				// Custom ids are globally unique across providers.
				for (const model of body.models) {
					const owner = store.providers.find(
						(provider) =>
							provider.id !== id &&
							provider.models.some((m) => m.id === model.id)
					)
					if (owner) {
						return HttpResponse.json(
							{
								message: `Model "${model.id}" already belongs to another provider`
							},
							{ status: 409 }
						)
					}
				}
				row.models = body.models
			}
			// A connection-touching patch re-checks the effective
			// configuration — keyed or keyless — before anything mutates.
			if (body.apiKey !== undefined || body.baseUrl !== undefined) {
				const failure = connectionCheck(
					body.apiKey === undefined ? row.apiKey : body.apiKey
				)
				if (failure) return failure
			}
			if (body.name !== undefined) row.name = body.name
			if (body.baseUrl !== undefined) row.baseUrl = body.baseUrl
			if (body.apiKey !== undefined) row.apiKey = body.apiKey
			return HttpResponse.json(providerView(row))
		}),

		http.delete('*/api/providers/:id', ({ params }) => {
			const id = String(params.id)
			const index = store.providers.findIndex((provider) => provider.id === id)
			if (index === -1) {
				return HttpResponse.json(
					{ message: `Provider "${id}" not found` },
					{ status: 404 }
				)
			}
			store.providers.splice(index, 1)
			// The DB's ON DELETE SET NULL: rows pinned to the deleted
			// provider fall back to default routing (no model).
			for (const heartbeat of store.heartbeats.values()) {
				if (heartbeat.model?.providerId === id) heartbeat.model = undefined
			}
			for (const jobs of store.cronJobs.values()) {
				for (const job of jobs) {
					if (job.model?.providerId === id) job.model = undefined
				}
			}
			return HttpResponse.json({ message: 'Provider deleted' })
		})
	]
}
