import { SUPERAGENT_AGENT_ID, SUPERAGENT_NAME } from '../config.ts'
import type { AgentActivityItem } from '../services/agent/activity.ts'
import type { ThreadMessage } from '../services/agent/thread.ts'
import type {
	Agent,
	AgentConfiguration,
	SuperagentConfiguration
} from '../types/agent.ts'

/** Mutable in-memory state the mock handlers read from and write to. */
export interface MockStore {
	agents: Agent[]
	configurations: Map<string, AgentConfiguration>
	thread: ThreadMessage[]
	activity: Map<string, AgentActivityItem[]>
	/** The one token POST /auth/exchange accepts. */
	token: string
}

// Fixed anchor for every seeded timestamp: 2026-10-23T12:00:00Z.
const ANCHOR = Date.parse('2026-10-23T12:00:00Z')

const minutesAgo = (minutes: number): string =>
	new Date(ANCHOR - minutes * 60_000).toISOString()

/** Seeded user-created agents; the system superagent is never in the list. */
const AGENT_SEEDS: Agent[] = [
	{ id: 'research', name: 'Research', icon: 'globe' },
	{ id: 'scribe', name: 'Scribe', icon: 'pencil' },
	{ id: 'sentinel', name: 'Sentinel', icon: 'shield' }
]

/** Configuration seeds keyed by agent id (`_superagent` is system-reserved). */
const SUPERAGENT_CONFIG = {
	name: SUPERAGENT_NAME,
	icon: 'message-square',
	soul: {
		personality:
			'Warm, direct, and precise. Prefers short answers with concrete next steps.',
		identity: `${SUPERAGENT_NAME} is the user's primary assistant and coordinates the other agents.`
	},
	userContext:
		'The user builds personal automation tooling and prefers concise, technical answers.',
	operatingInstructions:
		'Keep replies under five lines unless asked to elaborate. Surface agent failures immediately with a suggested fix.',
	toolsContext:
		"Local tools: file system read/write, browser, and shell access are available on the user's devices.",
	toolsEnabled: ['browser.browse', 'fs.read', 'fs.write', 'shell.run'],
	cronJobs: [
		{
			id: 'cron-digest',
			time: '09:00',
			frequency: 'daily',
			model: 'gpt-4',
			prompt:
				'Compile the overnight digest: agent failures, research highlights, and today’s action items.'
		}
	],
	heartbeat: {
		id: 'heartbeat-ava',
		model: 'auto',
		enabled: true,
		duration: '30m',
		content: 'Check agent heartbeats and flag anything stalled.'
	}
} satisfies SuperagentConfiguration

const CONFIGURATIONS: Array<[id: string, config: AgentConfiguration]> = [
	[SUPERAGENT_AGENT_ID, SUPERAGENT_CONFIG],
	[
		'research',
		{
			name: 'Research',
			icon: 'globe',
			soul: {
				personality:
					'Methodical and skeptical. Cross-checks claims before presenting them.',
				identity: 'Research scans sources and produces verified summaries.'
			},
			operatingInstructions:
				'Verify every claim against two independent sources. Cite what you use.',
			toolsContext: 'Uses the browser and a local cache of saved papers.',
			toolsEnabled: ['browser.browse', 'fs.read'],
			heartbeat: {
				id: 'heartbeat-research',
				model: 'gpt-4',
				enabled: true,
				duration: '1hr',
				content:
					'Scan new papers on personal agents and append anything relevant to the notes.'
			}
		}
	],
	[
		'scribe',
		{
			name: 'Scribe',
			icon: 'pencil',
			soul: {
				personality:
					'Careful listener. Captures exactly what was said, then distills it.',
				identity: 'Scribe transcribes conversations and extracts action items.'
			},
			operatingInstructions:
				'Never paraphrase quotes. Mark decisions and owners explicitly.',
			toolsContext: 'Reads meeting recordings and writes structured notes.',
			toolsEnabled: ['fs.read', 'fs.write'],
			heartbeat: {
				id: 'heartbeat-scribe',
				model: 'claude-3-sonnet',
				enabled: false,
				duration: '2hr',
				content: 'Clean up unfinished transcripts.'
			}
		}
	],
	[
		'sentinel',
		{
			name: 'Sentinel',
			icon: 'shield',
			soul: {
				personality: 'Calm and watchful. Errs on the side of alerting early.',
				identity: 'Sentinel watches auth logs and system health.'
			},
			operatingInstructions:
				'Alert on any failed login or stale heartbeat within one minute.',
			toolsContext: 'Reads auth logs and service health endpoints.',
			toolsEnabled: ['fs.read'],
			heartbeat: {
				id: 'heartbeat-sentinel',
				model: 'gpt-4',
				enabled: true,
				duration: '30m',
				content: 'Scan auth logs for failures and report anomalies.'
			}
		}
	]
]

