import { getTools } from '#lib/services/tools'
import type { Tool } from '#lib/types/tool'
import type { PageLoad } from './$types'

export const load: PageLoad = async () => {
	let tools: Tool[] = []
	let toolsError: string | null = null
	try {
		tools = await getTools()
	} catch (e) {
		toolsError = e instanceof Error ? e.message : 'Failed to load tools'
	}
	return { tools, toolsError }
}

export const prerender = false
