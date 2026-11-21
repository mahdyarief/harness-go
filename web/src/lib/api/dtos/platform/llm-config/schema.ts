import z from 'zod'

export const LlmConfigFormSchema = z
  .object({
    enabled: z.boolean(),
    endpoint: z
      .string()
      .trim()
      .max(2048, 'Endpoint maksimal 2048 karakter')
      .refine((value) => {
        if (!value) return true
        try {
          const url = new URL(value)
          return (
            url.protocol === 'https:' &&
            !url.username &&
            !url.password &&
            !value.includes('?') &&
            !value.includes('#')
          )
        } catch {
          return false
        }
      }, 'Gunakan endpoint HTTPS lengkap tanpa kredensial, query, atau fragmen'),
    model: z.string().trim().max(200, 'Model maksimal 200 karakter'),
    language: z
      .string()
      .trim()
      .max(64, 'Bahasa maksimal 64 karakter')
      .optional()
      .transform((value) => (value ? value : '')),
    timeout_seconds: z
      .number()
      .int()
      .min(1, 'Waktu tunggu minimal 1 detik')
      .max(300, 'Waktu tunggu maksimal 300 detik'),
    max_tokens: z
      .number()
      .int()
      .min(1, 'Batas token minimal 1')
      .max(16384, 'Batas token maksimal 16384'),
    credential: z
      .string()
      .max(4096, 'Kredensial maksimal 4096 karakter')
      .refine((value) => !/[\r\n]/.test(value), 'Kredensial tidak boleh berisi baris baru')
      .optional()
      .transform((value) => (value?.trim() ? value : undefined)),
    clear_credential: z.boolean(),
  })
  .superRefine((body, ctx) => {
    if (body.clear_credential && body.credential !== undefined) {
      ctx.addIssue({
        code: 'custom',
        path: ['credential'],
        message: 'Pilih penggantian atau penghapusan kredensial, bukan keduanya',
      })
    }
    if (body.enabled && body.clear_credential) {
      ctx.addIssue({
        code: 'custom',
        path: ['enabled'],
        message: 'Nonaktifkan integrasi sebelum menghapus kredensial',
      })
    }
    if (body.enabled || body.endpoint || body.model || body.credential !== undefined) {
      if (!body.endpoint)
        ctx.addIssue({ code: 'custom', path: ['endpoint'], message: 'Endpoint wajib diisi' })
      if (!body.model)
        ctx.addIssue({ code: 'custom', path: ['model'], message: 'Model wajib diisi' })
    }
  })

export type LlmConfigFormDto = z.output<typeof LlmConfigFormSchema>
