import { getProviders } from '#lib/services/providers'
import type { Provider } from '#lib/types/provider'
import type { PageLoad } from './$types'

export const load: PageLoad = async () => {
	let providers: Provider[] = []
	let providersError: string | null = null
	try {
		;({ providers } = await getProviders())
	} catch (e) {
		providersError = e instanceof Error ? e.message : 'Failed to load providers'
	}
	return { providers, providersError }
}

export const prerender = false
