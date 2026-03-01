import { defineWorkersConfig } from "@cloudflare/vitest-pool-workers/config";

export default defineWorkersConfig({
	test: {
		poolOptions: {
			workers: {
				singleWorker: true,
				// R2 uses SQLite internally in Miniflare; WAL files break
				// the isolated storage frame tracker. Same root cause as the
				// API worker's DO storage issue.
				isolatedStorage: false,
				wrangler: { configPath: "./wrangler.toml" },
			},
		},
	},
});
