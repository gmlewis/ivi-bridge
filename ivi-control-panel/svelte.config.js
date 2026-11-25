export default {
	compilerOptions: {
		warningFilter: (warning) => !warning.code.startsWith('a11y_')
	}
};
