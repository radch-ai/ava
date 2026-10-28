/**
 * Server-side agent identity rules. The production backend implements the
 * same rules; this module is the mock server's copy. The name pattern is
 * declared once in `#routes/agent/new/agent-name` and shared with the
 * create form; the client never derives ids.
 */

export { isValidAgentName } from '#routes/agent/new/agent-name'

/**
 * Derive the agent id from a display name: lowercase, spaces to
 * single hyphens, anything else stripped. Derived ids can never start with
 * `_` — that prefix is reserved for system entries like `_superagent`.
 */
export function deriveAgentId(name: string): string {
	return name
		.trim()
		.toLowerCase()
		.replace(/ +/g, '-')
		.replace(/[^a-z0-9-]/g, '')
		.replace(/-{2,}/g, '-')
		.replace(/^-+|-+$/g, '')
}
