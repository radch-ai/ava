import { afterEach, beforeAll, afterAll, describe, expect, it } from 'vitest';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { AuthStore } from '$lib/auth/store.svelte';

const MOCK_USER = { sub: 'u1', name: 'Ava' };

const server = setupServer(
	// Node fetch has no cookie jar; "has session" here just means the server
	// says so. Cookie mechanics are the browser's job (browser-verified).
	http.get('*/api/auth/me', () => HttpResponse.json({ user: MOCK_USER })),
	http.post('*/api/auth/exchange', async ({ request }) => {
		const { token } = (await request.json()) as { token: string };
		if (token !== 'secret-token') {
			return HttpResponse.json({ message: 'Invalid token' }, { status: 401 });
		}
		return HttpResponse.json(
			{ user: MOCK_USER },
			{ headers: { 'Set-Cookie': 'ava_session=s; Path=/; HttpOnly; SameSite=Strict' } }
		);
	}),
	http.post(
		'*/api/auth/logout',
		() =>
			new HttpResponse(null, {
				status: 204,
				headers: { 'Set-Cookie': 'ava_session=; Max-Age=0' }
			})
	)
);

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('AuthStore (token session provider)', () => {
	it('restores an authenticated session from GET /auth/me', async () => {
		const store = new AuthStore();
		await store.initialize();
		expect(store.isAuthenticated).toBe(true);
	});

	it('starts unauthenticated when /auth/me rejects with 401', async () => {
		const store = new AuthStore();
		server.use(
			http.get('*/api/auth/me', () => HttpResponse.json({ message: 'No session' }, { status: 401 }))
		);
		await store.initialize();
		expect(store.isAuthenticated).toBe(false);
	});

	it('authenticates via submitToken and POST /auth/exchange', async () => {
		const store = new AuthStore();
		await store.submitToken('secret-token');
		expect(store.isAuthenticated).toBe(true);
	});

	it('rejects an invalid token with the server message and stays unauthenticated', async () => {
		const store = new AuthStore();
		await expect(store.submitToken('wrong')).rejects.toThrow('Invalid token');
		expect(store.isAuthenticated).toBe(false);
	});

	it('logs out via POST /auth/logout and clears state', async () => {
		const store = new AuthStore();
		await store.submitToken('secret-token');
		await store.logout();
		expect(store.isAuthenticated).toBe(false);
	});

	it('clears state without network on handleSessionExpired', async () => {
		const store = new AuthStore();
		await store.submitToken('secret-token');
		store.handleSessionExpired();
		expect(store.isAuthenticated).toBe(false);
	});
});
