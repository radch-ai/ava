/**
 * One message for a caught value: an Error's own message when it has one,
 * the caller's fallback otherwise. The api client wraps failures in Error,
 * so the fallback only covers thrown primitives.
 */
export function errorMessage(e: unknown, fallback: string): string {
	return e instanceof Error && e.message ? e.message : fallback
}
