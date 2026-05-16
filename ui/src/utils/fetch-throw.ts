export class NetworkError extends Error {
  status?: number;
  headers?: Headers;
  data?: Response;

  constructor(message: string) {
    super(message);
    Error.captureStackTrace?.(this, this.constructor);
    this.name = this.constructor.name;
    this.message = message;
  }
}

type FetchInput = Parameters<typeof fetch>[0];
type FetchInit = Parameters<typeof fetch>[1];

const shouldAttemptRefresh = (url: string) => {
  if (!url.includes("/api/")) return false;
  if (url.includes("/api/auth/login")) return false;
  if (url.includes("/api/auth/logout")) return false;
  if (url.includes("/api/auth/refresh")) return false;
  return true;
};

const parseErrorMessage = async (res: Response): Promise<string | undefined> => {
  try {
    const data = (await res.clone().json()) as { message?: string };
    return data?.message;
  } catch {
    return undefined;
  }
};

const isTokenExpiredError = (message?: string) => {
  if (!message) return false;
  return message === "auth.token_expired" || message.includes("auth.token_expired");
};

const doFetch = async (
  customFetch: typeof fetch,
  input: FetchInput,
  init?: FetchInit,
  retried = false,
): Promise<Response> => {
  const response = await customFetch(input, init);
  if (response.ok) {
    return response;
  }

  if (response.status === 401 && !retried) {
    const url = typeof input === "string" ? input : input.toString();
    const errorMessage = await parseErrorMessage(response);
    if (isTokenExpiredError(errorMessage) && shouldAttemptRefresh(url)) {
      const refreshRes = await customFetch("/api/auth/refresh", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
      });
      if (refreshRes.ok) {
        const retryRes = await doFetch(customFetch, input, init, true);
        return retryRes;
      }
    }
  }

  const error = new NetworkError(response.statusText);
  error.status = response.status;
  error.headers = response.headers;
  error.data = response;
  throw error;
};

function fetchThrow(input: FetchInput, init?: FetchInit): Promise<Response>;
function fetchThrow(
  customFetch: typeof fetch,
): (input: FetchInput, init?: FetchInit) => Promise<Response>;
function fetchThrow(
  inputOrCustomFetch: FetchInput | typeof fetch,
  init?: FetchInit,
): Promise<Response> | ((input: FetchInput, init?: FetchInit) => Promise<Response>) {
  if (typeof inputOrCustomFetch === "function") {
    const customFetch = inputOrCustomFetch;
    return (input: FetchInput, init?: FetchInit) => doFetch(customFetch, input, init);
  }

  return doFetch(window.fetch.bind(window), inputOrCustomFetch as FetchInput, init);
}

export default fetchThrow;
