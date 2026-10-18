This is a [Next.js](https://nextjs.org) project bootstrapped with [`create-next-app`](https://nextjs.org/docs/app/api-reference/cli/create-next-app).

## Real daemon mode

By default the UI uses in-memory mocks. To talk to a running `agentd`:

```bash
# Terminal 1 — daemon (LLM configured in agentd.yaml)
./bin/agentd start -v

# Terminal 2 — cockpit
cd web
NEXT_PUBLIC_USE_MOCK=false npm run dev
```

| Variable | Purpose |
| --- | --- |
| `NEXT_PUBLIC_USE_MOCK` | Set to `false` to call the daemon instead of mocks (default: mock on). |
| `NEXT_PUBLIC_API_URL` | Daemon base URL (default: `http://localhost:8765`). |

The UI never sends `api.materialize_token`: a `NEXT_PUBLIC_` value is inlined into the browser bundle, so anyone who loads the page could read it. The daemon trusts writes whose `Origin` is `api.ui_origin` (default `http://localhost:3000`) and skips the token for them. If you serve the UI from another address, set `api.ui_origin` in the daemon config to that exact `scheme://host:port`. See [docs/config-reference.md](../docs/config-reference.md).

Chat sends `tools: [{ name: "create_plan" }]` on `/v1/chat/completions` so the daemon can return `tool_calls` (`create_plan`, `status_report`). The UI maps `project_name` → plan card fields, renders status/clarification panels instead of raw JSON, and approves via materialize (refreshing the board and opening the first new task).

## Getting Started

First, run the development server:

```bash
npm run dev
# or
yarn dev
# or
pnpm dev
# or
bun dev
```

Open [http://localhost:3000](http://localhost:3000) with your browser to see the result.

You can start editing the page by modifying `app/page.tsx`. The page auto-updates as you edit the file.

This project uses [`next/font`](https://nextjs.org/docs/app/building-your-application/optimizing/fonts) to automatically optimize and load [Geist](https://vercel.com/font), a new font family for Vercel.

## Learn More

To learn more about Next.js, take a look at the following resources:

- [Next.js Documentation](https://nextjs.org/docs) - learn about Next.js features and API.
- [Learn Next.js](https://nextjs.org/learn) - an interactive Next.js tutorial.

You can check out [the Next.js GitHub repository](https://github.com/vercel/next.js) - your feedback and contributions are welcome!

## Deploy on Vercel

The easiest way to deploy your Next.js app is to use the [Vercel Platform](https://vercel.com/new?utm_medium=default-template&filter=next.js&utm_source=create-next-app&utm_campaign=create-next-app-readme) from the creators of Next.js.

Check out our [Next.js deployment documentation](https://nextjs.org/docs/app/building-your-application/deploying) for more details.
