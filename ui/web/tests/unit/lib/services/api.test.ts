import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { api } from '$lib/services/api';
import { authStore } from '$lib/auth';

vi.mock('$lib/auth', () => ({
	authStore: {
		handleSessionExpired: vi.fn()
	}
}));

const { handleSessionExpired } = vi.mocked(authStore);

vi.stubEnv('VITE_BACKEND_API_URL', 'http://localhost:8080');

const server = setupServer(
	http.get('*/api/forbidden', () =>
		HttpResponse.json({ message: 'Session expired' }, { status: 401 })
	)
);

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterEach(() => {
	server.resetHandlers();
	vi.clearAllMocks();
});
afterAll(() => server.close());

describe('api', () => {
	describe('api instance', () => {
		it('is a ky instance with expected methods', () => {
			expect(api).toBeDefined();
			expect(typeof api.get).toBe('function');
			expect(typeof api.post).toBe('function');
			expect(typeof api.patch).toBe('function');
			expect(typeof api.put).toBe('function');
			expect(typeof api.delete).toBe('function');
		});
	});

	describe('auth wiring', () => {
		it('signals the auth store and throws the server message on 401', async () => {
			await expect(api.get('forbidden').json()).rejects.toThrow('Session expired');
			expect(handleSessionExpired).toHaveBeenCalledTimes(1);
		});
	});
});
