# web — SPA conventions

The React half of the repo: Vite + TanStack Router/Query/Form/Table, Tailwind v4,
and the vendored design system under `src/components/ui`.

## Design system only — never raw primitives

Never write native form, table, or action primitives in feature code. Always
compose the component from `src/components/ui`:

| Never use                                               | Use instead                                              |
| ------------------------------------------------------- | -------------------------------------------------------- |
| `<select>`, `<option>`, `<optgroup>`                    | `Select`, `SelectTrigger`, `SelectContent`, `SelectItem` |
| `<input>` (text, checkbox, radio, switch)               | `Input`, `Checkbox`, `RadioGroup`, `Switch`              |
| `<textarea>`                                            | `Textarea`                                               |
| `<button>`                                              | `Button`                                                 |
| `<table>`, `<thead>`, `<tbody>`, `<tr>`, `<td>`, `<th>` | `DataGrid`, `DataGridContainer`, `DataGridTable`         |
| `<dialog>` or hand-rolled overlays                      | `AlertDialog`, `Dialog`, `Sheet`, `Drawer`               |
| hand-rolled dropdowns/menus                             | `DropdownMenu`, `Popover`, `Command`                     |

`src/components/ui` is the only place raw primitives are allowed, because that
is where they are wrapped, styled, and given keyboard/focus behaviour. Do not
fork a primitive into feature code, and do not restyle a native element to look
like a design-system one.

Semantic elements are still expected and are not "primitives" in this sense:
`<form>`, `<label>`, `<fieldset>`, headings, `<ul>`/`<li>`, `<p>`, and inline
`<span>` stay as they are.

When a needed control has no wrapper yet, add the wrapper under
`src/components/ui` first (upstream shadcn/ui shape, minimal local edits), then
use it from the feature block.

## Verify before finishing

```sh
grep -rnE '<(select|option|optgroup|textarea|input|button|table|thead|tbody|tr|td|th)([[:space:]]|>)' \
  src --include='*.tsx' | grep -v '^src/components/ui/'
```

The command must print nothing. Then run `tsc --noEmit`, `eslint .`,
`prettier --check`, and `vite build` (or `make check` from the repo root).

## Other layers

- Feature code reaches the API through `src/lib/api/queries`, never through
  `services`, `client-fetch`, or axios directly.
- UI copy is Indonesian; keep existing design tokens instead of adding new
  colour, radius, or shadow values.
- Every element you add must be wired up: no placeholder controls, no state
  that nothing reads or updates.
