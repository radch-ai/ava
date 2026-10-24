/**
 * Tool Types
 *
 * Tools are shared across agents - each user has their own set of tools.
 * The enabled state is per-agent via toolsEnabled: string[] in agent config.
 */

/**
 * User tool definition
 */
export interface Tool {
	id: string
	name: string
	description?: string
}
