import js from '@eslint/js';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    ignores: [
      '**/node_modules/**',
      '**/dist/**',
      '**/coverage/**',
      'packages/proto/gen/**',
      'server/**',
      'spike/**',
      'docs/**',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.strict,
  {
    rules: {
      // Правило проекта: без any вне сгенерированного кода.
      '@typescript-eslint/no-explicit-any': 'error',
      // Правило безопасности: случайность только из crypto.getRandomValues.
      'no-restricted-properties': [
        'error',
        {
          object: 'Math',
          property: 'random',
          message: 'Используй crypto.getRandomValues: Math.random запрещён правилами безопасности.',
        },
      ],
    },
  },
);
