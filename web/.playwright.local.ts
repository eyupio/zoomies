import base from './playwright.config';
import { defineConfig } from '@playwright/test';

const exe = '/opt/pw-browsers/chromium';
export default defineConfig({
  ...base,
  projects: (base.projects ?? []).map((p) => ({
    ...p,
    use: { ...p.use, launchOptions: { ...(p.use as any)?.launchOptions, executablePath: exe } },
  })),
});
