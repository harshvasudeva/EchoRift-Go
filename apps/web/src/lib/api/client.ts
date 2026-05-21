const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "";

export class APIError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string) {
    super(code);
    this.status = status;
    this.code = code;
  }
}

type RequestOptions = {
  accessToken?: string | null;
};

export async function apiGet<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  return apiRequest<T>("GET", path, undefined, options);
}

export async function apiPost<T>(
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  return apiRequest<T>("POST", path, body, options);
}

async function apiRequest<T>(
  method: string,
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (options.accessToken) {
    headers.Authorization = `Bearer ${options.accessToken}`;
  }

  const response = await fetch(`${API_BASE_URL}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "include",
  });

  if (!response.ok) {
    let code = `http_${response.status}`;
    try {
      const payload = (await response.json()) as { error?: string };
      code = payload.error ?? code;
    } catch {
      // Response was not JSON.
    }
    throw new APIError(response.status, code);
  }

  return response.json() as Promise<T>;
}
