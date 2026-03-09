import { env, SELF } from "cloudflare:test";
import { describe, it, expect, beforeEach } from "vitest";

const FAKE_SCRIPT = '#!/usr/bin/env bash\nset -euo pipefail\necho "hello"';
const FAKE_BINARY = new Uint8Array([0x7f, 0x45, 0x4c, 0x46]); // ELF header stub
const FAKE_SIG = new Uint8Array([0xde, 0xad, 0xbe, 0xef]);
const FAKE_PUBKEY = "MCowBQYDK2VwAyEATestKeyHere=";
const FAKE_MANIFEST = JSON.stringify({
	version: "0.1.0",
	ed25519PublicKey: FAKE_PUBKEY,
	platforms: {
		"linux-amd64": {
			url: "https://install.savecraft.gg/daemon/savecraft-daemon-linux-amd64",
			signatureUrl: "https://install.savecraft.gg/daemon/savecraft-daemon-linux-amd64.sig",
			sha256: "abc123",
		},
	},
});
const FAKE_INSTALLER_METADATA = JSON.stringify({ version: "1.2.3" });

async function cleanR2(): Promise<void> {
	const listed = await env.INSTALL.list();
	for (const obj of listed.objects) {
		await env.INSTALL.delete(obj.key);
	}
}

describe("install worker", () => {
	beforeEach(async () => {
		await cleanR2();
		await env.INSTALL.put("curl/install.sh", FAKE_SCRIPT);
		await env.INSTALL.put("daemon/manifest.json", FAKE_MANIFEST);
		await env.INSTALL.put("curl/metadata.json", FAKE_INSTALLER_METADATA);
	});

	describe("install script (CLI user-agents)", () => {
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
				expect(body).toContain("#!/usr/bin/env bash");
				expect(resp.headers.get("content-type")).toBe("text/x-shellscript; charset=utf-8");
				expect(resp.headers.get("content-disposition")).toBe(
					'inline; filename="install.sh"',
				);
			});
		}
	});

	describe("install script (browser redirect)", () => {
		const nonWindowsBrowserAgents = [
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		];

		for (const ua of nonWindowsBrowserAgents) {
			it(`redirects non-Windows browser to site (${ua.slice(0, 30)}...)`, async () => {
				const resp = await SELF.fetch("https://install.savecraft.gg/", {
					headers: { "user-agent": ua },
					redirect: "manual",
				});
				expect(resp.status).toBe(302);
				expect(resp.headers.get("location")).toContain(env.REDIRECT_URL);
			});
		}
	});

	describe("Windows browser → .cmd installer", () => {
		const windowsBrowserAgents = [
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
		];

		for (const ua of windowsBrowserAgents) {
			it(`serves .cmd installer to Windows browser (${ua.slice(0, 40)}...)`, async () => {
				const resp = await SELF.fetch("https://install.savecraft.gg/", {
					headers: { "user-agent": ua },
				});
				expect(resp.status).toBe(200);
				expect(resp.headers.get("content-disposition")).toContain("savecraft-install.cmd");
				const body = await resp.text();
				expect(body).toContain("@echo off");
				expect(body).toContain(env.APP_NAME);
				expect(body).toContain("Unblock-File");
			});
		}

		it("stops existing processes before downloading", async () => {
			const resp = await SELF.fetch("https://install.savecraft.gg/", {
				headers: {
					"user-agent":
						"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
				},
			});
			const body = await resp.text();
			expect(body).toContain("savecraft-daemon.exe\\\" stop");
			expect(body).toContain("Stop-Process -Name savecraft-tray");
		});

		it("does not serve .cmd to Windows Phone", async () => {
			const resp = await SELF.fetch("https://install.savecraft.gg/", {
				headers: {
					"user-agent":
						"Mozilla/5.0 (Windows Phone 8.1; ARM; Trident/7.0; Touch; rv:11.0; IEMobile/11.0) like Gecko",
				},
				redirect: "manual",
			});
			expect(resp.status).toBe(302);
			const location = resp.headers.get("location")!;
			expect(location).toContain(env.REDIRECT_URL);
		});

		it("still serves install script to CLI tools on Windows (WSL)", async () => {
			const resp = await SELF.fetch("https://install.savecraft.gg/", {
				headers: { "user-agent": "curl/8.7.1" },
			});
			expect(resp.status).toBe(200);
			expect(resp.headers.get("content-type")).toBe("text/x-shellscript; charset=utf-8");
		});
	});

	it("redirects when no user-agent is set", async () => {
		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: {},
			redirect: "manual",
		});
		expect(resp.status).toBe(302);
	});

	it("prepends all env vars into the script", async () => {
		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: { "user-agent": "curl/8.0" },
		});
		const body = await resp.text();
		const lines = body.split("\n");
		expect(lines[0]).toBe(`SAVECRAFT_INSTALL_URL="${env.INSTALL_URL}"`);
		expect(lines[1]).toBe(`SAVECRAFT_SERVER_URL="${env.SERVER_URL}"`);
		expect(lines[2]).toBe(`SAVECRAFT_FRONTEND_URL="${env.REDIRECT_URL}"`);
		expect(lines[3]).toBe(`SAVECRAFT_INSTALLER_VERSION="1.2.3"`);
		expect(lines[4]).toBe(`SAVECRAFT_ED25519_PUBKEY="${FAKE_PUBKEY}"`);
		expect(lines[5]).toBe(`SAVECRAFT_APP_NAME="${env.APP_NAME}"`);
		expect(lines[6]).toBe(`SAVECRAFT_STATUS_PORT="${env.STATUS_PORT}"`);
		expect(body).toContain("#!/usr/bin/env bash");
	});

	it("injects SAVECRAFT_FRONTEND_URL from REDIRECT_URL", async () => {
		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: { "user-agent": "curl/8.0" },
		});
		const body = await resp.text();
		expect(body).toContain(`SAVECRAFT_FRONTEND_URL="${env.REDIRECT_URL}"`);
	});

	it("falls back to defaults when metadata files are missing", async () => {
		await env.INSTALL.delete("daemon/manifest.json");
		await env.INSTALL.delete("curl/metadata.json");

		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: { "user-agent": "curl/8.0" },
		});
		const body = await resp.text();
		expect(body).toContain('SAVECRAFT_INSTALLER_VERSION="dev"');
		expect(body).toContain('SAVECRAFT_ED25519_PUBKEY=""');
		expect(body).toContain(`SAVECRAFT_APP_NAME="${env.APP_NAME}"`);
		expect(body).toContain("#!/usr/bin/env bash");
	});

	it("returns 404 when script is missing from R2", async () => {
		await env.INSTALL.delete("curl/install.sh");
		const resp = await SELF.fetch("https://install.savecraft.gg/", {
			headers: { "user-agent": "curl/8.0" },
		});
		expect(resp.status).toBe(404);
	});

	describe("daemon downloads (/daemon/...)", () => {
		beforeEach(async () => {
			await env.INSTALL.put("daemon/savecraft-daemon-linux-amd64", FAKE_BINARY);
			await env.INSTALL.put("daemon/savecraft-daemon-linux-amd64.sig", FAKE_SIG);
		});

		it("serves a binary from R2", async () => {
			const resp = await SELF.fetch(
				"https://install.savecraft.gg/daemon/savecraft-daemon-linux-amd64",
				{ headers: { "user-agent": "curl/8.0" } },
			);
			expect(resp.status).toBe(200);
			expect(resp.headers.get("content-type")).toBe("application/octet-stream");

			const buf = new Uint8Array(await resp.arrayBuffer());
			expect(buf).toEqual(FAKE_BINARY);
		});

		it("serves binary regardless of user-agent", async () => {
			const resp = await SELF.fetch(
				"https://install.savecraft.gg/daemon/savecraft-daemon-linux-amd64",
				{ headers: { "user-agent": "Mozilla/5.0" }, redirect: "manual" },
			);
			expect(resp.status).toBe(200);
			expect(resp.headers.get("content-type")).toBe("application/octet-stream");
		});

		it("serves .sig files", async () => {
			const resp = await SELF.fetch(
				"https://install.savecraft.gg/daemon/savecraft-daemon-linux-amd64.sig",
				{ headers: { "user-agent": "curl/8.0" } },
			);
			expect(resp.status).toBe(200);
			expect(resp.headers.get("content-type")).toBe("application/octet-stream");

			const buf = new Uint8Array(await resp.arrayBuffer());
			expect(buf).toEqual(FAKE_SIG);
		});

		it("serves manifest.json with application/json content-type", async () => {
			const resp = await SELF.fetch(
				"https://install.savecraft.gg/daemon/manifest.json",
				{ headers: { "user-agent": "curl/8.0" } },
			);
			expect(resp.status).toBe(200);
			expect(resp.headers.get("content-type")).toBe("application/json");

			const data = await resp.json();
			expect(data).toHaveProperty("version", "0.1.0");
			expect(data).toHaveProperty("ed25519PublicKey", FAKE_PUBKEY);
			expect(data).toHaveProperty("platforms.linux-amd64.sha256", "abc123");
		});

		it("returns 404 for missing daemon file", async () => {
			const resp = await SELF.fetch(
				"https://install.savecraft.gg/daemon/savecraft-daemon-linux-arm64",
				{ headers: { "user-agent": "curl/8.0" } },
			);
			expect(resp.status).toBe(404);
		});
	});
});
