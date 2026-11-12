import js from '@eslint/js';
import prettier from 'eslint-config-prettier/flat';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';
import tseslint from 'typescript-eslint';

export default [
  {
    ignores: [
      'bindings/',
      'src/lib/generated/',
      'dist/',
      'dist-mock/',
      'node_modules/',
      'test-results/',
      'playwright-report/',
      'package-lock.json',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...svelte.configs.recommended,
  {
    files: ['**/*.svelte'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
      },
    },
    rules: {
      'svelte/no-at-html-tags': 'error',
    },
  },
  {
    files: ['src/**/*.{js,ts,svelte}'],
    languageOptions: {
      globals: globals.browser,
    },
  },
  {
    files: ['*.{js,ts}', 'tests/**/*.{js,ts}'],
    languageOptions: {
      globals: globals.node,
    },
  },
  prettier,
];
