import { m } from "@/paraglide/messages";
import type { components } from "./schema";

type ApiError = components["schemas"]["ErrorModel"];

/**
 * Unwraps an openapi-fetch result: throws (using the RFC 7807 `detail`,
 * falling back to a generic message) on an error response, throws the same
 * generic message if the call "succeeded" with no body, otherwise returns
 * the data. Not for endpoints that legitimately respond 204/empty on
 * success (e.g. DELETE /api/cubes/{cubeId}) — there `data` is always falsy
 * and this would misfire on every success.
 */
export function unwrap<T>(data: T | undefined, error: ApiError | undefined): T {
  if (error) throw new Error(error.detail ?? m.error_generic());
  if (!data) throw new Error(m.error_generic());
  return data;
}
