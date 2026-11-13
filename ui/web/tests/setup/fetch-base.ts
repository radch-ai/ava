/**
 * Node's Request rejects relative URLs; resolve string inputs against a
 * fixed base so the api client's same-origin '/api' default works unchanged
 * under msw/node. Request/URL instances pass through untouched.
 */
const NodeRequest = globalThis.Request
const BASE = 'http://localhost'

globalThis.Request = class extends NodeRequest {
	constructor(input: RequestInfo | URL, init?: RequestInit) {
		super(typeof input === 'string' ? new URL(input, BASE).href : input, init)
	}
}
