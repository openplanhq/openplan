/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_OPENPLAN_TENANT_ID?: string;
  readonly VITE_DEBUG?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
