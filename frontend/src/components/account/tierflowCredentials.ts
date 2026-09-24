/**
 * Console credentials are optional and independent of the inference API key.
 * Empty edit inputs mean "keep existing" because API responses redact secrets.
 */
export function applyTierflowConsoleCredentials(
  credentials: Record<string, unknown>,
  cookie: string,
  userId: string
): void {
  const trimmedCookie = cookie.trim()
  const trimmedUserId = userId.trim()
  if (trimmedCookie) credentials.tierflow_cookie = trimmedCookie
  if (trimmedUserId) credentials.tierflow_user_id = trimmedUserId
}
