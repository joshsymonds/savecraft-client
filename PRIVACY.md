# Privacy Policy

**Last updated:** March 4, 2026

---

**TL;DR:** Savecraft collects the minimum data needed to connect your game saves to AI assistants. We store your email address, your game save data (which you push to us), and notes you create. We do not run analytics, do not track you, do not sell your data, and do not see your conversations with AI assistants. Our code is [open source](https://github.com/joshsymonds/savecraft) — you can verify all of this yourself.

---

## Who we are

Savecraft is operated by Josh Symonds ("we," "us," "our").

**General contact:** [josh@savecraft.gg](mailto:josh@savecraft.gg)
**Privacy contact:** [privacy@savecraft.gg](mailto:privacy@savecraft.gg)

Savecraft is a gaming companion tool that parses your game save files and serves structured game state data to AI assistants (Claude, ChatGPT, Gemini) via the Model Context Protocol (MCP). It consists of a local daemon that runs on your gaming device, a cloud service that stores and serves your data, and a web interface for managing your devices and settings.

This policy applies to the hosted service at **savecraft.gg** and the Savecraft daemon software. If you self-host Savecraft from our open-source repository, your deployment is governed by your own privacy practices, not this policy.

## What we collect and why

We collect only what's necessary to provide the service. Here is everything, with nothing omitted:

### Account data

When you create an account, we collect your **email address** and **display name** through our authentication provider, Clerk. We use this to identify your account and display which account your devices are linked to.

**Legal basis (GDPR):** Contract performance — you provide this to create and use your account.
**Retention:** Until you delete your account.

### Device data

When you link a gaming device, the daemon registers with our server and reports its **hostname** (e.g., "steam-deck"), **operating system** (e.g., "linux"), and **architecture** (e.g., "arm64"). A device-specific authentication token is generated; we store only a SHA-256 hash of this token, never the token itself.

**Legal basis:** Contract performance — device identification is required for the daemon to push save data.
**Retention:** Until you unlink the device, plus a 7-day cleanup period.

### API keys

You can generate API keys for programmatic access. We store a **SHA-256 hash** of each key, a short prefix for identification (e.g., "sk_...abc"), and a label you provide. The full key is shown to you once at creation and is never stored.

**Legal basis:** Contract performance.
**Retention:** Until you delete the key or your account.

### Game save data

This is the core of the service. When the daemon detects a save file change, it parses the file locally on your device, converts it to structured JSON (character stats, gear, skills, quest progress — whatever the game plugin extracts), and pushes that JSON to our cloud. We store every snapshot as an immutable record so you can track changes over time.

The daemon reads your save files in **read-only** mode. It cannot modify your saves. The raw save file never leaves your device — only the parsed JSON output is transmitted.

**Legal basis:** Contract performance — serving your game state to AI assistants is the entire service.
**Retention:** All snapshots are currently retained for the life of your account. We may introduce time-based thinning in the future (e.g., keeping daily snapshots for a month, then weekly) and will update this policy before doing so.

### Notes

You (or an AI assistant acting on your behalf during conversation) can create notes attached to your saves — build guides, farming goals, session reminders. Notes are user-authored markdown stored alongside your save data.

**Legal basis:** Contract performance.
**Retention:** Until you delete them.

### Authentication and session data

When you connect an AI assistant via MCP, the OAuth handshake creates client registrations, authorization codes, and access tokens. These are stored in Cloudflare KV with automatic expiration (TTL-managed). A single-column record tracks whether you've connected an MCP client, used to show connection status in the web UI.

**Legal basis:** Contract performance (MCP authentication is required for the service to function).
**Retention:** Tokens expire automatically per their TTL. The MCP activity flag persists until your account is deleted.

### Device status events

The daemon reports operational status (online/offline, parse success/failure, push status) to power the real-time activity feed in the web UI. We retain the last 100 events per device.

**Legal basis:** Legitimate interest — operational monitoring helps you verify your daemon is working and helps us debug issues.
**Retention:** Rolling window of 100 events per device, pruned on insert.

## What we do NOT collect

This matters as much as what we do collect:

- **No analytics or telemetry.** No Google Analytics, no Posthog, no tracking pixels, no third-party scripts.
- **No IP addresses.** Cloudflare sees IP addresses at the network edge, but our application code never reads, stores, or logs them.
- **No conversation history.** We never see what you say to Claude, ChatGPT, or Gemini. The AI assistant requests specific data from us (e.g., "get this character's equipped gear"), and we return structured JSON. The conversation itself stays entirely between you and the AI provider.
- **No device fingerprinting.** We do not collect User-Agent strings, screen dimensions, installed fonts, or any browser fingerprint data.
- **No behavioral tracking.** No click tracking, session recording, heatmaps, or funnel analysis.

## How data flows through MCP

This is worth explaining clearly because it's a new kind of data flow that most privacy policies don't address.

When you connect an AI assistant to Savecraft, the assistant can use our MCP tools to request your game data. A typical interaction looks like this: you ask the AI a question about your character, the AI calls our `get_section` tool with your save ID, we return the requested JSON data (e.g., your equipped gear), and the AI uses that data to answer your question.

We serve data to the AI assistant **on your behalf and under your authorization.** We do not control what the AI provider does with the data after receiving it — that is governed by your agreement with the AI provider (Anthropic, OpenAI, Google, etc.). We do not cache requests from AI providers, and we do not retain logs of which tools are called or what data is returned.

## Cookies

Savecraft uses a single cookie:

| Cookie | Provider | Purpose | Duration |
|--------|----------|---------|----------|
| `__client_uat` | Clerk | Authentication session management | Session |

This cookie is **strictly necessary** for the service to function (it keeps you logged in) and is exempt from consent requirements under the ePrivacy Directive. We do not use any analytics, advertising, or tracking cookies. No cookie consent banner is needed or shown because there are no optional cookies to consent to.

## Who has access to your data

We name every third party, what they receive, and why.

### Cloudflare

**Role:** Infrastructure provider (data processor under GDPR).
**What they process:** All application data — save snapshots, account metadata, notes, authentication tokens, device events. Cloudflare Workers execute your API requests; R2 stores save snapshots; D1 (SQLite) stores account and device metadata, notes, and the search index; KV stores OAuth tokens.
**Data location:** Your data is stored and processed on Cloudflare's global network, including in the United States.
**Transfer safeguards:** Cloudflare is certified under the EU-U.S. Data Privacy Framework and incorporates EU Standard Contractual Clauses in its Data Processing Addendum, which applies automatically to all customers.
**Their privacy policy:** [cloudflare.com/privacypolicy](https://www.cloudflare.com/privacypolicy/)

### Clerk

**Role:** Authentication provider (data processor for authentication services; independent data controller for its own account management).
**What they receive:** Your email address, display name, and authentication credentials (hashed). Clerk also processes session data and device metadata as part of authentication.
**Data location:** United States (Google Cloud Platform).
**Transfer safeguards:** Clerk is certified under the EU-U.S. Data Privacy Framework and offers a DPA with Standard Contractual Clauses at [clerk.com/legal/dpa](https://clerk.com/legal/dpa).
**Their privacy policy:** [clerk.com/legal/privacy](https://clerk.com/legal/privacy)

### Stripe (future)

When we add paid subscriptions, Stripe will process payments. Stripe will receive your payment card details, billing address, and transaction data directly — we will not store payment information ourselves. Stripe acts as both a data processor (handling transactions on our behalf) and an independent data controller (for fraud prevention and regulatory compliance). We will update this policy before adding Stripe.

**No other third parties have access to your data.** We do not use advertising networks, data brokers, marketing platforms, or social media integrations.

## International data transfers

If you are in the EU/EEA or UK, your data is transferred to and processed in the United States and potentially other countries where Cloudflare operates edge infrastructure. These transfers are protected by:

- The **EU-U.S. Data Privacy Framework** adequacy decision (European Commission, July 10, 2023), under which both Cloudflare and Clerk are certified.
- **EU Standard Contractual Clauses** (Commission Decision 2021/914) incorporated into both Cloudflare's and Clerk's data processing agreements, as a fallback mechanism.
- The **UK International Data Transfer Addendum** for UK-originating data.

## Your rights

### Everyone

You can request a copy of all data we hold about you, ask us to correct inaccurate data, or delete your account and all associated data by emailing [privacy@savecraft.gg](mailto:privacy@savecraft.gg). You can also delete individual saves, notes, and devices directly through the web UI or MCP tools at any time.

### EU/EEA and UK residents

Under GDPR, you have the right to:

- **Access** your personal data and receive a copy in a portable format
- **Rectify** inaccurate or incomplete data
- **Erase** your data ("right to be forgotten")
- **Restrict** processing in certain circumstances
- **Object** to processing based on legitimate interest
- **Data portability** — receive your data in a structured, machine-readable format (your game state is already structured JSON)
- **Lodge a complaint** with your local data protection supervisory authority

We respond to all data rights requests within **one month**. If a request is complex, we may extend this by up to two additional months with notice. Requests are free of charge.

We do not have a Data Protection Officer, as our processing activities do not involve large-scale systematic monitoring or special category data. For any privacy concerns, contact us directly at [privacy@savecraft.gg](mailto:privacy@savecraft.gg).

### California residents

We do not currently meet the applicability thresholds of the California Consumer Privacy Act (CCPA/CPRA). Regardless, we voluntarily state: **we do not sell or share your personal information** as defined under California law, and we have never done so. If the CCPA becomes applicable to us, we will update this policy with the required disclosures.

## Children's privacy

Savecraft is not directed at children under 13. We do not knowingly collect personal information from children under 13. If you are under 13, please do not use Savecraft or provide any information to us. If we learn that we have collected personal information from a child under 13, we will delete that data promptly. If you believe a child under 13 has provided us with personal information, please contact us at [privacy@savecraft.gg](mailto:privacy@savecraft.gg).

In EU member states where the age of digital consent is higher than 13, users below that age require parental or guardian consent to use the service.

## Data security

Save data and notes are stored in Cloudflare's infrastructure, which provides encryption at rest and in transit. Authentication tokens are hashed (SHA-256 for device tokens; bcrypt for Clerk credentials). OAuth tokens are opaque and stored with automatic expiration. The daemon runs with minimal system permissions — on Linux/Steam Deck, kernel-enforced sandboxing (via systemd) restricts it to read-only access to save file directories and write access only to its own configuration. WASM plugins that parse save files are sandboxed and cannot access the filesystem, network, or environment variables.

Our source code is publicly available. You can inspect exactly what data the daemon collects, how plugins parse saves, and how the server handles requests.

## Changes to this policy

We will update this policy when our data practices change. For material changes — new data collection, new third-party services, changes to retention periods — we will notify you via email and/or a prominent notice on savecraft.gg at least 30 days before the changes take effect. For minor clarifications or formatting changes, we will update the "Last updated" date at the top.

Previous versions of this policy will be available in our public Git repository.

## Contact

For any privacy-related questions, concerns, or data rights requests:

**Email:** [privacy@savecraft.gg](mailto:privacy@savecraft.gg)

We aim to respond to all inquiries within 5 business days and to all formal data rights requests within one month.
