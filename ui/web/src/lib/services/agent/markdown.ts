/**
 * Assistant answers render as sanitized markdown: `marked` parses,
 * DOMPurify strips anything executable — model output is untrusted
 * input, and `{@html}` without it is an XSS lane. The app is a pure
 * SPA (ssr = false, adapter-static): this always runs in a browser,
 * so no server fallback exists.
 */

import DOMPurify from 'dompurify'
import { marked } from 'marked'

export function renderMarkdown(source: string): string {
	return DOMPurify.sanitize(
		marked.parse(source, { async: false, gfm: true, breaks: true })
	)
}
