import { defineConfig } from "@playwright/test";

const externalBaseURL = process.env.PLAYWRIGHT_BASE_URL;
const useExternalServer = process.env.PLAYWRIGHT_EXTERNAL_SERVER === "1";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  fullyParallel: false,
  use: {
    baseURL: externalBaseURL || "http://127.0.0.1:8742",
    headless: true,
    channel: "chrome",
  },
  webServer: useExternalServer
    ? undefined
    : {
        command: "cd .. && go build -o /tmp/maestro-web-smoke ./cmd/maestro && /tmp/maestro-web-smoke demo-web --host 127.0.0.1 --port 8742",
        url: "http://127.0.0.1:8742/healthz",
        reuseExistingServer: true,
        timeout: 120_000,
      },
});
