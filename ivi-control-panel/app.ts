import { render_hot } from 'gpuix-svelte';

render_hot(new URL('./App.svelte', import.meta.url), {
	title: 'IVI Control Panel',
	width: 1080,
	height: 820
});
