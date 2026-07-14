/** Human "time left" label for a pending_payment deadline. */
export function remainingLabel(expiresAt: string, now: number = Date.now()): string {
  const ms = new Date(expiresAt).getTime() - now;
  if (ms <= 0) return "0m";
  const totalMinutes = Math.ceil(ms / 60_000);
  const h = Math.floor(totalMinutes / 60);
  const min = totalMinutes % 60;
  return h > 0 ? `${h}h ${min}m` : `${min}m`;
}
