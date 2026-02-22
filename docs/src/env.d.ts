/// <reference types="astro/client" />

declare module 'virtual:lakta-docs' {
	interface Field {
		key: string;
		type: string;
		default?: string;
		required?: boolean;
		envVar: string;
		description?: string;
		enum?: string;
	}

	interface CodeOnlyOption {
		option: string;
		type: string;
		description?: string;
	}

	interface Passthrough {
		targetType: string;
		targetPackage: string;
		targetVersion: string;
		docsUrl: string;
	}

	interface ModuleDoc {
		category: string;
		type: string;
		package: string;
		configPath: string;
		description: string;
		fields: Field[];
		codeOnly?: CodeOnlyOption[];
		passthrough?: Passthrough;
	}

	interface LaktaDocs {
		modules: ModuleDoc[];
	}

	const docs: LaktaDocs;
	export default docs;
}
