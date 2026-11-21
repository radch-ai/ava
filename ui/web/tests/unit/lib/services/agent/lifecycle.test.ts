import { describe, expect, it } from 'vitest'
import { parseLifecycleEvent } from '#lib/services/agent/lifecycle'

// The durable feed arrives as raw JSON: the surface must reduce only shapes it
// understands, and drop anything else rather than crash on a newer gateway.

describe('parseLifecycleEvent', () => {
	it('accepts every kind this surface reduces, keyed by its identity', () => {
		for (const kind of [
			'item_started',
			'message_committed',
			'thread_changed'
		]) {
			expect(
				parseLifecycleEvent({ kind, itemId: 'item-1', pos: 1, threadId: 't-1' })
			).toEqual({ kind, itemId: 'item-1', pos: 1, threadId: 't-1' })
		}
		for (const kind of ['tool_started', 'tool_finished']) {
			expect(
				parseLifecycleEvent({
					kind,
					itemId: 'item-1',
					pos: 1,
					invocationId: 'inv-1'
				})
			).toEqual({ kind, itemId: 'item-1', pos: 1, invocationId: 'inv-1' })
		}
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'item-1',
				pos: 7,
				threadId: 't-1',
				outcome: 'completed',
				rows: []
			})
		).toMatchObject({ kind: 'item_finished', outcome: 'completed' })
		// The admission fact reduces to no case: the decoder drops it.
		expect(
			parseLifecycleEvent({
				kind: 'item_accepted',
				itemId: 'item-1',
				pos: 1,
				threadId: 't-1'
			})
		).toBeUndefined()
	})

	it('places committed rows only with the thread they belong to', () => {
		// A tool outcome that carries its committed row names the thread.
		expect(
			parseLifecycleEvent({
				kind: 'tool_finished',
				itemId: 'item-1',
				pos: 3,
				invocationId: 'inv-1',
				threadId: 't-1',
				rows: [{ id: 'r-1', role: 'tool', content: '{}' }]
			})
		).toMatchObject({ kind: 'tool_finished', threadId: 't-1' })
		expect(
			parseLifecycleEvent({
				kind: 'tool_finished',
				itemId: 'item-1',
				pos: 3,
				invocationId: 'inv-1',
				rows: [{ id: 'r-1', role: 'tool', content: '{}' }]
			})
		).toBeUndefined()
		// A rowless tool fact stays live-only: no thread needed.
		expect(
			parseLifecycleEvent({
				kind: 'tool_finished',
				itemId: 'item-1',
				pos: 3,
				invocationId: 'inv-1'
			})
		).toMatchObject({ kind: 'tool_finished', invocationId: 'inv-1' })
	})

	it('accepts a threadless cancellation of a never-started submission', () => {
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'item-1',
				pos: 4,
				outcome: 'cancelled',
				reason: 'service_restart'
			})
		).toMatchObject({ kind: 'item_finished', outcome: 'cancelled' })
		// A cancellation that carries rows still keys its thread.
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'item-1',
				pos: 4,
				outcome: 'cancelled',
				rows: [{ id: 'r-1', role: 'user', content: 'hi' }]
			})
		).toBeUndefined()
		// Every other terminal outcome requires its thread.
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'item-1',
				pos: 4,
				outcome: 'completed'
			})
		).toBeUndefined()
	})

	it('drops an event missing the identity its kind keys on', () => {
		expect(
			parseLifecycleEvent({ kind: 'item_started', itemId: 'i', pos: 1 })
		).toBeUndefined()
		expect(
			parseLifecycleEvent({
				kind: 'item_started',
				itemId: 'i',
				pos: 1,
				threadId: ''
			})
		).toBeUndefined()
		expect(
			parseLifecycleEvent({
				kind: 'tool_started',
				itemId: 'i',
				pos: 1,
				threadId: 't-1'
			})
		).toBeUndefined()
		expect(
			parseLifecycleEvent({
				kind: 'tool_finished',
				itemId: 'i',
				pos: 1,
				invocationId: ''
			})
		).toBeUndefined()
	})

	it('drops an unknown kind instead of failing the batch', () => {
		expect(
			parseLifecycleEvent({ kind: 'item_teleported', itemId: 'i', pos: 1 })
		).toBeUndefined()
	})

	it('drops malformed payloads', () => {
		expect(parseLifecycleEvent(undefined)).toBeUndefined()
		expect(parseLifecycleEvent(null)).toBeUndefined()
		expect(parseLifecycleEvent('item_started')).toBeUndefined()
		expect(
			parseLifecycleEvent({ kind: 'item_started', pos: 1 })
		).toBeUndefined()
		expect(
			parseLifecycleEvent({ kind: 'item_started', itemId: '', pos: 1 })
		).toBeUndefined()
		expect(
			parseLifecycleEvent({ kind: 'item_started', itemId: 'i' })
		).toBeUndefined()
		expect(
			parseLifecycleEvent({
				kind: 'item_started',
				itemId: 'i',
				pos: 1,
				threadId: 't-1',
				rows: 'x'
			})
		).toBeUndefined()
	})

	it('requires a known terminal outcome on item_finished', () => {
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'i',
				pos: 1,
				threadId: 't-1',
				outcome: 'vanished'
			})
		).toBeUndefined()
		// A terminal event with no outcome at all is equally malformed.
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'i',
				pos: 1,
				threadId: 't-1'
			})
		).toBeUndefined()
		for (const outcome of [
			'completed',
			'failed',
			'stopped',
			'cancelled',
			'interrupted'
		]) {
			expect(
				parseLifecycleEvent({
					kind: 'item_finished',
					itemId: 'i',
					pos: 1,
					threadId: 't-1',
					outcome
				})
			).toMatchObject({ outcome })
		}
	})

	it('drops an event whose committed rows cannot be rendered', () => {
		// One malformed row poisons the whole event: the display would
		// otherwise hand a non-string row straight to the renderer.
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'i',
				pos: 1,
				threadId: 't-1',
				outcome: 'completed',
				rows: [{ id: 1 }]
			})
		).toBeUndefined()
		expect(
			parseLifecycleEvent({
				kind: 'item_finished',
				itemId: 'i',
				pos: 1,
				threadId: 't-1',
				outcome: 'completed',
				rows: [
					{ id: 'row-1', role: 'assistant', content: 'answer' },
					{ id: 'row-2', role: 'tool', content: 'tool', extra: null }
				]
			})
		).toMatchObject({ rows: [{ id: 'row-1' }, { id: 'row-2' }] })
	})
})
