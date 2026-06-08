<script>
  import { GLOBAL_PROJECT } from '$lib/colors.js';
  let { name } = $props();
  let isGlobal = $derived(name === GLOBAL_PROJECT || !name);
  let label = $derived(name || GLOBAL_PROJECT);
</script>

<span class="proj-chip mono" class:global={isGlobal}>{label}</span>

<style>
  .proj-chip {
    font-family: var(--font-mono); font-size: 11.5px; color: var(--accent);
    border: 1px solid var(--accent-line); border-radius: 20px; padding: 1px 9px;
    white-space: nowrap;
  }
  .proj-chip.global {
    color: var(--e-preference);
    border-color: color-mix(in oklab, var(--e-preference) 40%, transparent);
  }
</style>
