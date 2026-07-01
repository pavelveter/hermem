import { defineConfig } from "vitest/config";

// vitest config used by both `npm test` (locally) and
// `.github/workflows/sdk.yml::typescript-sdk` (CI matrix job).
// `coverage` is opt-in: `npm test` skips it; `npx vitest run
// --coverage` enables it. Reports go to ./coverage/ in:
//
//   - text    : terminal summary (always on when coverage is on)
//   - json    : machine-parseable summary (consumed by downstream tools)
//   - html    : developer-friendly browsable report
//   - lcov    : standard for codecov.io / SonarQube / most CI vendors
//
// `include` mirrors the test discovery glob — coverage ignores
// anything else (node_modules, dist, services, etc).
//
// Reporter ordering is intentional: text runs first so a developer
// watching the CI log sees the headline %, then lcov / json / html
// for the artifacts.
export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      reporter: ["text", "lcov", "json", "html"],
      reportsDirectory: "./coverage",
    },
  },
});
