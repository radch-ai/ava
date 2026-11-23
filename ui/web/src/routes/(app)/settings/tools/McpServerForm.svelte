<script lang="ts">
	import { Button } from '#components/ui/button'
	import { Input } from '#components/ui/input'
	import { Label } from '#components/ui/label'
	import { Textarea } from '#components/ui/textarea'
	import { createMcpServer, updateMcpServer } from '#lib/services/mcp'
	import type {
		McpServer,
		McpServerWithToolsResponse,
		UpdateMcpServerRequest
	} from '#lib/types/mcp'
	import { errorMessage } from '#lib/utils/errors'

	let {
		server = undefined,
		onsaved,
		oncancel
	}: {
		server?: McpServer
		onsaved: (result?: McpServerWithToolsResponse) => void
		oncancel: () => void
	} = $props()

	// Editing starts from the stored row; header values are write-only, so
	// the textarea starts empty and stays empty = keep. The snapshot is
	// deliberate: form fields initialize once from the server prop and never
	// track it.
	// svelte-ignore state_referenced_locally
	let name = $state(server?.name ?? '')
	// svelte-ignore state_referenced_locally
	let url = $state(server?.url ?? '')
	let headersText = $state('')
	// svelte-ignore state_referenced_locally
	let timeoutText = $state<string | number | null>(
		server?.timeoutMs ? String(server.timeoutMs) : ''
	)
	// svelte-ignore state_referenced_locally
	let allowPrivateNetwork = $state(server?.allowPrivateNetwork ?? false)
	// svelte-ignore state_referenced_locally
	let allowPlaintext = $state(server?.allowPlaintext ?? false)
	// An emptied textarea means "keep" by default; the explicit checkbox is
	// the clear path (the wire takes an empty map).
	let clearHeaders = $state(false)
	let saving = $state(false)
	let error = $state<string | null>(null)

	// svelte-ignore state_referenced_locally
	const editing = server !== undefined
	// svelte-ignore state_referenced_locally
	const storedUrl = server?.url ?? ''

	/** "KEY: VALUE" lines → a map; malformed lines are a form error. */
	function parsePairs(text: string, what: string): Record<string, string> {
		const out: Record<string, string> = {}
		for (const line of text.split('\n')) {
			const trimmed = line.trim()
			if (!trimmed) continue
			const sep = trimmed.indexOf(':')
			if (sep === -1)
				throw new Error(`${what}: "${trimmed}" needs "KEY: VALUE"`)
			const key = trimmed.slice(0, sep).trim()
			if (!key) throw new Error(`${what}: a line has an empty key`)
			out[key] = trimmed.slice(sep + 1).trim()
		}
		return out
	}

	// '' is the clear; any other text must parse — garbage yields NaN,
	// which timeoutProblem rejects: never a silent clear.
	function parsedTimeout(): number | undefined {
		const text = timeoutText == null ? '' : String(timeoutText).trim()
		if (text === '') return undefined
		return Number(text)
	}

	// The field only holds whole milliseconds in the backend's range: an
	// out-of-range value is an error, never a silent clear — an empty field
	// is the clear.
	function timeoutProblem(): string | null {
		const value = parsedTimeout()
		if (value === undefined) return null
		if (!Number.isInteger(value) || value < 1 || value > 600000)
			return 'Timeout must be a whole number of milliseconds, 1–600000'
		return null
	}

	// A destination change invalidates its approvals: the app clears both
	// flags with the URL, so the UI mirrors that instead of sending an
	// approval the backend would drop.
	function onUrlInput(): void {
		if (url.trim() === storedUrl) return
		allowPrivateNetwork = false
		allowPlaintext = false
	}

	// The edit patch carries only fields that actually changed: a rename
	// stays a rename — no connection re-verify, no reconnect, no
	// re-discovery.
	function buildPatch(): UpdateMcpServerRequest {
		if (!server) return {}
		const patch: UpdateMcpServerRequest = {}
		if (name.trim() !== server.name) patch.name = name.trim()
		if (url.trim() !== storedUrl) {
			patch.url = url.trim()
			// A new destination was never approved: mirrors the server's
			// reset, and the same save may re-approve it.
			patch.allowPrivateNetwork = allowPrivateNetwork
			patch.allowPlaintext = allowPlaintext
		} else {
			if (allowPrivateNetwork !== server.allowPrivateNetwork)
				patch.allowPrivateNetwork = allowPrivateNetwork
			if (allowPlaintext !== server.allowPlaintext)
				patch.allowPlaintext = allowPlaintext
		}

		// Clearing wins over leftover text: the textarea keeps its state
		// while disabled, so a ticked clear is the operator's latest word.
		if (clearHeaders && (server.headerKeys?.length ?? 0) > 0) patch.headers = {}
		else if (headersText.trim())
			patch.headers = parsePairs(headersText, 'Headers')

		const timeoutMs = parsedTimeout()
		if (timeoutMs !== undefined && timeoutMs !== server.timeoutMs)
			patch.timeoutMs = timeoutMs
		if (timeoutMs === undefined && server.timeoutMs !== undefined)
			patch.clearTimeoutMs = true
		return patch
	}

	async function save(): Promise<void> {
		error = timeoutProblem()
		if (error) return
		saving = true
		try {
			let result: McpServerWithToolsResponse | undefined
			if (server) {
				const patch = buildPatch()
				if (Object.keys(patch).length === 0) {
					// Nothing changed: closing is the honest outcome.
					oncancel()
					return
				}
				result = await updateMcpServer(server.id, patch)
			} else {
				const timeoutMs = parsedTimeout()
				result = await createMcpServer({
					name: name.trim(),
					url: url.trim(),
					...(timeoutMs !== undefined && { timeoutMs }),
					...(allowPrivateNetwork && { allowPrivateNetwork }),
					...(allowPlaintext && { allowPlaintext }),
					headers: parsePairs(headersText, 'Headers')
				})
			}
			// The response carries the listing and the cosmetic skips, so
			// the page can show what the pass left out.
			onsaved(result)
		} catch (e) {
			error = errorMessage(e, 'Failed to save server')
		} finally {
			saving = false
		}
	}
