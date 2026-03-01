import installScript from './install.sh';

interface Env {
	SERVER_URL: string;
	REDIRECT_URL: string;
}

const CLI_PATTERNS = ['curl', 'wget', 'httpie', 'powershell'];

function isCli(userAgent: string): boolean {
	const lower = userAgent.toLowerCase();
	return CLI_PATTERNS.some((p) => lower.includes(p));
}

export default {
	async fetch(request: Request, env: Env): Promise<Response> {
		const ua = request.headers.get('user-agent') ?? '';

		if (isCli(ua)) {
			const script = `SAVECRAFT_SERVER_URL="${env.SERVER_URL}"
${installScript}`;
			return new Response(script, {
				headers: {
					'content-type': 'text/x-shellscript; charset=utf-8',
					'content-disposition': 'inline; filename="install.sh"',
				},
			});
		}

		return Response.redirect(env.REDIRECT_URL, 302);
	},
} satisfies ExportedHandler<Env>;
