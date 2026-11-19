import { getMcpServers } from '#lib/services/mcp'
import { getTools } from '#lib/services/tools'
import type { McpServer } from '#lib/types/mcp'
import type { Tool } from '#lib/types/tool'
import { errorMessage } from '#lib/utils/errors'
import type { PageLoad } from './$types'

interface Fetched<T> {
	rows: T
	error: string | null
}

// Independent fetches run together; each failure stays a rendered error
// pair instead of failing the whole load.
export const load: PageLoad = async () => {
	const [tools, servers] = await Promise.all([
		getTools()
			.then((rows): Fetched<Tool[]> => ({ rows, error: null }))
			.catch(
				(e): Fetched<Tool[]> => ({
					rows: [],
					error: errorMessage(e, 'Failed to load tools')
				})
			),
		getMcpServers()
			.then((rows): Fetched<McpServer[]> => ({ rows, error: null }))
			.catch(
				(e): Fetched<McpServer[]> => ({
					rows: [],
					error: errorMessage(e, 'Failed to load MCP servers')
				})
			)
	])
	return {
		tools: tools.rows,
		toolsError: tools.error,
		servers: servers.rows,
		serversError: servers.error
	}
}

export const prerender = false
