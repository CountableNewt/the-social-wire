/** Decode only structured errors: an edge proxy may return an HTML error page. */
export async function privateDraftRequestFailure(response: Response, operation: string): Promise<{ message: string; code?: string }> {
  const body: unknown = await response.json().catch(() => undefined);
  const fields = body && typeof body === "object" ? body as Record<string, unknown> : undefined;
  const detail = (value: unknown) => typeof value === "string" && value.trim() ? value.trim().replace(/\s+/g, " ").slice(0, 500) : undefined;
  const code = detail(fields?.error);
  const reason = detail(fields?.message) ?? detail(fields?.reason) ?? code ?? (response.statusText || "Request failed");
  const recovery = response.status === 401 || response.status === 403
    ? "Sign in again to restore draft access."
    : response.status === 413
      ? "Reduce the draft or attachment size before retrying."
      : response.status === 429
        ? "Wait a moment, then retry."
        : code === "UploadPending"
          ? "Wait for the attachment upload to finish, then retry."
          : code === "AssetRetired"
            ? "Retry to copy your local file."
            : response.status === 409
              ? "The remote draft changed. Retry to reconcile."
              : response.status === 400
                ? "Check the draft details and attachments before retrying."
                : "Retry when the connection or draft service is available.";
  const diagnostics = `HTTP ${response.status}${code && code !== reason ? ` · ${code}` : ""}`;
  return { message: `${operation} failed: ${reason} (${diagnostics}). ${recovery} Your local copy has not been removed.`, code };
}