</script>

<form
	class="bg-card space-y-4 rounded-lg border p-4"
	onsubmit={(e) => {
		e.preventDefault()
		void save()
	}}
>
	<div class="space-y-1.5">
		<Label for="mcp-name">Name</Label>
		<Input id="mcp-name" bind:value={name} placeholder="Notes MCP" required />
	</div>

	<div class="space-y-1.5">
		<Label for="mcp-url">URL</Label>
		<Input
			id="mcp-url"
			bind:value={url}
			oninput={onUrlInput}
			placeholder="https://mcp.example.com/mcp"
			required
		/>
	</div>

	<div class="space-y-2 rounded-lg border p-3">
		<p class="text-sm font-medium">Destination approvals</p>
		<label class="flex items-start gap-2 text-xs">
			<input type="checkbox" bind:checked={allowPrivateNetwork}>
			<span>
				Allow non-public destinations (loopback/LAN). Link-local and metadata
				addresses stay blocked.
			</span>
		</label>
		<label class="flex items-start gap-2 text-xs">
			<input type="checkbox" bind:checked={allowPlaintext}>
			<span>
				Allow insecure HTTP including credentials (loopback/LAN only).
			</span>
		</label>
		{#if allowPlaintext}
			<p class="text-destructive text-xs">
				Plain HTTP to a loopback/LAN endpoint carries your credentials in
				cleartext: anyone on that path can read or alter the traffic.
				Public-internet HTTP never receives credentials.
			</p>
		{/if}
	</div>

	<div class="space-y-1.5">
		<Label for="mcp-headers">Headers — KEY: VALUE per line</Label>
		<Textarea
			id="mcp-headers"
			bind:value={headersText}
			rows={3}
			placeholder="Authorization: Bearer …"
			disabled={clearHeaders}
		/>
		{#if editing && (server?.headerKeys?.length ?? 0) > 0}
			<label class="text-muted-foreground flex items-center gap-2 text-xs">
				<input type="checkbox" bind:checked={clearHeaders}>
				Clear the {(server?.headerKeys ?? []).length} sealed value(s)
			</label>
		{/if}
	</div>

	<div class="space-y-1.5">
		<Label for="mcp-timeout">Per-call timeout (ms, optional)</Label>
		<!-- A number input swallows unparseable text before the form
			ever sees it; the field stays text so validation can reject
			it instead of silently clearing. -->
		<Input
			id="mcp-timeout"
			bind:value={timeoutText}
			inputmode="numeric"
			placeholder="30000"
		/>
	</div>

	<p class="text-muted-foreground text-xs">
		{editing
			? 'Saving re-checks the connection when the config changed — an endpoint that cannot be reached or approved is rejected. A rename alone skips the check.'
			: 'The connection is checked before anything is saved — an endpoint that cannot be reached or approved is rejected.'}
	</p>

	{#if error}
		<p class="text-destructive bg-destructive/10 rounded-lg p-3 text-sm">
			{error}
		</p>
	{/if}

	<div class="flex items-center justify-end gap-2 border-t pt-3">
		<Button type="button" variant="outline" onclick={oncancel} disabled={saving}
			>Cancel</Button
		>
		<Button type="submit" disabled={saving}>
			{saving
				? 'Checking connection…'
				: editing
					? 'Save changes'
					: 'Add server'}
		</Button>
	</div>
</form>
