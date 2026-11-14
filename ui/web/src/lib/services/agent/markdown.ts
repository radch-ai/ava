/**
 * Assistant answers render as sanitized markdown: `marked` parses,
 * DOMPurify strips anything executable — model output is untrusted
 * input, and `{@html}` without it is an XSS lane. Server-side
 * renders (no DOM) fall back to escaped plain text.
 */

import DOMPurify from 'dompurify'
import { marked } from 'marked'

export function renderMarkdown(source: string): string {
	if (typeof window === 'undefined') {
		return source
			.replace(/&/g, '&amp;')
			.replace(/</g, '&lt;')
			.replace(/>/g, '&gt;')
	}
	return DOMPurify.sanitize(
		marked.parse(source, { async: false, gfm: true, breaks: true })
	)
}
