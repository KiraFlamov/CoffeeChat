/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Адрес HTTP API. */
  readonly VITE_API_URL?: string;
  /** Адрес WebSocket. */
  readonly VITE_WS_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