/** Chronological conversation (oldest first). */
const CONVERSATION: Array<['user' | 'assistant', string]> = [
	['user', `Morning ${SUPERAGENT_NAME} — what’s on my plate today?`],
	[
		'assistant',
		'Two heartbeats flagged overnight: Sentinel saw a failed login attempt, and Research summarized three papers on personal agents. Full digest is in the activity tab.'
	],
	['user', 'Show me the research summary.'],
	[
		'assistant',
		'The papers converge on local-first orchestration: agents that keep state on-device and call out to models only when needed. I saved the key claims to your notes.'
	],
	['user', 'Ask Sentinel to watch for more login failures this week.'],
	[
		'assistant',
		'Done — Sentinel’s heartbeat now checks auth logs hourly and will ping you on any failure.'
	],
	['user', 'Can you draft a reply to Dana about the sync issue?'],
	[
		'assistant',
		'Drafted: “The sync stall was caused by the stale cursor — cleared on our side, your data should catch up within an hour.” Want me to send it?'
	],
	['user', 'Send it.'],
	['assistant', 'Sent. I’ll watch the thread and flag any reply.'],
	['user', 'What did Scribe finish yesterday?'],
	[
		'assistant',
		'Scribe transcribed the design call and extracted four action items. Two are yours; both are on today’s list.'
	],
	['user', 'Move the second one to Friday.'],
	['assistant', 'Moved. I’ll remind you Friday morning.'],
	['user', 'Any idea why the build failed last night?'],
	[
		'assistant',
		'A dependency pin drifted — the lockfile wanted 1.4.2 but CI cached 1.4.0. Re-running with a clean cache passed.'
	],
	['user', 'Add a cron to clean the cache weekly.'],
	[
		'assistant',
		'Added: Sundays at 09:00, model gpt-4, prompt “Clean CI caches and report reclaimed space.”'
	],
	['user', 'Summarize Dana’s reply when it lands.'],
	[
		'assistant',
		'Watching the thread. I’ll summarize anything longer than a paragraph.'
	],
	['user', 'How much did the agents spend this week?'],
	[
		'assistant',
		'Model spend is $4.12 across 1,204 calls — 71% Research, the rest split between Scribe and Sentinel.'
	],
	['user', 'Good. Keep the digest short like this.'],
	['assistant', 'Understood — digests stay under five lines from now on.']
]

const ACTIVITY_ACTIONS: Record<string, string[]> = {
	[SUPERAGENT_AGENT_ID]: [
		'thread.message',
		'digest.send',
		'cron.run',
		'watch.notify'
	],
	research: ['paper.scan', 'summary.save', 'heartbeat.run'],
	scribe: ['transcribe.run', 'action.extract', 'heartbeat.run'],
	sentinel: ['auth.scan', 'alert.send', 'heartbeat.run']
}

const ACTIVITY_TRIGGERS = ['user', 'ava', 'heartbeat', 'cron']

function seedActivity(): Map<string, AgentActivityItem[]> {
	const activity = new Map<string, AgentActivityItem[]>()
	for (const [id, actions] of Object.entries(ACTIVITY_ACTIONS)) {
		activity.set(
			id,
			Array.from({ length: 12 }, (_, index): AgentActivityItem => {
				const status: AgentActivityItem['status'] =
					index % 5 === 1 ? 'running' : index % 7 === 3 ? 'failed' : 'completed'
				return {
					id: `${id}-${index + 1}`,
					createdAt: minutesAgo(index * 120),
					triggeredBy: ACTIVITY_TRIGGERS[index % ACTIVITY_TRIGGERS.length],
					action: actions[index % actions.length],
					status,
					message:
						status === 'failed'
							? `Failed after retry; escalated to ${SUPERAGENT_NAME}`
							: status === 'running'
								? 'Waiting for model response'
								: 'Completed without errors'
				}
			})
		)
	}
	return activity
}

/**
 * Create a fresh seed store. Each consumer (dev server, test) gets its own
 * instance so mutations never leak across sessions or test runs.
 */
export function createSeedData(): MockStore {
	// Newest first: the conversation above is authored oldest-first.
	const thread = CONVERSATION.map(
		([role, content], index): ThreadMessage => ({
			id: `msg-${index + 1}`,
			role,
			content,
			createdAt: minutesAgo((CONVERSATION.length - 1 - index) * 5)
		})
	).reverse()

	return {
		agents: [...AGENT_SEEDS],
		configurations: new Map(CONFIGURATIONS),
		thread,
		activity: seedActivity(),
		token: 'ava-mock-token'
	}
}
