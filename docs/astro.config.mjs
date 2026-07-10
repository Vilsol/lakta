// @ts-check
import { readFileSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';
import starlightLlmsTxt from 'starlight-llms-txt';
import tailwindcss from '@tailwindcss/vite';
import yaml from 'js-yaml';

const __dirname = dirname(fileURLToPath(import.meta.url));
const docsYamlPath = resolve(__dirname, '../docs.yaml');

/** @returns {import('vite').Plugin} */
function laktaDocsPlugin() {
	const virtualId = 'virtual:lakta-docs';
	const resolvedId = '\0' + virtualId;

	return {
		name: 'lakta-docs',
		resolveId(id) {
			if (id === virtualId) return resolvedId;
		},
		load(id) {
			if (id === resolvedId) {
				this.addWatchFile(docsYamlPath);
				const data = yaml.load(readFileSync(docsYamlPath, 'utf-8'));
				return `export default ${JSON.stringify(data)}`;
			}
		},
	};
}

// https://astro.build/config
export default defineConfig({
	site: 'https://vilsol.github.io',
	base: '/lakta',
	integrations: [
		starlight({
			title: 'Lakta',
			logo: { src: './src/assets/lakta.svg', alt: 'Lakta' },
			head: [
				{ tag: 'meta', attrs: { property: 'og:image', content: 'https://vilsol.github.io/lakta/og.png' } },
				{ tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
				{ tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
				{ tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
				{ tag: 'meta', attrs: { name: 'twitter:image', content: 'https://vilsol.github.io/lakta/og.png' } },
			],
			plugins: [
				starlightLinksValidator(),
				starlightLlmsTxt({
					projectName: 'Lakta',
					description: 'An opinionated Go microservice framework: modules, dependency injection, and lifecycle management.',
				}),
			],
			editLink: { baseUrl: 'https://github.com/Vilsol/lakta/edit/main/docs/' },
			lastUpdated: true,
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/Vilsol/lakta' }],
			sidebar: [
				{
					label: 'Getting Started',
					items: [
						{ label: 'Installation', slug: 'getting-started/installation' },
						{ label: 'Your First Service', slug: 'getting-started/first-service' },
						{ label: 'Module Lifecycle', slug: 'getting-started/lifecycle' },
					],
				},
				{
					label: 'Core Concepts',
					items: [
						{ label: 'Modules', slug: 'core-concepts/modules' },
						{ label: 'Runtime', slug: 'core-concepts/runtime' },
						{ label: 'Dependency Injection', slug: 'core-concepts/dependency-injection' },
						{ label: 'Configuration', slug: 'core-concepts/configuration' },
						{ label: 'Multi-instance Modules', slug: 'core-concepts/multi-instance' },
					],
				},
				{
					label: 'Modules',
					items: [
						{ label: 'Logging', slug: 'modules/logging' },
						{ label: 'OpenTelemetry', slug: 'modules/otel' },
						{ label: 'HTTP (Fiber)', slug: 'modules/http' },
						{ label: 'Connect-RPC', slug: 'modules/connect' },
						{ label: 'gRPC Server', slug: 'modules/grpc-server' },
						{ label: 'gRPC Client', slug: 'modules/grpc-client' },
						{ label: 'Database (pgx)', slug: 'modules/database' },
						{ label: 'Health Checks', slug: 'modules/health' },
						{ label: 'Authentication', slug: 'modules/auth' },
						{ label: 'Error Handling', slug: 'modules/errors' },
						{ label: 'Validation', slug: 'modules/validation' },
						{ label: 'Resilience', slug: 'modules/resilience' },
						{ label: 'Caching', slug: 'modules/cache' },
						{ label: 'Event Bus', slug: 'modules/events' },
						{ label: 'Feature Flags', slug: 'modules/feature-flags' },
						{ label: 'Background Workers', slug: 'modules/workers' },
						{ label: 'Debug Actuator', slug: 'modules/actuator' },
						{ label: 'Temporal Workflows', slug: 'modules/temporal' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Writing a Custom Module', slug: 'guides/custom-module' },
						{ label: 'Config Passthrough', slug: 'guides/config-passthrough' },
						{ label: 'Context-aware Logging', slug: 'guides/context-logging' },
						{ label: 'Testing with testkit', slug: 'guides/testing' },
						{ label: 'Generating Config Docs', slug: 'guides/config-docgen' },
						{ label: 'Deploying to Production', slug: 'guides/deployment' },
						{ label: 'Production Checklist', slug: 'guides/production-checklist' },
						{ label: 'Microservices Example', slug: 'guides/microservices-example' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'Config Schema', slug: 'reference/config-schema' },
						{ label: 'Environment Variables', slug: 'reference/env-vars' },
						{ label: 'API Index', slug: 'reference/api-index' },
					],
				},
			],
			customCss: ['./src/styles/global.css'],
		}),
	],
	vite: {
		plugins: [tailwindcss(), laktaDocsPlugin()],
	},
});
