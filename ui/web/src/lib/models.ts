// src/lib/models.ts

export interface Model {
	id: string
	name: string
}

export interface ModelGroup {
	provider: string
	models: Model[]
}

const modelsConfig = {
	defaultModel: 'auto',
	providers: [
		{
			provider: 'OpenAI',
			models: [
				{ id: 'gpt-4', name: 'GPT-4' },
				{ id: 'gpt-3.5-turbo', name: 'GPT-3.5 Turbo' }
			]
		},
		{
			provider: 'Anthropic',
			models: [{ id: 'claude-3-sonnet', name: 'Claude 3 Sonnet' }]
		},
		{
			provider: 'Gemini',
			models: [
				{ id: 'gemini-pro', name: 'Gemini Pro' },
				{ id: 'gemini-ultra', name: 'Gemini Ultra' }
			]
		},
		{
			provider: 'Grok',
			models: [{ id: 'grok-1', name: 'Grok-1' }]
		}
	]
}

export const defaultModel = modelsConfig.defaultModel
export const modelGroups: ModelGroup[] = modelsConfig.providers
