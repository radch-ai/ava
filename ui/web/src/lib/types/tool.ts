/**
 * Tool Types
 *
 * Tools are shared across agents - the user configures the full set at the server level.
 * The enabled state is per-agent via toolsEnabled: string[] in agent config.
 */

/**
 * Tool definition
 */
export interface Tool {
	id: string
	name: string
	description?: string
}
