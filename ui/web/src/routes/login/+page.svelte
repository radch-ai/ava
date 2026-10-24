<script lang="ts">
	import { goto } from '$app/navigation';
	import { Button } from '$components/ui/button';
	import { Input } from '$components/ui/input';
	import { Label } from '$components/ui/label';
	import LoaderCircle from '@lucide/svelte/icons/loader-circle';
	import { authStore } from '$lib/auth';
	import { APP_NAME } from '$lib/config';

	let token = $state('');
	let error = $state<string | null>(null);
	let submitting = $state(false);

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		if (!token.trim() || submitting) return;
		error = null;
		submitting = true;
		try {
			await authStore.submitToken(token.trim());
			await goto('/');
		} catch (e) {
			error = e instanceof Error ? e.message : 'Login failed';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex h-svh items-center justify-center px-4">
	<div class="w-full max-w-sm space-y-6">
		<div class="space-y-2 text-center">
			<h1 class="text-2xl font-semibold">{APP_NAME}</h1>
			<p class="text-sm text-muted-foreground">Enter your access token to continue</p>
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
					disabled={submitting}
				/>
			</div>

			{#if error}
				<p
					class="rounded-lg bg-destructive/10 p-3 text-center text-sm text-destructive"
					role="alert"
				>
					{error}
				</p>
			{/if}

			<Button type="submit" class="w-full" disabled={submitting || !token.trim()}>
				{#if submitting}
					<LoaderCircle class="mr-2 size-4 animate-spin" />
				{/if}
				Sign in
			</Button>
		</form>
	</div>
</div>
