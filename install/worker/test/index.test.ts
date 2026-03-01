import { env, SELF } from "cloudflare:test";
import { describe, it, expect, beforeEach } from "vitest";

const FAKE_SCRIPT = '#!/usr/bin/env bash\nset -euo pipefail\necho "hello"';

describe("install worker", () => {
	beforeEach(async () => {
		await env.INSTALL.put("curl/install.sh", FAKE_SCRIPT);
	});

	describe("CLI user-agents", () => {
		const cliAgents = [
			["curl/8.7.1", "curl"],
			["Wget/1.21.4", "wget"],
			["HTTPie/3.2.2", "httpie"],
			["PowerShell/7.4.0", "powershell"],
		] as const;

		for (const [ua, name] of cliAgents) {
			it(`serves install script to ${name}`, async () => {
				const resp = await SELF.fetch("https://install.savecraft.gg/", {
					headers: { "user-agent": ua },
				});
				expect(resp.status).toBe(200);

				const body = await resp.text();
				expect(body.split('\n')[0]).toBe(`SAVECRAFT_SERVER_URL="${env.SERVER_URL}"`);
				expect(body).toContain("#!/usr/bin/env bash");
				expect(resp.headers.get("content-type")).toBe("text/x-shellscript; charset=utf-8");
				expect(resp.headers.get("content-disposition")).toBe(
					'inline; filename="install.sh"',
				);
			});
		}
	});

	describe("browser user-agents", () => {
		const browserAgents = [
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		];

		for (const ua of browserAgents) {
			it(`redirects browser (${ua.slice(0, 30)}...)`, async () => {
				const resp = await SELF.fetch("https://install.savecraft.gg/", {
					headers: { "user-agent": ua },
					redirect: "manual",
				});
				expect(resp.status).toBe(302);
				expect(resp.headers.get("location")).toContain(env.REDIRECT_URL);
			});
		}
	});

	it("redirects when no user-agent is set", async () => {
		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: {},
			redirect: "manual",
		});
		expect(resp.status).toBe(302);
	});

	it("prepends SERVER_URL to the script", async () => {
		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: { "user-agent": "curl/8.0" },
		});
		const body = await resp.text();
		const firstLine = body.split("\n")[0]!;
		expect(firstLine).toBe(`SAVECRAFT_SERVER_URL="${env.SERVER_URL}"`);
	});

	it("returns 404 when script is missing from R2", async () => {
		await env.INSTALL.delete("curl/install.sh");
		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: { "user-agent": "curl/8.0" },
		});
		expect(resp.status).toBe(404);
	});
});
