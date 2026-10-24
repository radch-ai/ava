import { goto } from '$app/navigation';
import { api } from '../services/api';

/**
 * Token-session auth store.
 *
 * Public surface is the provider seam: an alternative auth provider
 * implements the same methods and swaps the export in `./index.ts` — no
 * component or call-site changes. Scheme specifics live only in here:
 *
 * - credential = HttpOnly `ava_session` cookie set by `POST /auth/exchange`
 */
export class AuthStore {
	isAuthenticated = $state(false);

	/** Restore state from an existing session cookie, or start clean. */
	async initialize(): Promise<void> {
		try {
			// Session probe, not data: the HttpOnly cookie is invisible to
			// JS, so asking the server is the only way to check it. ky
			// throws on non-2xx, so the line below only runs after a 2xx —
			// any failure skips it and lands in the catch.
			await api.get('auth/me');
			this.isAuthenticated = true;
		} catch {
			this.#clear();
		}
	}

	/** Send the user to the login surface. */
	async login(): Promise<void> {
		await goto('/login');
	}

	/** Exchange a long-lived token for the session cookie. Token-provider only. */
	async submitToken(token: string): Promise<void> {
		await api.post('auth/exchange', { json: { token } });
		this.isAuthenticated = true;
	}

	/** Revoke the session; the layout guard handles navigation once state clears. */
	async logout(): Promise<void> {
		try {
			await api.post('auth/logout');
		} catch {
			// Session already gone server-side — clearing locally is still correct.
		}
		this.#clear();
	}

	/**
	 * Session invalid mid-app (api client saw a 401). Clearing state flips the
	 * layout guard, which sends the user back to the login surface.
	 */
	handleSessionExpired(): void {
		this.#clear();
	}

	#clear(): void {
		this.isAuthenticated = false;
	}
}

export const authStore = new AuthStore();
