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

	// Editing starts from the stored row; header and env values are
	// write-only, so their textareas start empty and stay empty = keep. The
	// snapshot is deliberate: form fields initialize once from the server
	// prop and never track it.
	// svelte-ignore state_referenced_locally
	let name = $state(server?.name ?? '')
	// svelte-ignore state_referenced_locally
	let transport = $state(server?.transport === 'stdio' ? 'stdio' : 'http')
	// svelte-ignore state_referenced_locally
	let url = $state(server?.url ?? '')
	// svelte-ignore state_referenced_locally
	let command = $state(server?.command ?? '')
	// svelte-ignore state_referenced_locally
	let argsText = $state((server?.args ?? []).join('\n'))
	let headersText = $state('')
	let envText = $state('')
	// svelte-ignore state_referenced_locally
	let timeoutText = $state<string | number | null>(
		server?.timeoutMs ? String(server.timeoutMs) : ''
	)
	// svelte-ignore state_referenced_locally
	let allowPrivateNetwork = $state(server?.allowPrivateNetwork ?? false)
	// svelte-ignore state_referenced_locally
	let allowPlaintext = $state(server?.allowPlaintext ?? false)
	// svelte-ignore state_referenced_locally
	let inheritEnv = $state(server?.inheritEnv ?? false)
	// Installing a process — or changing which one runs — needs the
	// acknowledgment, so it starts unticked and is re-asked on edits.
	let acknowledgeProcess = $state(false)
	// An emptied textarea means "keep" by default; the explicit checkbox is
	// the clear path (the wire takes an empty map).
	let clearHeaders = $state(false)
	let clearEnv = $state(false)
	let saving = $state(false)
	let error = $state<string | null>(null)

	// svelte-ignore state_referenced_locally
	const editing = server !== undefined
	// svelte-ignore state_referenced_locally
	const storedUrl = server?.url ?? ''
	// svelte-ignore state_referenced_locally
	const storedTransport = server?.transport === 'stdio' ? 'stdio' : 'http'
	// svelte-ignore state_referenced_locally
	const storedCommand = server?.command ?? ''
	// svelte-ignore state_referenced_locally
	const storedArgs = server?.args ?? []

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

	/** One argument per line; blank lines are noise. */
	function parseArgs(text: string): string[] {
		return text
			.split('\n')
			.map((line) => line.trim())
			.filter(Boolean)
	}

	// The acknowledgment fingerprint covers command and args: either
	// changing re-prompts, and the backend clears the stored ack when they
	// differ. Installs always ask; unchanged edits do not.
	function ackRequired(): boolean {
		if (transport !== 'stdio') return false
		if (!editing || storedTransport !== 'stdio') return true
		return (
			command.trim() !== storedCommand ||
			parseArgs(argsText).join('\n') !== storedArgs.join('\n')
		)
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
		if (transport !== storedTransport) patch.transport = transport
		if (transport === 'http') {
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
			if (clearHeaders && (server.headerKeys?.length ?? 0) > 0)
				patch.headers = {}
			else if (headersText.trim())
				patch.headers = parsePairs(headersText, 'Headers')
		} else {
			if (command.trim() !== storedCommand) patch.command = command.trim()
			const args = parseArgs(argsText)
			if (args.join('\n') !== storedArgs.join('\n')) patch.args = args
			if (inheritEnv !== server.inheritEnv) patch.inheritEnv = inheritEnv
			if (clearEnv && (server.envKeys?.length ?? 0) > 0) patch.env = {}
			else if (envText.trim()) patch.env = parsePairs(envText, 'Environment')
			if (ackRequired() && acknowledgeProcess)
				patch.acknowledgeProcessExecution = true
		}

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
		if (transport === 'stdio' && ackRequired() && !acknowledgeProcess) {
			error =
				'Acknowledge that this server runs a process with your privileges.'
			return
		}
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
				const stdio = transport === 'stdio'
				result = await createMcpServer({
					name: name.trim(),
					...(stdio
						? {
								transport,
								command: command.trim(),
								args: parseArgs(argsText),
								env: parsePairs(envText, 'Environment'),
								...(inheritEnv && { inheritEnv }),
								acknowledgeProcessExecution: true
							}
						: {
								url: url.trim(),
								...(allowPrivateNetwork && { allowPrivateNetwork }),
								...(allowPlaintext && { allowPlaintext }),
								headers: parsePairs(headersText, 'Headers')
							}),
					...(timeoutMs !== undefined && { timeoutMs })
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

	<fieldset class="space-y-1.5">
		<legend class="text-sm font-medium">Transport</legend>
		<div class="flex items-center gap-4 text-sm">
			<label class="flex items-center gap-2">
				<input
					type="radio"
					name="mcp-transport"
					value="http"
					bind:group={transport}
				>
				<span>HTTP</span>
			</label>
			<label class="flex items-center gap-2">
				<input
					type="radio"
					name="mcp-transport"
					value="stdio"
					bind:group={transport}
				>
				<span>stdio (local process)</span>
			</label>
		</div>
	</fieldset>

	{#if transport === 'http'}
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
	{:else}
		<div class="space-y-1.5">
			<Label for="mcp-command">Command</Label>
			<Input id="mcp-command" bind:value={command} placeholder="npx" required />
			<p class="text-muted-foreground text-xs">
				The executable is found through <code>PATH</code>; a bare name like
				<code>npx</code>
				or <code>uvx</code> is enough.
			</p>
		</div>

		<div class="space-y-1.5">
			<Label for="mcp-args">Arguments — one per line</Label>
			<Textarea
				id="mcp-args"
				bind:value={argsText}
				rows={3}
				placeholder="-y&#10;@modelcontextprotocol/server-filesystem&#10;/home/me/notes"
			/>
		</div>

		<div class="space-y-1.5">
			<Label for="mcp-env">Environment — KEY: VALUE per line</Label>
			<Textarea
				id="mcp-env"
				bind:value={envText}
				rows={3}
				placeholder="API_TOKEN: …"
				disabled={clearEnv}
			/>
			{#if editing && (server?.envKeys?.length ?? 0) > 0}
				<label class="text-muted-foreground flex items-center gap-2 text-xs">
					<input type="checkbox" bind:checked={clearEnv}>
					Clear the {(server?.envKeys ?? []).length} sealed value(s)
				</label>
			{/if}
			<p class="text-muted-foreground text-xs">
				Sealed values are handed only to the spawned process — never shown back.
			</p>
		</div>

		<div class="space-y-2 rounded-lg border p-3">
			<label class="flex items-start gap-2 text-xs">
				<input type="checkbox" bind:checked={inheritEnv}>
				<span>
					Inherit ava's own environment. The child would see every variable ava
					runs with — including its secrets master key. Leave this off unless
					the server needs a variable that cannot be sealed below.
				</span>
			</label>
			{#if inheritEnv}
				<p class="text-destructive text-xs">
					Every ava process variable, secrets included, becomes readable by this
					server.
				</p>
			{/if}
			<label class="flex items-start gap-2 text-xs">
				<input
					type="checkbox"
					bind:checked={acknowledgeProcess}
					disabled={!ackRequired()}
				>
				<span>
					I understand this server runs
					<code>{command.trim() || 'the command'}</code>
					as a process with my own privileges — no sandbox, no containment.
				</span>
			</label>
			{#if !ackRequired()}
				<p class="text-muted-foreground text-xs">
					The stored process is already acknowledged; changing the command or
					arguments asks again.
				</p>
			{/if}
		</div>
	{/if}

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
			? 'Saving re-checks the connection when the config changed — a server that cannot start or be reached is rejected. A rename alone skips the check.'
			: 'The connection is checked before anything is saved — a server that cannot start or be reached is rejected.'}
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
