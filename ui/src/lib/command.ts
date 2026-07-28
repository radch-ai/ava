export { commands } from '$lib/bindings';
export type * from '$lib/bindings';

export type CommandResult<T, E = unknown> =
	| { status: 'ok'; data: T }
	| { status: 'error'; error: E };

/** Unwrap a tauri-specta command result; throws Error with a display message on failure. */
export function unwrapCommand<T, E = unknown>(result: CommandResult<T, E>): T {
	if (result.status === 'error') {
		throw new Error(formatCommandError(result.error));
	}
	return result.data;
}

function formatCommandError(error: unknown): string {
	if (error && typeof error === 'object' && 'message' in error) {
		const message = (error as { message?: unknown }).message;
		if (typeof message === 'string' && message.length > 0) return message;
	}
	if (typeof error === 'string' && error.length > 0) return error;
	return 'Command failed';
}
