import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { loadEnv } from 'vite';
import { defineConfig } from 'vitest/config';

// Адреса API подставляются при сборке и в исходниках не хранятся.
const requiredEnv = ['VITE_API_URL', 'VITE_WS_URL'] as const;

export default defineConfig(({ command, mode }) => {
  if (command === 'build') {
    // Сборка без адресов падает сразу, а не подставляет адрес по умолчанию.
    // Файлы .env ищутся там же, где их ищет сам Vite: в каталоге запуска (web/).
    const env = loadEnv(mode, '.', 'VITE_');
    const missing = requiredEnv.filter((name) => !env[name]);
    if (missing.length > 0) {
      throw new Error(
        `Не заданы переменные сборки: ${missing.join(', ')}. ` +
          'Адреса стенда берутся из вывода Terraform (api_url, ws_url).',
      );
    }
  }
  return {
    plugins: [react(), tailwindcss()],
    server: {
      // Порт закреплён: на этот origin настроен CORS локального сервера.
      port: 5173,
      strictPort: true,
    },
    worker: {
      format: 'es',
    },
    test: {
      environment: 'jsdom',
    },
  };
});
