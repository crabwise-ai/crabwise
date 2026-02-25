# Proxy Mapping Spec (v1)

The proxy mapping spec normalizes provider request/response payloads into Crabwise canonical fields used by the policy gate and audit writer.

## Schema

- `version`: spec version string (`"1"` for M2)
- `provider`: provider key (`openai`, etc.)
- `request`:
  - `model`, `stream`, `input_summary`
  - `tools.path` + `tools.each.{name,raw_args}`
- `response`:
  - `model`, `finish_reason`
  - `usage.input_tokens`, `usage.output_tokens`
  - `error.error_type`, `error.error_message`
- `stream`:
  - `usage.input_tokens`, `usage.output_tokens`
  - `finish_reason`

## Extraction primitives

- `path`: selector using gjson-style dot notation semantics
- `default`: fallback value when selector missing
- `truncate`: max string length
- `serialize`: optional output serializer (`json`)
- `map`: string-to-string value translation
- `each`: per-item mapping for arrays

## Notes

- Example specs may use `$.` prefix for readability; selectors compile to dot notation.
- Mapping failures produce `mapping_degraded=true` markers (or 502 in strict mode).
