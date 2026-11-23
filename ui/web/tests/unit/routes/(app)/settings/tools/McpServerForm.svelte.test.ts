import {
	afterEach,
	beforeEach,
	describe,
	expect,
	it,
	type Mock,
	vi
} from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import * as mcpService from '#lib/services/mcp'
import type { McpServer } from '#lib/types/mcp'
import McpServerForm from '#routes/settings/tools/McpServerForm.svelte'

// The form never hits the network; the service layer is a spy.
vi.mock('#lib/services/mcp', () => ({
	createMcpServer: vi.fn(),
	updateMcpServer: vi.fn()
}))

// One HTTP server with a loopback endpoint and both approvals granted:
// the state an operator reaches by editing the flags.
const server: McpServer = {
	id: 'notes-mcp',
	name: 'Notes MCP',
	url: 'http://127.0.0.1:3333/mcp',
	headerKeys: ['Authorization'],
	enabled: true,
	toolCount: 2,
	catalogState: 'active',
	allowPrivateNetwork: true,
	allowPlaintext: true
}

/** The two callbacks the form reports through. */
interface FormCallbacks {
	onsaved: Mock
	oncancel: Mock
}

function renderForm(
	row: McpServer = server,
	oncancel = vi.fn()
): FormCallbacks {
	const onsaved = vi.fn()
	render(McpServerForm, { server: row, onsaved, oncancel })
	return { onsaved, oncancel }
}

describe('McpServerForm edit patch', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('sends only the name for a rename — the connection fields stay absent', async () => {
		const { onsaved } = renderForm()
		await page.getByLabelText('Name').fill('Notes MCP Renamed')
		await page.getByRole('button', { name: 'Save changes' }).click()

		// The patch shape is the claim: a rename carries no connection
		// fields, whatever the shared browser page's render count.
		expect(mcpService.updateMcpServer).toHaveBeenCalledWith('notes-mcp', {
			name: 'Notes MCP Renamed'
		})
		expect(onsaved).toHaveBeenCalledOnce()
	})

	it('closes without a request when nothing changed', async () => {
		const { oncancel, onsaved } = renderForm()
		await page.getByRole('button', { name: 'Save changes' }).click()

		expect(mcpService.updateMcpServer).not.toHaveBeenCalled()
		expect(onsaved).not.toHaveBeenCalled()
		expect(oncancel).toHaveBeenCalledOnce()
	})

	it('replaces typed headers and sends the empty map when the clear box is checked', async () => {
		renderForm()
		await page
			.getByLabelText('Headers — KEY: VALUE per line')
			.fill('Authorization: Bearer abc')
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ headers: { Authorization: 'Bearer abc' } }
		)

		vi.clearAllMocks()
		cleanup()
		renderForm()
		await page.getByRole('checkbox', { name: /Clear the 1 sealed/ }).click()
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ headers: {} }
		)
	})

	it('lets a ticked clear win over leftover header text', async () => {
		renderForm()
		await page
			.getByLabelText('Headers — KEY: VALUE per line')
			.fill('Authorization: Bearer abc')
		// The textarea keeps its state while disabled: the tick is the
		// operator's latest word, so the clear must win.
		await page.getByRole('checkbox', { name: /Clear the 1 sealed/ }).click()
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ headers: {} }
		)
	})

	it('clears a stored timeout when the field is emptied', async () => {
		const onsaved = vi.fn()
		render(McpServerForm, {
			server: { ...server, timeoutMs: 30000 },
			onsaved,
			oncancel: vi.fn()
		})
		await page.getByLabelText('Per-call timeout (ms, optional)').fill('')
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ clearTimeoutMs: true }
		)
	})

	// Out-of-range input is blocked before any request: the form's own
	// range guard mirrors the backend rule.
	it('never sends an out-of-range timeout', async () => {
		const { oncancel } = renderForm()
		const field = page.getByLabelText('Per-call timeout (ms, optional)')
		for (const value of ['0', '600001']) {
			await field.fill(value)
			await page.getByRole('button', { name: 'Save changes' }).click()
		}

		await vi.waitFor(() =>
			expect(mcpService.updateMcpServer).not.toHaveBeenCalled()
		)
		expect(oncancel).not.toHaveBeenCalled()
	})

	// Present-but-unparseable input is an error, never a silent clear:
	// the save is blocked before any request leaves the form.
	it('rejects garbage in the timeout field instead of clearing it', async () => {
		const { oncancel } = renderForm()
		await page.getByLabelText('Per-call timeout (ms, optional)').fill('12x')
		await page.getByRole('button', { name: 'Save changes' }).click()

		await expect
			.element(page.getByText(/Timeout must be a whole number/))
			.toBeInTheDocument()
		expect(mcpService.updateMcpServer).not.toHaveBeenCalled()
		expect(oncancel).not.toHaveBeenCalled()
	})

	it('names malformed secret lines instead of dropping them', async () => {
		renderForm()
		await page.getByLabelText('Headers — KEY: VALUE per line').fill('TOKEN=abc')
		await page.getByRole('button', { name: 'Save changes' }).click()

		expect(mcpService.updateMcpServer).not.toHaveBeenCalled()
		await expect
			.element(page.getByText(/Headers: "TOKEN=abc" needs "KEY: VALUE"/))
			.toBeInTheDocument()
	})

	it('diffs both approvals, including an explicit clear', async () => {
		renderForm()
		await page
			.getByRole('checkbox', {
				name: /Allow non-public destinations/
			})
			.click()
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ allowPrivateNetwork: false }
		)

		vi.clearAllMocks()
		cleanup()
		renderForm({ ...server, allowPlaintext: false })
		await page.getByRole('checkbox', { name: /Allow insecure HTTP/ }).click()
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ allowPlaintext: true }
		)
	})

	// A destination change invalidates its approvals: the UI mirrors the
	// server's reset instead of sending an approval it would drop.
	it('resets both approvals when the URL changes', async () => {
		renderForm()
		await page.getByLabelText('URL').fill('https://mcp.example.com/mcp')
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{
				url: 'https://mcp.example.com/mcp',
				allowPrivateNetwork: false,
				allowPlaintext: false
			}
		)
	})

	it('re-approves a changed destination in the same save', async () => {
		renderForm()
		await page.getByLabelText('URL').fill('https://mcp.example.com/mcp')
		// The reset ran on input; the operator approves the new endpoint.
		await page.getByRole('checkbox', { name: /Allow non-public/ }).click()
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{
				url: 'https://mcp.example.com/mcp',
				allowPrivateNetwork: true,
				allowPlaintext: false
			}
		)
	})
})

