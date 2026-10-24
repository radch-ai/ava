import type { PageLoad } from './$types';
import { getSuperagentThread, type SuperagentThread } from '$lib/services/user';

interface PageLoadResult {
	thread: SuperagentThread | null;
	threadError: string | null;
}

export const load: PageLoad<PageLoadResult> = async () => {
	try {
		const thread = await getSuperagentThread();
		return { thread, threadError: null };
	} catch (error) {
		const threadError = error instanceof Error ? error.message : 'Failed to load conversation';
		return { thread: null, threadError };
	}
};
