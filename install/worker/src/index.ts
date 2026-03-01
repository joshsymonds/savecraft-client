interface Env {
	SERVER_URL: string;
	REDIRECT_URL: string;
	INSTALL: R2Bucket;
}

const CLI_PATTERNS = ['curl', 'wget', 'httpie', 'powershell'];

function isCli(userAgent: string): boolean {
	const lower = userAgent.toLowerCase();
	return CLI_PATTERNS.some((p) => lower.includes(p));
}

export default {
	async fetch(request: Request, env: Env): Promise<Response> {
		const ua = request.headers.get('user-agent') ?? '';

		if (!isCli(ua)) {
			return Response.redirect(env.REDIRECT_URL, 302);
		}

		const obj = await env.INSTALL.get('curl/install.sh');
		if (!obj) {
			return new Response('Install script not found\n', { status: 404 });
		}

		const script = await obj.text();
		const patched = `SAVECRAFT_SERVER_URL="${env.SERVER_URL}"\n${script}`;

		return new Response(patched, {
			headers: {
				'content-type': 'text/x-shellscript; charset=utf-8',
				'content-disposition': 'inline; filename="install.sh"',
			},
		});
	},
} satisfies ExportedHandler<Env>;
