import z from 'zod'

/** Parse the textarea into distinct, trimmed keys. Duplicates are collapsed. */
export function parseExaKeys(value: string): string[] {
  const seen = new Set<string>()
  for (const line of value.split('\n')) {
    const key = line.trim()
    if (key) seen.add(key)
  }
  return [...seen]
}

export const ExaConfigFormSchema = z
  .object({
    enabled: z.boolean(),
    keys: z
      .string()
      .max(12000, 'Daftar kunci terlalu panjang')
      .refine((value) => parseExaKeys(value).length <= 20, 'Maksimal 20 kunci Exa')
      .refine(
        (value) => parseExaKeys(value).every((key) => key.length <= 512 && !/\s/.test(key)),
        'Setiap kunci harus satu baris tanpa spasi',
      ),
    clear_keys: z.boolean(),
  })
  .superRefine((body, ctx) => {
    if (body.clear_keys && parseExaKeys(body.keys).length > 0) {
      ctx.addIssue({
        code: 'custom',
        path: ['keys'],
        message: 'Pilih penggantian atau penghapusan kunci, bukan keduanya',
      })
    }
  })

/** Wire body: `keys` is omitted when the user is not replacing them. */
export type ExaConfigFormDto = {
  enabled: boolean
  keys?: string[]
  clear_keys: boolean
}
