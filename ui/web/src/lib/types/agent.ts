/**
 * Agent Types
 *
 * Aligned with database schema:
 * - agents table: config (JSONB), tools_enabled (UUID[])
 * - heartbeat table: id, model, fk(user_id, agent_id), enabled, duration, content
 * - cron table: id, fk(user_id, agent_id), time, frequency, prompt
 * - user_tools table: referenced by tools_enabled UUID array
 *
 * At runtime: SELECT * FROM user_tools WHERE id = ANY(agent.tools_enabled)
 */

// Icon data/types are pure TS with no component imports, keeping this
// file loadable by the mock server's node-side module graph
// (vite.config / msw) — never import the AgentIcon component here.
import {
	type AgentIconName,
	ALL_AGENT_ICON_NAMES
} from '#components/agent-icons/index.ts'
import { SUPERAGENT_AGENT_ID, SUPERAGENT_NAME } from '#lib/config'

// ============================================================================
// RUNTIME AGENT TYPE (for UI routing)
// ============================================================================

/**
 * Agent displayed in UI - uses icon name string for flexibility
 */
export interface Agent {
	/** Agent id used in URLs and API paths; `_`-prefixed ids are system-reserved. */
	id: string
	name: string
	label: string
	icon: AgentIconName
}

/**
 * superagent agent definition - the primary assistant
 */
export const SUPERAGENT_AGENT: Agent = {
	id: SUPERAGENT_AGENT_ID,
	name: SUPERAGENT_NAME,
	label: `Chat with ${SUPERAGENT_NAME}`,
	icon: 'message-square'
}

// ============================================================================
// SEPARATE TABLE ENTITIES
// ============================================================================

/**
 * Heartbeat configuration (stored in separate heartbeat table)
 */
export interface HeartbeatConfig {
	id?: string
	model: string
	enabled: boolean
	duration: '30m' | '1hr' | '2hr'
	content: string
}

/**
 * Cron job item (stored in separate cron table)
 */
export interface CronJob {
	id: string
	time: string
	frequency: 'daily' | 'weekly' | 'custom'
	/** Custom cron expression (used when frequency is 'custom') */
	cronExpression?: string
	model: string
	prompt: string
}

// ============================================================================
// SOUL STRUCTURE (JSONB in DB)
// ============================================================================

/**
 * Soul configuration - stored as JSONB in database
 * Contains identity/personality information
 */
export interface SoulConfig {
	/** Personality traits and behavioral characteristics */
	personality: string
	/** Identity definition and core characteristics */
	identity: string
}

// ============================================================================
// MAIN AGENT CONFIGURATION
// ============================================================================

/**
 * Core agent configuration fields (maps to agents table)
 */
export interface AgentConfigBase {
	/** Display name for the agent */
	name: string
	/** Icon for the agent (from the agent-icons set) */
	icon?: string
	/** Agent personality/identity (stored as JSONB) */
	soul: SoulConfig
	/** Operating rules, priorities, and behavioral guidelines */
	operatingInstructions: string
	/** Notes about local tools, conventions, and integrations */
	toolsContext: string
	/** Array of tool IDs that this agent can use */
	toolsEnabled: string[]
}

/**
 * superagent agent configuration (includes heartbeat and cron job references)
 */
export interface SuperagentConfiguration extends AgentConfigBase {
	heartbeat?: HeartbeatConfig
	cronJobs: CronJob[]
	/** Information about the user and their preferences */
	userContext: string
}

/**
 * Worker agent configuration
 */
export interface WorkerConfiguration extends AgentConfigBase {
	heartbeat?: HeartbeatConfig
}

/**
 * Union type for all agent configurations
 */
export type AgentConfiguration = SuperagentConfiguration | WorkerConfiguration

// ============================================================================
// FIELD CONFIGS FOR UI
// ============================================================================

/**
 * Field configuration for UI rendering
 */
export interface FieldConfig {
	label: string
	description: string
	placeholder: string
}

/**
 * Field configs for soul sub-fields
 */
export const SOUL_FIELD_CONFIGS: Record<keyof SoulConfig, FieldConfig> = {
	personality: {
		label: 'Personality',
		description: 'Personality traits and behavioral characteristics',
		placeholder: 'Describe the personality traits and behavioral patterns...'
	},
	identity: {
		label: 'Identity',
		description: 'Core identity and defining characteristics',
		placeholder: 'Define the core identity and what makes this agent unique...'
	}
}

