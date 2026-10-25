<script lang="ts">
	import LoaderCircle from '@lucide/svelte/icons/loader-circle'
	import { onMount, tick } from 'svelte'
	import { Button } from '#components/ui/button'
	import { Input } from '#components/ui/input'
	import { Label } from '#components/ui/label'
	import { authStore } from '#lib/auth'
	import { APP_NAME } from '#lib/config'
	import { goto } from '$app/navigation'
	import { resolve } from '$app/paths'

	let token = $state('')
	let error = $state<string | null>(null)
	let submitting = $state(false)
	let tokenInput = $state<HTMLInputElement | null>(null)

	onMount(() => tokenInput?.focus())

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault()
		if (!token.trim() || submitting) return
		error = null
		submitting = true
		try {
			await authStore.submitToken(token.trim())
			await goto(resolve('/(app)'))
		} catch (e) {
			error = e instanceof Error ? e.message : 'Login failed'
		} finally {
			submitting = false
			if (error) {
				await tick()
				tokenInput?.focus()
			}
		}
	}
</script>

<div class="flex h-svh items-center justify-center px-4">
	<div class="w-full max-w-sm space-y-6">
		<div class="space-y-2 text-center">
			<h1 class="text-2xl font-semibold">{APP_NAME}</h1>
			<p class="text-muted-foreground text-sm">
				Enter your access token to continue
			</p>
		</div>

		<form class="space-y-4" onsubmit={submit}>
			<div class="space-y-2">
				<Label for="token">Access token</Label>
				<Input
					id="token"
					type="password"
					autocomplete="off"
					placeholder="Paste your token"
					bind:value={token}
					bind:ref={tokenInput}
					disabled={submitting}
				/>
			</div>

			{#if error}
				<p
					class="bg-destructive/10 text-destructive rounded-lg p-3 text-center text-sm"
					role="alert"
				>
					{error}
				</p>
			{/if}

			<Button
				type="submit"
				class="w-full"
				disabled={submitting || !token.trim()}
			>
				{#if submitting}
					<LoaderCircle class="mr-2 size-4 animate-spin" />
				{/if}
				Sign in
			</Button>
		</form>
	</div>
</div>
