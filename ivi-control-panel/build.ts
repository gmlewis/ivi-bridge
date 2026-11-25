import { build } from 'gpuix-svelte/build';

await build({
	entry: './standalone.ts',
	archive: process.argv.includes('--archive'),
	aot: process.argv.includes('--aot'),
	trim: process.argv.includes('--trim')
});