/**
 * Field configs for agent configuration form
 */
export const FIELD_CONFIGS: Record<string, FieldConfig> = {
	name: {
		label: 'Name',
		description: 'Display name for this agent',
		placeholder: 'Enter agent name...'
	},
	icon: {
		label: 'Icon',
		description: 'Icon for this agent',
		placeholder: ''
	},
	soul: {
		label: 'Soul',
		description: "Define the agent's personality and identity",
		placeholder: ''
	},
	userContext: {
		label: 'User Context',
		description: 'Information about who you are and your preferences',
		placeholder:
			'Describe who you are, your role, and how you prefer to communicate...'
	},
	operatingInstructions: {
		label: 'Operating Instructions',
		description: 'Operating rules, priorities, and behavioral guidelines',
		placeholder:
			"Define operating rules, priorities, and do/don't guidelines..."
	},
	toolsContext: {
		label: 'Tools Context',
		description: 'Notes about local tools, conventions, and integrations',
		placeholder: 'E.g., "Calendar: Google Calendar. CRM: HubSpot."'
	},
	toolsEnabled: {
		label: 'Enabled Tools',
		description: 'Toggle which tools this agent can use',
		placeholder: ''
	},
	heartbeat: {
		label: 'Heartbeat',
		description: 'Periodic check-in configuration',
		placeholder: ''
	},
	cronJobs: {
		label: 'Cron Jobs',
		description: 'Add scheduled prompts that run at specified times',
		placeholder: ''
	}
}

/**
 * Field display order for each agent type
 */
export const AGENT_FIELDS = {
	superagent: [
		'name',
		'soul',
		'userContext',
		'operatingInstructions',
		'toolsContext',
		'toolsEnabled',
		'heartbeat',
		'cronJobs'
	] as const,
	worker: [
		'name',
		'soul',
		'operatingInstructions',
		'toolsContext',
		'toolsEnabled',
		'heartbeat'
	] as const
}

/**
 * All possible configuration field names (union of all agent types)
 */
export type ConfigFieldName =
	| (typeof AGENT_FIELDS.superagent)[number]
	| (typeof AGENT_FIELDS.worker)[number]

// ============================================================================
// DEFAULT FACTORIES
// ============================================================================

/**
 * Get a random icon name
 */
function getRandomIcon(): string {
	return ALL_AGENT_ICON_NAMES[
		Math.floor(Math.random() * ALL_AGENT_ICON_NAMES.length)
	]
}

/**
 * Create default soul configuration
 */
export function createDefaultSoulConfig(): SoulConfig {
	return {
		personality: '',
		identity: ''
	}
}

/**
 * Create default superagent configuration
 */
export function createDefaultSuperagentConfiguration(): SuperagentConfiguration {
	return {
		name: '',
		icon: getRandomIcon(),
		soul: createDefaultSoulConfig(),
		userContext: '',
		operatingInstructions: '',
		toolsContext: '',
		toolsEnabled: [],
		cronJobs: []
	}
}

/**
 * Create default Worker configuration
 */
export function createDefaultWorkerConfiguration(): WorkerConfiguration {
	return {
		name: '',
		icon: getRandomIcon(),
		soul: createDefaultSoulConfig(),
		operatingInstructions: '',
		toolsContext: '',
		toolsEnabled: [],
		heartbeat: createDefaultHeartbeatConfig()
	}
}

/**
 * Create default heartbeat configuration
 */
export function createDefaultHeartbeatConfig(): HeartbeatConfig {
	return {
		model: '',
		enabled: false,
		duration: '1hr',
		content: ''
	}
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

/**
 * Deep clone configuration object
 */
export function cloneConfiguration<T extends AgentConfiguration>(
	configuration: T
): T {
	return JSON.parse(JSON.stringify(configuration))
}

/**
 * Get a configuration field value safely
 */
export function getConfigurationField(
	configuration: AgentConfiguration,
	field: ConfigFieldName
): unknown {
	return (configuration as unknown as Record<string, unknown>)[field]
}

/**
 * Set a configuration field value safely
 */
export function setConfigurationField(
	configuration: AgentConfiguration,
	field: ConfigFieldName,
	value: unknown
): void {
	;(configuration as unknown as Record<string, unknown>)[field] = value
}
