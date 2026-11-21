/** GET /health — liveness plus a real database ping, wrapped in the hsg envelope. */
export type Health = {
  status: 'ok' | 'unavailable'
}
