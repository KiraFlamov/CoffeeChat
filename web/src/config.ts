/** Адреса сервера. Подставляются при сборке из переменных VITE_API_URL и VITE_WS_URL. */
export interface Config {
  readonly apiUrl: string;
  readonly wsUrl: string;
}

export function loadConfig(env: ImportMetaEnv = import.meta.env): Config {
  const apiUrl = env.VITE_API_URL;
  const wsUrl = env.VITE_WS_URL;
  if (!apiUrl || !wsUrl) {
    throw new Error('Не заданы переменные сборки VITE_API_URL и VITE_WS_URL');
  }
  return { apiUrl, wsUrl };
}
