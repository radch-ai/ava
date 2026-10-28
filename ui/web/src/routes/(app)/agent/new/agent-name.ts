/**
 * Agent display-name rule: ASCII letters, digits and single spaces between
 * words (trim first). Declared once — the create form and the mock API
 * handlers both use it.
 */
export function isValidAgentName(name: string): boolean {
	return /^[A-Za-z0-9]+(?: [A-Za-z0-9]+)*$/.test(name.trim())
}
