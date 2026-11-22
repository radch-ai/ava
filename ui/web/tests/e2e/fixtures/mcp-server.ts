/**
 * tests/e2e/fixtures/mcp-server.ts
 *
 * A minimal streamable-HTTP MCP server for real-backend e2e runs, started
 * by the specs themselves on an ephemeral port so the suite needs nothing
 * outside the repo. It serves the tools the specs name (echo, notes, plus
 * one name too long for the provider wire) and the /__fixture pause
 * controls the paused-catalog flow drives:
 *
 *   GET /__fixture/pause — listings carry a schema too deep for the
 *                          catalog (a rejected generation).
 *   GET /__fixture/ok    — listings return to the healthy shape.
 *
 * Node built-ins only; one JSON object per POST, JSON-RPC over HTTP.
 */
import { createServer, type Server } from 'node:http'

export interface McpFixture {
	/** Base URL (no path), e.g. http://127.0.0.1:37141 */
	url: string
	close(): Promise<void>
}

const LONG_NAME =
	'a_tool_with_a_name_longer_than_any_provider_wire_can_carry_xxxxxxxxxxxxxxxxxx'

/** A schema nested deeper than the catalog's 32-level bound. */
function rejectedSchema(): Record<string, unknown> {
	let schema: Record<string, unknown> = { type: 'string' }
	for (let i = 0; i < 40; i++) {
		schema = { type: 'array', items: schema }
	}
	return schema
}

export function startMcpFixture(host = '127.0.0.1'): Promise<McpFixture> {
	let paused = false

	const tools = () => [
		{
			name: 'echo',
			description: paused ? 'Echoes (rejected).' : 'Echoes its input text.',
			inputSchema: paused
				? rejectedSchema()
				: {
						type: 'object',
						properties: { text: { type: 'string' } },
						required: ['text']
					}
		},
		{
			name: 'notes',
			description: 'Append a note to the shared notepad.',
			inputSchema: {
				type: 'object',
				properties: { note: { type: 'string' } },
				required: ['note']
			}
		},
		{
			name: LONG_NAME,
			description: 'Unrepresentable name, skipped at discovery.',
			inputSchema: { type: 'object' }
		}
	]

	const server: Server = createServer((req, res) => {
		const url = new URL(req.url ?? '/', `http://${host}`)
		if (req.method === 'GET' && url.pathname === '/__fixture/pause') {
			paused = true
			res.writeHead(200).end('paused')
			return
		}
		if (req.method === 'GET' && url.pathname === '/__fixture/ok') {
			paused = false
			res.writeHead(200).end('ok')
			return
		}
		if (req.method !== 'POST') {
			res.writeHead(405).end()
			return
		}
		const chunks: Buffer[] = []
		req.on('data', (chunk: Buffer) => chunks.push(chunk))
		req.on('end', () => {
			let rpc: { id?: unknown; method?: string; params?: unknown }
			try {
				rpc = JSON.parse(Buffer.concat(chunks).toString('utf8'))
			} catch {
				res.writeHead(400).end()
				return
			}
			// Notifications carry no id: acknowledge and answer nothing.
			if (rpc.id === undefined || rpc.id === null) {
				res.writeHead(202).end()
				return
			}
			let result: unknown
			switch (rpc.method) {
				case 'initialize':
					result = {
						protocolVersion: '2025-03-26',
						capabilities: {},
						serverInfo: { name: 'e2e-fixture', version: '1.0.0' }
					}
					break
				case 'tools/list':
					result = { tools: tools() }
					break
				case 'tools/call': {
					const params = (rpc.params ?? {}) as {
						name?: string
						arguments?: { text?: string; note?: string }
					}
					const text = params.arguments?.text ?? params.arguments?.note ?? ''
					result = { content: [{ type: 'text', text }] }
					break
				}
				default:
					result = {}
			}
			res.writeHead(200, { 'Content-Type': 'application/json' })
			res.end(JSON.stringify({ jsonrpc: '2.0', id: rpc.id, result }))
		})
	})

	return new Promise<McpFixture>((resolve, reject) => {
		server.once('error', reject)
		server.listen(0, host, () => {
			const address = server.address()
			if (typeof address !== 'object' || address === null) {
				reject(new Error('fixture has no port'))
				return
			}
			resolve({
				url: `http://${host}:${address.port}`,
				close: () =>
					new Promise<void>((done) => {
						server.closeAllConnections?.()
						server.close(() => done())
					})
			})
		})
	})
}