// The create path asks for an endpoint only: no process is ever spawned.
describe('McpServerForm create', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('creates an HTTP server with headers and approvals', async () => {
		const onsaved = vi.fn()
		render(McpServerForm, { onsaved, oncancel: vi.fn() })

		await page.getByLabelText('Name').fill('Search MCP')
		await page.getByLabelText('URL').fill('http://192.168.1.50/mcp')
		await page
			.getByLabelText('Headers — KEY: VALUE per line')
			.fill('Authorization: Bearer abc')
		await page.getByRole('checkbox', { name: /Allow non-public/ }).click()
		await page.getByRole('checkbox', { name: /Allow insecure HTTP/ }).click()
		await page.getByRole('button', { name: 'Add server' }).click()

		expect(mcpService.createMcpServer).toHaveBeenCalledExactlyOnceWith({
			name: 'Search MCP',
			url: 'http://192.168.1.50/mcp',
			allowPrivateNetwork: true,
			allowPlaintext: true,
			headers: { Authorization: 'Bearer abc' }
		})
		expect(onsaved).toHaveBeenCalledOnce()
	})

	it('trims the name and URL before they leave the form', async () => {
		render(McpServerForm, { onsaved: vi.fn(), oncancel: vi.fn() })

		await page.getByLabelText('Name').fill('  Notes  ')
		await page.getByLabelText('URL').fill(' http://127.0.0.1:4444/mcp ')
		await page.getByRole('button', { name: 'Add server' }).click()

		expect(mcpService.createMcpServer).toHaveBeenCalledExactlyOnceWith({
			name: 'Notes',
			url: 'http://127.0.0.1:4444/mcp',
			headers: {}
		})
	})
})

// The save response carries the listing and its cosmetic skips, so the
// form hands it back to the page instead of dropping it.
describe('McpServerForm saved response', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('passes the created listing back to the page', async () => {
		const created = {
			server: { ...server, id: 'new-mcp', name: 'New MCP' },
			tools: [],
			skippedTools: [{ name: 'too_long', reason: 'exceeds 64 characters' }]
		}
		vi.mocked(mcpService.createMcpServer).mockResolvedValue(created)
		const onsaved = vi.fn()
		render(McpServerForm, { onsaved, oncancel: vi.fn() })

		await page.getByLabelText('Name').fill('New MCP')
		await page.getByLabelText('URL').fill('http://127.0.0.1:4444/mcp')
		await page.getByRole('button', { name: 'Add server' }).click()

		await vi.waitFor(() => {
			expect(onsaved).toHaveBeenCalledWith(created)
		})
	})
})
