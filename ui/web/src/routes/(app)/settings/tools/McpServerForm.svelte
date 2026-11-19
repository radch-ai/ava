<script lang="ts">
	import { Button } from '#components/ui/button'
	import { Input } from '#components/ui/input'
	import { Label } from '#components/ui/label'
	import * as Select from '#components/ui/select'
	import { Textarea } from '#components/ui/textarea'
	import { createMcpServer, updateMcpServer } from '#lib/services/mcp'
	import type { McpServer, UpdateMcpServerRequest } from '#lib/types/mcp'
	import { errorMessage } from '#lib/utils/errors'

	let {
		server = undefined,
		onsaved,
		oncancel
	}: {
		server?: McpServer
		onsaved: () => void
		oncancel: () => void
	} = $props()

	// Editing starts from the stored row; env/header values are
	// write-only, so the textareas start empty and stay empty = keep.
	// The snapshot is deliberate: form fields initialize once from the
	// server prop and never track it.
	// svelte-ignore state_referenced_locally
	let name = $state(server?.name ?? '')
	// svelte-ignore state_referenced_locally
	let transport = $state<'stdio' | 'http'>(
		server?.transport === 'http' ? 'http' : 'stdio'
	)
	// svelte-ignore state_referenced_locally
	let command = $state(server?.command ?? '')
	// svelte-ignore state_referenced_locally
	let argsText = $state((server?.args ?? []).join('\n'))
	// svelte-ignore state_referenced_locally
	let url = $state(server?.url ?? '')
	let envText = $state('')
	let headersText = $state('')
	// svelte-ignore state_referenced_locally
	// Bound to a number input: empty reads as '' before typing and null
	// after the browser clears it, so both mean "no timeout".
	let timeoutText = $state<string | number | null>(
		server?.timeoutMs ? String(server.timeoutMs) : ''
	)
	// An emptied map textarea means "keep" by default; the explicit
	// checkbox is the clear path (the wire takes an empty map).
	let clearEnv = $state(false)
	let clearHeaders = $state(false)
	let saving = $state(false)
	let error = $state<string | null>(null)

	// svelte-ignore state_referenced_locally
	const editing = server !== undefined
	const ARGS_PLACEHOLDER = '--stdio\n--verbose'

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

	function parsedArgs(): string[] {
		return argsText
			.split('\n')
			.map((line) => line.trim())
			.filter(Boolean)
	}

	function parsedTimeout(): number | undefined {
		const text = timeoutText == null ? '' : String(timeoutText).trim()
		if (text === '') return undefined
		const value = Number(text)
		return Number.isFinite(value) ? value : undefined
	}

	// The field only holds whole milliseconds in the backend's range
	// (mcpservers.go:29,60-68): out-of-range input is an error, never a
	// silent clear — an empty field is the clear.
	function timeoutProblem(): string | null {
		const value = parsedTimeout()
		if (value === undefined) return null
		if (!Number.isInteger(value) || value < 1 || value > 600000)
			return 'Timeout must be a whole number of milliseconds, 1–600000'
		return null
	}

	// The edit patch carries only fields that actually changed: a rename
	// stays a rename — no connection re-verify, no reconnect, no
	// re-discovery (the server process is untouched).
	function buildPatch(): UpdateMcpServerRequest {
		if (!server) return {}
		const patch: UpdateMcpServerRequest = {}
		if (name.trim() !== server.name) patch.name = name.trim()
		if (transport !== server.transport) patch.transport = transport
		if (transport === 'stdio') {
			const nextCommand = command.trim()
			if (nextCommand !== (server.command ?? '')) patch.command = nextCommand
			const args = parsedArgs()
			if (args.join('\n') !== (server.args ?? []).join('\n')) patch.args = args
		} else if (url.trim() !== (server.url ?? '')) {
			patch.url = url.trim()
		}

		// Typed content replaces the stored map wholesale; an
		// explicit clear sends the empty map; otherwise the field stays
		// absent and the stored map survives.
		if (envText.trim()) patch.env = parsePairs(envText, 'Environment')
		else if (clearEnv && (server.envKeys?.length ?? 0) > 0) patch.env = {}
		if (headersText.trim()) patch.headers = parsePairs(headersText, 'Headers')
		else if (clearHeaders && (server.headerKeys?.length ?? 0) > 0)
			patch.headers = {}

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
			if (server) {
				const patch = buildPatch()
				if (Object.keys(patch).length === 0) {
					// Nothing changed: closing is the honest outcome.
					oncancel()
					return
				}
				await updateMcpServer(server.id, patch)
			} else {
				const timeoutMs = parsedTimeout()
				await createMcpServer({
					name,
					transport,
					command,
					args: parsedArgs(),
					url,
					...(timeoutMs !== undefined && { timeoutMs }),
					env: parsePairs(envText, 'Environment'),
					headers: parsePairs(headersText, 'Headers')
				})
			}
			onsaved()
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
	<div class="grid gap-3 sm:grid-cols-2">
		<div class="space-y-1.5">
			<Label for="mcp-name">Name</Label>
			<Input id="mcp-name" bind:value={name} placeholder="Notes MCP" required />
		</div>
		<div class="space-y-1.5">
			<Label for="mcp-transport">Transport</Label>
			<Select.Root type="single" bind:value={transport}>
				<Select.Trigger id="mcp-transport" class="w-full">
					{transport === 'stdio'
						? 'stdio — local command'
						: 'http — streamable HTTP'}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="stdio" label="stdio — local command" />
					<Select.Item value="http" label="http — streamable HTTP" />
				</Select.Content>
			</Select.Root>
		</div>
	</div>

	{#if transport === 'stdio'}
		<div class="space-y-1.5">
			<Label for="mcp-command">Command</Label>
			<Input
				id="mcp-command"
				bind:value={command}
				placeholder="notes-mcp"
				required
			/>
		</div>
		<div class="grid gap-3 sm:grid-cols-2">
			<div class="space-y-1.5">
				<Label for="mcp-args">Arguments — one per line</Label>
				<Textarea
					id="mcp-args"
					bind:value={argsText}
					rows={3}
					placeholder={ARGS_PLACEHOLDER}
				/>
			</div>
			<div class="space-y-1.5">
				<Label for="mcp-env">Environment — KEY: VALUE per line</Label>
				<Textarea
					id="mcp-env"
					bind:value={envText}
					rows={3}
					placeholder="NOTES_TOKEN: abc123"
					disabled={clearEnv}
				/>
				{#if editing && (server?.envKeys?.length ?? 0) > 0}
					<label class="text-muted-foreground flex items-center gap-2 text-xs">
						<input type="checkbox" bind:checked={clearEnv}>
						Clear the {(server?.envKeys ?? []).length} sealed value(s)
					</label>
				{/if}
			</div>
		</div>
	{:else}
		<div class="space-y-1.5">
			<Label for="mcp-url">URL</Label>
			<Input
				id="mcp-url"
				bind:value={url}
				placeholder="https://mcp.example.com/mcp"
				required
			/>
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
	{/if}

	<div class="space-y-1.5">
		<Label for="mcp-timeout">Per-call timeout (ms, optional)</Label>
		<Input
			id="mcp-timeout"
			bind:value={timeoutText}
			type="number"
			min={1}
			max={600000}
			placeholder="30000"
		/>
	</div>

	<p class="text-muted-foreground text-xs">
		{editing
			? 'Saving re-checks the connection when the config changed — a server that cannot start is rejected. A rename alone skips the check.'
			: 'The connection is checked before anything is saved — a server that cannot start is rejected.'}
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
