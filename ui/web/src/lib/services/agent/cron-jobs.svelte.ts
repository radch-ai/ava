import { SvelteMap } from 'svelte/reactivity'
import type { CronJob } from '#lib/types/agent'
import {
	createCronJob,
	deleteCronJob,
	getCronJobs,
	updateCronJob
} from './config'

// Cron jobs are fetched lazily — only when the Cron Jobs panel opens —
// and cached per agent; every mutation endpoint applies its result to
// the cache so the panel never refetches after an edit. Updates write
// fresh arrays (never in-place mutation) so readers re-render.
const cache = new SvelteMap<string, CronJob[]>()

/** Read the cached list for an agent, if it is loaded. */
export function cronJobsFor(agentId: string): CronJob[] | undefined {
	return cache.get(agentId)
}

/** Fetch the list once per agent; later calls are cache reads. */
export async function loadCronJobs(agentId: string): Promise<CronJob[]> {
	const cached = cache.get(agentId)
	if (cached) return cached
	const cronJobs = await getCronJobs(agentId)
	cache.set(agentId, cronJobs)
	return cronJobs
}

/** Create a job and add the stored row to the cache. */
export async function createCachedCronJob(
	agentId: string,
	job: CronJob
): Promise<CronJob> {
	const stored = await createCronJob(agentId, job)
	cache.set(agentId, [...(cache.get(agentId) ?? []), stored])
	return stored
}

/** Save the full edited job, replacing the cached row. */
export async function updateCachedCronJob(
	agentId: string,
	job: CronJob
): Promise<CronJob> {
	const stored = await updateCronJob(agentId, job)
	cache.set(
		agentId,
		(cache.get(agentId) ?? []).map((existing) =>
			existing.id === job.id ? stored : existing
		)
	)
	return stored
}

/** Delete a job and drop the cached row. */
export async function deleteCachedCronJob(
	agentId: string,
	jobId: string
): Promise<void> {
	await deleteCronJob(agentId, jobId)
	cache.set(
		agentId,
		(cache.get(agentId) ?? []).filter((job) => job.id !== jobId)
	)
}

/** Drop cached lists (all agents, or one); the next load refetches. */
export function invalidateCronJobs(agentId?: string) {
	if (agentId === undefined) cache.clear()
	else cache.delete(agentId)
}
