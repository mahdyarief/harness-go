import { z } from 'zod'

/**
 * Form-side top-up schema. The amount stays a string because the number input
 * emits raw text; the drawer converts it to a number before sending.
 */
export const topUpSchema = z.object({
  amount: z
    .string()
    .refine((value) => /^\d+$/.test(value) && Number(value) >= 1, 'Jumlah token minimal 1'),
  note: z.string().max(500, 'Catatan maksimal 500 karakter'),
})

export type TopUpFormDto = z.infer<typeof topUpSchema>

/** Wire payload the API expects: a positive integer and an optional note. */
export type TopUpDto = {
  amount: number
  note?: string
}
