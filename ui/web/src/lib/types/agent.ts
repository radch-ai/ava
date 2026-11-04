/**
 * Agent domain types, defaults, and the field configs driving the UI.
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
// AGENT IDENTITY (id + display fields)
// ============================================================================

/**
 * Agent identity — id plus the display fields; list entries and detail
 * bodies both carry it
 */
export interface AgentIdentity {
	/** Agent id used in URLs and API paths; `_`-prefixed ids are system-reserved. */
	id: string
	name: string
	icon: AgentIconName
}

/**
 * superagent agent definition - the primary assistant
 */
export const SUPERAGENT_AGENT: AgentIdentity = {
	id: SUPERAGENT_AGENT_ID,
	name: SUPERAGENT_NAME,
	icon: 'message-square'
}

/** Identity (id, name, icon) plus every AgentConfig field at one
 * level. */
export interface AgentConfigWithIdentity extends AgentIdentity, AgentConfig {}

// ============================================================================
// SEPARATE TABLE ENTITIES
// ============================================================================

/**
 * Heartbeat configuration (stored in separate heartbeat table)
 */
export interface HeartbeatConfig {
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
 * Soul: the agent's personality and identity texts.
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
 * Agent configuration. userContext is superagent-only; heartbeat
 * starts disabled with empty fields (the superagent is seeded
 * enabled); cronJobs is empty on regular agents.
 */
export interface AgentConfig {
	/** Agent personality and identity texts */
	soul: SoulConfig
	/** Information about the user and their preferences — superagent only */
	userContext?: string
	/** Operating rules, priorities, and behavioral guidelines */
	operatingInstructions: string
	/** Notes about local tools, conventions, and integrations */
	toolsContext: string
	/** Array of tool IDs that this agent can use */
	toolsEnabled: string[]
	/** Heartbeat config; disabled with empty fields until saved */
	heartbeat: HeartbeatConfig
	/** The agent's cron jobs — empty on regular agents */
	cronJobs: CronJob[]
}
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
		'icon',
		'soul',
		'userContext',
		'operatingInstructions',
		'toolsContext',
		'toolsEnabled',
		'heartbeat',
		'cronJobs'
	] as const,
	agent: [
		'name',
		'icon',
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
	| (typeof AGENT_FIELDS.agent)[number]

// ============================================================================
// DEFAULT FACTORIES
// ============================================================================

/**
 * Get a random icon name
 */
function getRandomIcon(): AgentIconName {
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
 * Create default agent configuration — the editor's starting shape.
 * heartbeat starts disabled with empty fields; cron jobs start
 * empty.
 */
export function createDefaultAgentConfig(): AgentConfig &
	Pick<AgentIdentity, 'name' | 'icon'> {
	return {
		name: '',
		icon: getRandomIcon(),
		soul: createDefaultSoulConfig(),
		operatingInstructions: '',
		toolsContext: '',
		toolsEnabled: [],
		heartbeat: { model: '', enabled: false, duration: '1hr', content: '' },
		cronJobs: []
	}
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

/**
 * Deep clone configuration object
 */
export function cloneConfiguration<T extends AgentConfig>(configuration: T): T {
	return JSON.parse(JSON.stringify(configuration))
}

/**
 * Get a configuration field value safely
 */
export function getConfigurationField(
	configuration: AgentConfig & Pick<AgentIdentity, 'name' | 'icon'>,
	field: ConfigFieldName
): unknown {
	return (configuration as unknown as Record<string, unknown>)[field]
}

/**
 * Set a configuration field value safely
 */
export function setConfigurationField(
	configuration: AgentConfig & Pick<AgentIdentity, 'name' | 'icon'>,
	field: ConfigFieldName,
	value: unknown
): void {
	;(configuration as unknown as Record<string, unknown>)[field] = value
}
