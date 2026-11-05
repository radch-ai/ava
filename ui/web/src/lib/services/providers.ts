/**
 * Provider catalog service — the /api/providers endpoints. The
 * backend seeds the catalog with the list this UI once hardcoded
 * (openai, anthropic, gemini, grok); everything here is user-saved
 * state.
 */

import type {
	CreateProviderRequest,
	CreateProviderResponse,
	GetProvidersResponse,
	Provider,
	UpdateProviderRequest
} from '#lib/types/provider'
import { api } from './api'

/** The whole catalog in one response: defaultModel + providers. */
export async function getProviders(): Promise<GetProvidersResponse> {
	return api.get('providers').json<GetProvidersResponse>()
}

/** Add a connection; the server mints the uuid id. */
export async function createProvider(
	input: CreateProviderRequest
): Promise<CreateProviderResponse> {
	return api.post('providers', { json: input }).json<CreateProviderResponse>()
}

/** Edit a connection and/or replace its model list wholesale. */
export async function updateProvider(
	id: string,
	patch: UpdateProviderRequest
): Promise<Provider> {
	return api.patch(`providers/${id}`, { json: patch }).json<Provider>()
}

/** Remove a connection and its models. */
export async function deleteProvider(id: string): Promise<void> {
	await api.delete(`providers/${id}`)
}
