<script lang="ts">
  import { invalidate } from '$app/navigation'
  import ky from '$lib/api/ky'
  import Button from '$lib/lunaria/Button.svelte'
  import TextInput from '$lib/lunaria/TextInput.svelte'

  let login = $state({ username: '', password: '' })
  let disabled = $state(false)
  let error: string | null = $state(null)

  const clearError = () => (error = null)

  async function onLogin() {
    disabled = true
    try {
      const res = await ky
        .post(`api/login`, { json: login })
        .json<{ token: string; username: string }>()
      localStorage.setItem('concinnity:token', res.token)
      error = ''
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : (e?.toString() ?? `Failed to login!`)
    }
    disabled = false
    if (!error) {
      invalidate('app:auth').catch(console.error)
    }
  }
</script>

<h2>Login</h2>
<div class="spacer"></div>
<label for="login-username">E-mail / Username</label>
<TextInput
  id="login-username"
  bind:value={login.username}
  oninput={clearError}
  error={!!error}
  {disabled}
  type="email"
  placeholder="e.g. aelia@retrixe.xyz"
/>
<label for="login-password">Password</label>
<TextInput
  id="login-password"
  bind:value={login.password}
  oninput={clearError}
  error={!!error}
  {disabled}
  type="password"
  onkeypress={e => e.key === 'Enter' && onLogin() /* eslint-disable-line */}
/>
{#if error === ''}
  <p class="result">Logged in successfully! You should be redirected shortly...</p>
{:else if !!error}
  <p class="result error">{error}</p>
{/if}
<div class="spacer"></div>
<Button {disabled} onclick={onLogin}>Login</Button>
<div class="spacer"></div>
<p>Don't have an account? <a href="/register">Sign up</a></p>
