import { unwrapData } from "@/lib/mappers";

// ApiError is what every failed daemon call throws: message is the daemon's own
// error text, so a UI can show it as is.
export class ApiError extends Error {
  readonly status: number;
  readonly code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  // Default true: return the envelope's `data`. Set false for bodies that are
  // not the daemon envelope (OpenAI-style chat completions).
  unwrap?: boolean;
}

async function errorFromResponse(res: Response): Promise<ApiError> {
  const fallback = `Request failed (HTTP ${res.status})`;
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    return new ApiError(fallback, res.status);
  }
  const err = (body as { error?: { code?: unknown; message?: unknown; details?: unknown } } | null)?.error;
  const message = typeof err?.message === "string" && err.message ? err.message : fallback;
  const details = Array.isArray(err?.details) ? err.details.filter((d) => typeof d === "string") : [];
  const code = typeof err?.code === "string" ? err.code : undefined;
  return new ApiError(details.length > 0 ? `${message}: ${details.join("; ")}` : message, res.status, code);
}

export async function requestJSON<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const { method, body, unwrap = true } = options;
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";

  const init: RequestInit = {
    ...(method ? { method } : {}),
    ...(Object.keys(headers).length > 0 ? { headers } : {}),
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  };
  // Plain GETs stay a bare fetch(url).
  const res = Object.keys(init).length > 0 ? await fetch(url, init) : await fetch(url);
  if (!res.ok) throw await errorFromResponse(res);

  const parsed: unknown = await res.json();
  return unwrap ? unwrapData<T>(parsed) : (parsed as T);
}
