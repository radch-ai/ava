/**
 * Provider catalog service — the /api/providers endpoints plus the
 * selector-facing /api/models. The backend merges embedded system
 * providers into every response; custom models are user-saved
 * state.
 */

import type {
	CreateProviderRequest,
	CreateProviderResponse,
	GetModelsResponse,
	GetProvidersResponse,
	Provider,
	ProviderModelsResponse,
	UpdateProviderRequest
} from '#lib/types/provider'
import { api } from './api'

/** The whole catalog in one response: providers with their custom
 * models. */
export async function getProviders(): Promise<GetProvidersResponse> {
	return api.get('providers').json<GetProvidersResponse>()
}

/** The selector catalog: grouped models only, no connection config. */
export async function getModels(): Promise<GetModelsResponse> {
	return api.get('models').json<GetModelsResponse>()
}

/** One provider's system (fetched/fallback) model ids. */
export async function getModelsByProvider(
	id: string
): Promise<ProviderModelsResponse> {
	return api.get(`providers/${id}/models`).json<ProviderModelsResponse>()
}

/** Add a connection; the server mints the uuid id. */
export async function createProvider(
	input: CreateProviderRequest
): Promise<CreateProviderResponse> {
	return api.post('providers', { json: input }).json<CreateProviderResponse>()
}

/** Edit a connection and/or replace its custom model list wholesale. */
export async function updateProvider(
	id: string,
	patch: UpdateProviderRequest
): Promise<Provider> {
	return api.patch(`providers/${id}`, { json: patch }).json<Provider>()
}

/** Remove a stored connection; system providers revert to keyless. */
export async function deleteProvider(id: string): Promise<void> {
	await api.delete(`providers/${id}`)
}
