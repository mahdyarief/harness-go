import z from 'zod'

// Field-level zod helpers shared by the request DTO schemas. `attribute` is the
// field name shown in the message.

const required = (attribute: string) => `Kolom ${attribute} wajib diisi.`
const mustBeString = (attribute: string) => `${attribute} harus berupa teks.`
const mustBeNumber = (attribute: string) => `${attribute} harus berupa angka.`
const mustBeInteger = (attribute: string) => `${attribute} harus berupa bilangan bulat.`

export const requiredString = (attribute: string) =>
  z
    .string({
      error: (issue) => (issue.input === undefined ? required(attribute) : mustBeString(attribute)),
    })
    .trim()
    .nonempty(required(attribute))

export const requiredNonNegativeInteger = (attribute: string) =>
  z
    .number({
      error: (issue) => (issue.input === undefined ? required(attribute) : mustBeNumber(attribute)),
    })
    .int(mustBeInteger(attribute))
    .min(0, `${attribute} minimal 0.`)

export const optionalNumber = (attribute: string) =>
  z
    .number({ error: mustBeNumber(attribute) })
    .int(mustBeInteger(attribute))
    .optional()

export const optionalString = (attribute: string) =>
  z.string({ error: mustBeString(attribute) }).optional()

/**
 * Required enum validation; the accepted values are the source of truth for
 * the union type it validates.
 */
export const requiredEnum = <const T extends readonly [string, ...string[]]>(
  values: T,
  attribute: string,
) => z.enum(values, { error: `${attribute} yang dipilih tidak valid.` })
