import { render } from 'gpuix-svelte';
import App from './App.svelte';

render(App, {
	title: 'IVI Control Panel',
	width: 1080,
	height: 820,
	app: {
		name: 'IVI Control Panel',
		identifier: 'com.gmlewis.ivi-control-panel',
		icon: './assets/icon.png'
	}
});
